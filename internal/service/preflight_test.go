package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type preflightReadLog struct {
	logger.Interface
	reads atomic.Int64
}

func (l *preflightReadLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.HasPrefix(sql, "SELECT") {
		l.reads.Add(1)
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestLibraryPreflightQueriesAndInvalidation(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.User{}, &model.Setting{}, &model.PlayProfile{})
	repo := repository.New(db)
	cache := NewRuntimeCacheService(nil, nil)
	emby := NewEmbyService(nil, nil, repo).SetRuntimeCache(cache)
	lib := model.Library{Name: "Movies", Path: "/fixture/movies", Type: "movie"}
	if err := repo.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: lib.Path, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "viewer", PasswordHash: "unused"}
	if err := repo.User.Create(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	if err := repo.User.UpdateFields(t.Context(), user.ID, map[string]any{"hide_adult": false}); err != nil {
		t.Fatal(err)
	}
	user.HideAdult = false
	emby.mediaVisibility(WithAuthenticatedUser(t.Context(), &user), user.ID)
	reads := &preflightReadLog{Interface: db.Logger}
	db.Logger = reads
	for _, want := range []int64{2, 1} {
		reads.reads.Store(0)
		current, err := repo.User.FindByID(t.Context(), user.ID)
		if err != nil {
			t.Fatal(err)
		}
		ctx := WithAuthenticatedUser(t.Context(), current)
		if !emby.mediaVisibility(ctx, user.ID).IncludeNSFW {
			t.Fatal("unexpected visibility")
		}
		got, err := FindLibraryBasic(ctx, repo, cache, lib.ID)
		if err != nil || got == nil || got.Name != lib.Name || len(got.Roots) != 0 {
			t.Fatalf("basic library = %+v, err=%v", got, err)
		}
		if n := reads.reads.Load(); n != want {
			t.Fatalf("preflight reads=%d, want %d", n, want)
		}
		got.Name = "must not mutate cached value"
	}
	reads.reads.Store(0)
	if rows, err := repo.Library.ListBasic(t.Context()); err != nil || len(rows) != 1 || len(rows[0].Roots) != 0 || reads.reads.Load() != 1 {
		t.Fatalf("basic list: rows=%+v reads=%d err=%v", rows, reads.reads.Load(), err)
	}
	if full, err := repo.Library.FindByID(t.Context(), lib.ID); err != nil || full == nil || len(full.Roots) != 1 {
		t.Fatalf("full library lost roots: %+v err=%v", full, err)
	}
	media := NewMediaService(nil, zap.NewNop(), repo).SetRuntimeCache(cache)
	if err := media.UpdateLibraryCover(t.Context(), lib.ID, "/fixture/cover.jpg"); err != nil {
		t.Fatal(err)
	}
	if got, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID); err != nil || got.CoverURL != "/fixture/cover.jpg" {
		t.Fatalf("stale cover: %+v err=%v", got, err)
	}
	// 有效期过后必须重读；不依赖 wall-clock sleep。
	cache.mu.Lock()
	for key, item := range cache.memory {
		item.expiresAt = time.Now().Add(-time.Second)
		cache.memory[key] = item
	}
	cache.mu.Unlock()
	reads.reads.Store(0)
	if _, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID); err != nil || reads.reads.Load() != 1 {
		t.Fatalf("expired cache reads=%d err=%v", reads.reads.Load(), err)
	}
	for i := 0; i < 2; i++ {
		reads.reads.Store(0)
		if got, err := FindLibraryBasic(t.Context(), repo, cache, "missing"); err != nil || got != nil || reads.reads.Load() != 1 {
			t.Fatalf("missing library: %+v err=%v", got, err)
		}
	}
	repo.InvalidateReadCache()
	readError := errors.New("library read failed")
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail-library-read", func(q *gorm.DB) {
		if q.Statement.Table == "libraries" {
			q.AddError(readError)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID); got != nil || !errors.Is(err, readError) {
		t.Fatalf("query failure became a cached success: %+v err=%v", got, err)
	}
	if err := db.Callback().Query().Remove("test:fail-library-read"); err != nil {
		t.Fatal(err)
	}
	if got, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID); err != nil || got == nil {
		t.Fatalf("query error was cached: %+v err=%v", got, err)
	}
	if err := repo.Library.Delete(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID); err != nil || got != nil {
		t.Fatalf("deleted library remained cached: %+v err=%v", got, err)
	}
}

func TestPreflightVisibilityInvalidationAndTargetUser(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.User{}, &model.PlayProfile{}, &model.Setting{})
	repo := repository.New(db)
	emby := NewEmbyService(nil, nil, repo)
	viewer := model.User{Username: "viewer", PasswordHash: "unused"}
	admin := model.User{Username: "admin", PasswordHash: "unused", Role: "admin"}
	for _, user := range []*model.User{&viewer, &admin} {
		if err := repo.User.Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.User.UpdateFields(t.Context(), admin.ID, map[string]any{"hide_adult": false}); err != nil {
		t.Fatal(err)
	}
	admin.HideAdult = false
	ctx := WithAuthenticatedUser(t.Context(), &admin)
	if !UserHidesAdult(ctx, repo, viewer.ID) || UserHidesAdult(ctx, repo, admin.ID) {
		t.Fatal("administrator identity leaked into target-user visibility")
	}
	if !emby.mediaVisibility(ctx, admin.ID).IncludeNSFW {
		t.Fatal("initial adult visibility")
	}
	profile := model.PlayProfile{UserID: admin.ID, Name: "default", IsDefault: true, AllowAdult: true, AllowedLibraryIDs: `["allowed"]`}
	if err := repo.PlayProfile.Create(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	if v := emby.mediaVisibility(ctx, admin.ID); !v.LibraryRestricted || len(v.AllowedLibraryIDs) != 1 || v.AllowedLibraryIDs[0] != "allowed" {
		t.Fatalf("stale default profile: %+v", v)
	}
	if err := repo.PlayProfile.Update(ctx, profile.ID, map[string]any{"allowed_library_ids": "[]", "allow_adult": false}); err != nil {
		t.Fatal(err)
	}
	if v := emby.mediaVisibility(ctx, admin.ID); v.IncludeNSFW || !v.LibraryRestricted || len(v.AllowedLibraryIDs) != 0 {
		t.Fatalf("stale restricted profile: %+v", v)
	}
	if err := repo.PlayProfile.Delete(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}
	if v := emby.mediaVisibility(ctx, admin.ID); !v.IncludeNSFW || v.LibraryRestricted {
		t.Fatalf("deleted profile remained cached: %+v", v)
	}
	for _, enabled := range []bool{false, true} {
		value := "false"
		if enabled {
			value = "true"
		}
		if err := repo.Setting.Set(ctx, "adult.enabled", value); err != nil {
			t.Fatal(err)
		}
		if v := emby.mediaVisibility(ctx, admin.ID); v.IncludeNSFW != enabled {
			t.Fatalf("stale global setting: %+v", v)
		}
	}
	if err := repo.User.UpdateFields(ctx, admin.ID, map[string]any{"hide_adult": true}); err != nil {
		t.Fatal(err)
	}
	// 修改前已通过认证的请求可以结束，但它的旧快照不能填入新请求的缓存。
	emby.mediaVisibility(ctx, admin.ID)
	admin.HideAdult = true
	if v := emby.mediaVisibility(WithAuthenticatedUser(t.Context(), &admin), admin.ID); v.IncludeNSFW {
		t.Fatal("old request snapshot contaminated new visibility")
	}
}

func TestLibraryCacheOldReadCannotRefillAfterWrite(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{})
	repo := repository.New(db)
	cache := NewRuntimeCacheService(nil, nil)
	lib := model.Library{Name: "before", Path: "/fixture/movies", Type: "movie"}
	if err := repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	loaded, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	if err := db.Callback().Query().After("gorm:query").Register("test:block-library-read", func(q *gorm.DB) {
		if q.Statement.Table == "libraries" {
			once.Do(func() { close(loaded); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID)
		done <- err
	}()
	select {
	case <-loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not reach barrier")
	}
	if err := repo.Library.UpdateFields(t.Context(), lib.ID, map[string]any{"name": "after"}); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old read did not finish")
	}
	if got, err := FindLibraryBasic(t.Context(), repo, cache, lib.ID); err != nil || got == nil || got.Name != "after" {
		t.Fatalf("old read refilled current cache: %+v err=%v", got, err)
	}
	before := repo.ReadCacheKey()
	rollback := errors.New("rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repository.New(tx).Library.UpdateFields(t.Context(), lib.ID, map[string]any{"name": "rolled back"}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || repo.ReadCacheKey() != before {
		t.Fatalf("transaction rollback changed shared cache: %v", err)
	}
}
