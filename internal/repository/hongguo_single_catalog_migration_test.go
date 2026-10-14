package repository

import (
	"os"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm"
)

func TestHongGuoSingleCatalogMigration(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.HongGuoModels()...); err != nil {
		t.Fatal(err)
	}
	repo := New(db).HongGuo
	work, err := repo.SaveDetail(t.Context(), hongguo.Work{SourceID: "100", Title: "完整详情", EpisodeCount: 2, CoverURL: "https://example.invalid/detail", Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var episode model.HongGuoEpisode
	if err := db.First(&episode, "work_id = ?", work.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE hongguo_works DROP COLUMN summary_only;
 CREATE TABLE hongguo_discoveries (source_id varchar(32) PRIMARY KEY, source_category varchar(32) NOT NULL DEFAULT '', title text, overview text, cover_url text, episode_count bigint, update_text text, created_at timestamptz, updated_at timestamptz);
 INSERT INTO hongguo_discoveries VALUES
 ('100','real-drama','过期摘要','','https://example.invalid/stale',99,'',now(),now()),
 ('200','ai-drama','只有摘要','简介','https://example.invalid/summary',12,'连载',now(),now());`).Error; err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../scripts/migrations/20261013_hongguo_single_catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	// 删除旧表遇到依赖时必须整笔回滚，不使用 CASCADE 丢弃依赖。
	if err := db.Exec(`CREATE VIEW retained_discoveries AS SELECT source_id FROM hongguo_discoveries`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(sql)).Error; err == nil {
		t.Fatal("migration ignored dependent view")
	}
	if err := db.Exec("ROLLBACK").Error; err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("hongguo_discoveries") || db.Migrator().HasColumn("hongguo_works", "summary_only") {
		t.Fatal("failed migration did not roll back")
	}
	if err := db.Exec("DROP VIEW retained_discoveries").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatal(err)
		}
	}
	if db.Migrator().HasTable("hongguo_discoveries") {
		t.Fatal("old table retained")
	}
	// 模拟部署后的新连接，清除迁移前 SELECT * 的驱动缓存。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(c any) error { return c.(*stdlib.Conn).Conn().DeallocateAll(t.Context()) })
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	var got model.HongGuoWork
	if err := db.First(&got, "source_id = ?", "100").Error; err != nil {
		t.Fatal(err)
	}
	if got.ID != work.ID || got.Title != work.Title || got.SummaryOnly || got.EpisodeCount != 2 || got.SourceCategory != "real-drama" {
		t.Fatalf("detail changed: %+v", got)
	}
	var summary model.HongGuoWork
	if err := db.First(&summary, "source_id = ?", "200").Error; err != nil {
		t.Fatal(err)
	}
	if !summary.SummaryOnly || summary.Title != "只有摘要" || summary.EpisodeCount != 12 {
		t.Fatalf("summary lost: %+v", summary)
	}
	var image model.HongGuoArtwork
	if err := db.First(&image, "source_id = ?", "100").Error; err != nil || image.SourceURL != "https://example.invalid/detail" {
		t.Fatalf("poster replaced: %+v %v", image, err)
	}
	hydrated, err := repo.SaveDetail(t.Context(), hongguo.Work{SourceID: "200", Title: "补齐", EpisodeCount: 1, Snapshot: []byte(`{}`)})
	if err != nil || hydrated.ID != summary.ID || hydrated.SummaryOnly {
		t.Fatalf("upgrade failed: %+v %v", hydrated, err)
	}
	var oldEpisode model.HongGuoEpisode
	if err := db.First(&oldEpisode, "id = ?", episode.ID).Error; err != nil || oldEpisode.WorkID != work.ID {
		t.Fatalf("episode identity lost: %v", err)
	}
	if err := db.AutoMigrate(model.HongGuoModels()...); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("hongguo_discoveries") {
		t.Fatal("old table recreated")
	}
}
