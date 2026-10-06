package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNFODeletePrunesOnlyEmptyItems(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "NFO", Type: model.LibraryTypeNFOTV}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).NFO
	add := func(path, episode string) model.Media {
		t.Helper()
		m := model.Media{LibraryID: lib.ID, Path: path, CatalogSource: model.CatalogSourceNFO, ScrapeStatus: "matched"}
		input := &NFOIngest{Items: []model.NFOItem{
			{Kind: "series", LocalKey: "series", NFOFields: model.NFOFields{Title: "Series"}},
			{Kind: "season", LocalKey: "season", SeasonNum: 1},
			{Kind: "episode", LocalKey: episode, EpisodeNum: 1},
		}, Binding: model.NFOMediaBinding{Fingerprint: episode}}
		if _, err := repo.Ingest(t.Context(), &m, input); err != nil {
			t.Fatal(err)
		}
		return m
	}
	check := func(want int64) {
		t.Helper()
		var got int64
		if err := db.Model(&model.NFOItem{}).Where("library_id = ?", lib.ID).Count(&got).Error; err != nil || got != want {
			t.Fatalf("NFO items=%d want=%d err=%v", got, want, err)
		}
	}
	first := add("/fixture/first.mkv", "ep1")
	version := add("/fixture/version.mkv", "ep1")
	other := add("/fixture/other.mkv", "ep2")
	var binding model.NFOMediaBinding
	if err := db.First(&binding, "media_id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	state := model.NFOUserState{UserID: "viewer", ItemID: binding.ItemID, Favorite: true, PositionMs: 42}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	event := model.NFOPlaybackEvent{UserID: "viewer", ItemID: binding.ItemID, SessionID: "session", MediaID: first.ID, LibraryID: lib.ID}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	deleteOne := func(id string) {
		t.Helper()
		count, err := DeleteMedia(db.WithContext(t.Context()).Where("id = ?", id))
		if err != nil || count != 1 {
			t.Fatalf("delete count=%d err=%v", count, err)
		}
	}
	deleteOne(first.ID)
	check(4)
	deleteOne(version.ID)
	check(3)
	// 外层事务失败必须同时恢复文件、绑定和被清理的集/季/剧。
	rollback := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		if _, err := DeleteMedia(tx.Where("id = ?", other.ID)); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	check(3)
	deleteOne(other.ID)
	check(0)
	for _, row := range []any{&model.NFOUserState{}, &model.NFOPlaybackEvent{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("user data count=%d err=%v", count, err)
		}
	}
	// 改绑完成后清理旧条目；新层级已有绑定，不能被误删。
	rebound := add("/fixture/rebound.mkv", "old")
	if _, err := repo.Ingest(t.Context(), &rebound, &NFOIngest{
		Items:   []model.NFOItem{{Kind: "movie", LocalKey: "replacement"}},
		Binding: model.NFOMediaBinding{Fingerprint: "replacement"},
	}); err != nil {
		t.Fatal(err)
	}
	check(1)
	// 显式库/根清理也移除没有媒体的旧残留，且不会触碰其他库。
	if err := db.Create(&model.NFOItem{LibraryID: lib.ID, LocalKey: "orphan", Kind: "movie"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteMedia(db.Where("library_id = ?", lib.ID), lib.ID); err != nil {
		t.Fatal(err)
	}
	check(0)
	// 删除事务持有同库锁时，新版本入库等待提交；旧条目删完后可以正常重建绑定。
	last := add("/fixture/concurrent-old.mkv", "ep1")
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	peer, err := gorm.Open(postgres.Open(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := peer.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	if err := peer.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	defer tx.Rollback()
	if _, err := DeleteMedia(tx.Where("id = ?", last.ID)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		m := model.Media{LibraryID: lib.ID, Path: "/fixture/concurrent-new.mkv", CatalogSource: model.CatalogSourceNFO, ScrapeStatus: "matched"}
		_, err := New(peer).NFO.Ingest(ctx, &m, &NFOIngest{Items: []model.NFOItem{
			{Kind: "series", LocalKey: "series"}, {Kind: "season", LocalKey: "season", SeasonNum: 1},
			{Kind: "episode", LocalKey: "ep1", EpisodeNum: 1},
		}, Binding: model.NFOMediaBinding{Fingerprint: "concurrent"}})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("ingest did not wait for delete transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	check(3)
	var bindingCount int64
	if err := db.Model(&model.NFOMediaBinding{}).Count(&bindingCount).Error; err != nil || bindingCount != 1 {
		t.Fatalf("concurrent binding count=%d err=%v", bindingCount, err)
	}

}
