package service

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type parallelSearchBackend func(context.Context) ([]string, error)

func (f parallelSearchBackend) SearchMetadataIDs(ctx context.Context, _ string, _, _ int, _ repository.MetadataSearchFilter) ([]string, int64, error) {
	ids, err := f(ctx)
	return ids, int64(len(ids)), err
}

// 并发和取消可能新建连接，每条连接都必须固定到当前测试的隔离 schema。
func newParallelSearchService(t *testing.T) *MediaService {
	t.Helper()
	db := newServiceTestDB(t)
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	pgConfig, err := pgx.ParseConfig(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	pgConfig.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*pgConfig)
	sqlDB.SetMaxOpenConns(3)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: db.Logger})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	episodeID := "source-ep"
	for _, value := range []any{
		&model.Library{Base: model.Base{ID: "library"}, Name: "Search", Type: "mixed", Path: "/test/search"},
		&model.MetadataItem{PermanentBase: model.PermanentBase{ID: "ordinary"}, Kind: "movie", Title: "并行", Source: "test"},
		&model.Media{PermanentBase: model.PermanentBase{ID: "ordinary-file"}, LibraryID: "library", MetadataID: "ordinary", Path: "/test/search/ordinary"},
		&model.NFOItem{PermanentBase: model.PermanentBase{ID: "local"}, LibraryID: "library", LocalKey: "local", Kind: "movie", NFOFields: model.NFOFields{Title: "并行"}},
		&model.Media{PermanentBase: model.PermanentBase{ID: "local-file"}, LibraryID: "library", CatalogSource: "nfo", Path: "/test/search/local"},
		&model.NFOMediaBinding{MediaID: "local-file", ItemID: "local"},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "source"}, SourceID: "101", Kind: "series", Title: "并行", RelatedAlbumID: "101", SeasonIndex: 1},
		&model.Media{PermanentBase: model.PermanentBase{ID: "source-file"}, LibraryID: "library", CatalogSource: "hongguo", Path: "/test/search/source"},
		&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "source-ep"}, WorkID: "source", Number: 1},
		&model.HongGuoMediaBinding{MediaID: "source-file", WorkID: "source", EpisodeID: &episodeID},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	return NewMediaService(&config.Config{}, zap.NewNop(), repository.New(db))
}

func TestWebSourceSearchParallel(t *testing.T) {
	for _, mode := range []string{"indexed", "fallback", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			svc := newParallelSearchService(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			started := make(chan string, 3)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			wait := func(ctx context.Context, source string) error {
				select {
				case started <- source:
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			backend := func(source, id string) parallelSearchBackend {
				return func(ctx context.Context) ([]string, error) {
					if err := wait(ctx, source); err != nil {
						return nil, err
					}
					if mode == "fallback" {
						return nil, errors.New("index unavailable")
					}
					return []string{id}, nil
				}
			}
			if mode != "postgres" {
				svc.repo.MediaView.SetSearchBackend(backend("ordinary", "ordinary"))
				svc.repo.HongGuo.SetSearchBackend(backend("hongguo", "hg-group-101"))
			}
			observe := func(tx *gorm.DB) {
				sql := tx.Statement.SQL.String()
				source := ""
				switch {
				case strings.Contains(sql, "'nfo-' || search_metadata.id AS id"):
					source = "nfo"
				case mode == "postgres" && strings.HasPrefix(sql, "SELECT search_metadata.id AS id"):
					source = "ordinary"
				case mode == "postgres" && strings.Contains(sql, "AS search_metadata") && strings.Contains(sql, "hongguo_works"):
					source = "hongguo"
				}
				if source != "" && tx.Error == nil {
					// SQL 已实际执行；让三个来源都到达后才释放，不能只观察 goroutine 启动。
					_ = wait(tx.Statement.Context, source)
				}
			}
			if err := svc.repo.DB.Callback().Row().After("gorm:row").Register("test:parallel-search", observe); err != nil {
				t.Fatal(err)
			}
			var items []model.MediaView
			var total int64
			var searchErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				items, total, searchErr = svc.SearchMediaVisiblePage(ctx, "并行", 1, 30, MediaVisibility{IncludeNSFW: true})
			}()
			t.Cleanup(func() {
				cancel()
				unblock()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("search did not stop during cleanup")
				}
			})
			seen := map[string]bool{}
			for len(seen) < 3 {
				select {
				case source := <-started:
					if seen[source] {
						t.Fatalf("source queried twice: %s", source)
					}
					seen[source] = true
				case <-time.After(3 * time.Second):
					t.Fatalf("sources did not overlap: %v", seen)
				}
			}
			unblock()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("search did not finish after releasing sources")
			}
			ids := make([]string, 0, len(items))
			for _, item := range items {
				ids = append(ids, item.ID)
			}
			if searchErr != nil || total != 3 || !reflect.DeepEqual(ids, []string{"source-file", "local-file", "ordinary-file"}) {
				t.Fatalf("ids=%v total=%d err=%v", ids, total, searchErr)
			}
			select {
			case source := <-started:
				t.Fatalf("source queried twice: %s", source)
			default:
			}
		})
	}
}

func TestWebSourceSearchParallelPagination(t *testing.T) {
	svc := newParallelSearchService(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items (id,kind,title,source) SELECT 'ordinary-'||n,'movie','并行','test' FROM generate_series(1,105) n`,
		`INSERT INTO media (id,library_id,metadata_id,path) SELECT 'ordinary-file-'||n,'library','ordinary-'||n,'/test/ordinary/'||n FROM generate_series(1,105) n`,
		`INSERT INTO nfo_items (id,library_id,local_key,kind,title) SELECT 'local-'||n,'library',n::text,'movie','并行' FROM generate_series(1,105) n`,
		`INSERT INTO media (id,library_id,catalog_source,path) SELECT 'local-file-'||n,'library','nfo','/test/local/'||n FROM generate_series(1,105) n`,
		`INSERT INTO nfo_media_bindings (media_id,item_id,fingerprint,title) SELECT 'local-file-'||n,'local-'||n,'','并行' FROM generate_series(1,105) n`,
	} {
		if err := svc.repo.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	visibility := MediaVisibility{IncludeNSFW: true}
	// 有红果候选时，仍沿用原来的三库合并 100 条上限。
	items, total, err := svc.SearchMediaVisiblePage(t.Context(), "并行", 3, 50, visibility)
	if err != nil || total != 100 || len(items) != 0 {
		t.Fatalf("mixed page: count=%d total=%d err=%v", len(items), total, err)
	}
	if err := svc.repo.DB.Exec("UPDATE hongguo_works SET title = '其他'").Error; err != nil {
		t.Fatal(err)
	}
	// 无红果命中时，普通/NFO 各 100 条的旧总数及后续页不能被新的并行入口截掉。
	for _, query := range []string{"并行", "", "not-found"} {
		for _, page := range []int{1, 3, 8} {
			want, wantTotal, err := svc.repo.MediaView.SearchFilteredPage(t.Context(), query, (page-1)*50, 50, repository.MediaQueryFilter{})
			if err != nil {
				t.Fatal(err)
			}
			got, total, err := svc.SearchMediaVisiblePage(t.Context(), query, page, 50, visibility)
			if err != nil || total != wantTotal || len(got) != len(want) {
				t.Fatalf("query=%q page=%d count=%d/%d total=%d/%d err=%v", query, page, len(got), len(want), total, wantTotal, err)
			}
			for i := range got {
				if got[i].ID != want[i].ID {
					t.Fatalf("query=%q page=%d position=%d id=%s want=%s", query, page, i, got[i].ID, want[i].ID)
				}
			}
			if query == "并行" && total != 200 {
				t.Fatalf("ordinary/NFO total=%d, want 200", total)
			}
		}
	}
	// 索引成功返回 nil 表示无普通候选，不得被误判为索引失败后数据库回退。
	svc.repo.MediaView.SetSearchBackend(parallelSearchBackend(func(context.Context) ([]string, error) { return nil, nil }))
	items, total, err = svc.SearchMediaVisiblePage(t.Context(), "并行", 1, 30, visibility)
	if err != nil || total != 100 || len(items) != 30 {
		t.Fatalf("empty ordinary index: count=%d total=%d err=%v", len(items), total, err)
	}
	for _, item := range items {
		if item.CatalogSource != "nfo" {
			t.Fatalf("unexpected ordinary database fallback: %s", item.ID)
		}
	}
}

func TestWebSourceSearchParallelCancellation(t *testing.T) {
	for _, failDatabase := range []bool{false, true} {
		name := "parent"
		if failDatabase {
			name = "database"
		}
		t.Run(name, func(t *testing.T) {
			svc := newParallelSearchService(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			started := make(chan struct{}, 3)
			trigger := make(chan struct{})
			var stopped atomic.Int32
			backend := parallelSearchBackend(func(ctx context.Context) ([]string, error) {
				started <- struct{}{}
				<-ctx.Done()
				stopped.Add(1)
				return nil, ctx.Err()
			})
			svc.repo.MediaView.SetSearchBackend(backend)
			svc.repo.HongGuo.SetSearchBackend(backend)
			databaseErr := errors.New("NFO query failed")
			if err := svc.repo.DB.Callback().Row().Before("gorm:row").Register("test:search-failure", func(tx *gorm.DB) {
				if !strings.Contains(strings.Join(tx.Statement.Selects, " "), "'nfo-' || search_metadata.id AS id") {
					return
				}
				started <- struct{}{}
				select {
				case <-trigger:
					_ = tx.AddError(databaseErr)
				case <-tx.Statement.Context.Done():
					_ = tx.AddError(tx.Statement.Context.Err())
				}
			}); err != nil {
				t.Fatal(err)
			}
			var searchErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _, searchErr = svc.SearchMediaVisiblePage(ctx, "并行", 1, 30, MediaVisibility{IncludeNSFW: true})
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("search did not stop during cleanup")
				}
			})
			for range 3 {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("sources did not enter before cancellation")
				}
			}
			wantErr := context.Canceled
			if failDatabase {
				wantErr = databaseErr
				close(trigger)
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("search did not cancel and join its sources")
			}
			if !errors.Is(searchErr, wantErr) || stopped.Load() != 2 {
				t.Fatalf("err=%v want=%v stopped=%d", searchErr, wantErr, stopped.Load())
			}
		})
	}
}
