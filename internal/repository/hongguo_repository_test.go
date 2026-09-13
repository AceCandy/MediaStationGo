package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoDetailIsolationIdentityAndRollback(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	legacy := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "旧电影", Source: "manual"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	r := New(db).HongGuo
	if err := r.SaveDiscoveryPage(ctx, []hongguo.Work{{SourceID: "9000000000000000001", Title: "测试剧摘要", CoverURL: "https://example.invalid/summary"}}, model.HongGuoSyncState{Category: "real-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
	}
	rows, total, err := r.List(ctx, "测试", "real-drama", "", "", 1, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Hydrated || rows[0].ArtworkID == "" || rows[0].Title != "测试剧摘要" {
		t.Fatalf("discovery-only list projection: total=%d rows=%+v err=%v", total, rows, err)
	}
	rows, total, err = r.List(ctx, "", "real-drama", "短剧", "", 1, 10)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("discovery-only row entered detail tag filter: total=%d rows=%+v err=%v", total, rows, err)
	}
	input := hongguo.Work{SourceID: "9000000000000000001", Title: "测试剧", Tags: []string{"短剧"}, EpisodeCount: 2, TotalEpisodes: 2, Completed: true, CoverURL: "https://example.invalid/cover", VideoIDs: []string{"9000000000000000002", "9000000000000000003"}, People: []hongguo.Person{{SourceID: "9000000000000000004", Name: "演员甲", Subtitle: "主演", AvatarURL: "https://example.invalid/avatar"}}, Snapshot: json.RawMessage(`{"series_name":"测试剧"}`)}
	first, err := r.SaveDetail(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var episodes []model.HongGuoEpisode
	if err := db.Order("number").Find(&episodes).Error; err != nil || len(episodes) != 2 {
		t.Fatalf("episodes=%v err=%v", episodes, err)
	}
	input.Title = "更新标题"
	input.VideoIDs = nil
	second, err := r.SaveDetail(ctx, input)
	if err != nil || first.ID != second.ID {
		t.Fatalf("identity changed: %v", err)
	}
	var after []model.HongGuoEpisode
	if err := db.Order("number").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].ID != episodes[0].ID || after[1].ID != episodes[1].ID || after[0].SourceVideoID != "9000000000000000002" {
		t.Fatal("episode identity/source lost on refresh")
	}
	for _, table := range []struct {
		model    any
		expected int64
	}{{&model.HongGuoWork{}, 1}, {&model.HongGuoPerson{}, 1}, {&model.HongGuoCredit{}, 1}, {&model.HongGuoSnapshot{}, 1}, {&model.HongGuoArtwork{}, 2}, {&model.MetadataItem{}, 1}} {
		var n int64
		if err := db.Model(table.model).Count(&n).Error; err != nil || n != table.expected {
			t.Fatalf("%T count=%d err=%v", table.model, n, err)
		}
	}
	input.Title = "不应该保存"
	input.People = []hongguo.Person{{SourceID: "invalid", Name: "无效人物"}}
	if _, err := r.SaveDetail(ctx, input); err == nil {
		t.Fatal("invalid person accepted")
	}
	current, err := r.FindBySourceID(ctx, input.SourceID)
	if err != nil || current.Title != "更新标题" {
		t.Fatalf("transaction failed to rollback: %+v %v", current, err)
	}
	var old model.MetadataItem
	if err := db.First(&old, "id = ?", legacy.ID).Error; err != nil || old.Title != legacy.Title {
		t.Fatal("legacy data changed")
	}
	if first.SourceCategory != "real-drama" || second.SourceCategory != "real-drama" {
		t.Fatal("source category was not carried into the hydrated work")
	}
	if err := r.SaveDiscoveryPage(ctx, []hongguo.Work{{SourceID: input.SourceID, Title: "过期摘要", CoverURL: "https://example.invalid/stale-summary"}}, model.HongGuoSyncState{Category: "real-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
	}
	var poster model.HongGuoArtwork
	if err := db.First(&poster, "source_id = ?", input.SourceID).Error; err != nil || poster.WorkID == nil || *poster.WorkID != first.ID || poster.SourceURL != input.CoverURL {
		t.Fatalf("hydrated poster was overwritten by discovery: %+v %v", poster, err)
	}
	rows, total, err = r.List(ctx, "更新", "real-drama", "", "", 1, 1)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("list total=%d rows=%d err=%v", total, len(rows), err)
	}
	if !rows[0].Hydrated || rows[0].ArtworkID == "" || len(rows[0].TagList) != 1 || rows[0].TagList[0] != "短剧" {
		t.Fatalf("poster list projection missing: %+v", rows[0])
	}
	payload, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(payload), "example.invalid") || strings.Contains(string(payload), "source_url") {
		t.Fatalf("poster list leaked upstream artwork: %s %v", payload, err)
	}
	rows, total, err = r.List(ctx, "更新", "real-drama", "", "", 2, 1)
	if err != nil || total != 1 || len(rows) != 0 {
		t.Fatal("empty page count incorrect")
	}
	rows, total, err = r.List(ctx, "", "", "短", "", 1, 1)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("category filter used a partial tag match")
	}
	for _, row := range []model.HongGuoWork{
		{SourceID: "9000000000000000005", SourceCategory: "comic", Kind: model.MetadataKindSeries, Title: "历史漫画", Tags: "[]", RefreshedAt: first.RefreshedAt},
		{SourceID: "9000000000000000006", Kind: model.MetadataKindSeries, Title: "历史空分类", Tags: "[]", RefreshedAt: first.RefreshedAt},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err = r.List(ctx, "", "", "", "", 1, 10)
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("default list included comic: total=%d rows=%d err=%v", total, len(rows), err)
	}
	if err := r.ReplaceRank(ctx, "hot-drama", "", []hongguo.Work{{SourceID: "9000000000000000006", Title: "历史空分类"}, {SourceID: first.SourceID, Title: first.Title}}); err != nil {
		t.Fatal(err)
	}
	rows, total, err = r.List(ctx, "", "", "", "hot-drama", 1, 10)
	if err != nil || total != 2 || len(rows) != 2 || rows[0].SourceID != "9000000000000000006" || rows[1].SourceID != first.SourceID {
		t.Fatalf("official rank order lost: total=%d rows=%+v err=%v", total, rows, err)
	}
}

func TestHongGuoArtworkOwnershipMigration(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	work, err := New(db).HongGuo.SaveDetail(t.Context(), hongguo.Work{SourceID: "9300000000000000001", Title: "旧版海报", EpisodeCount: 1, CoverURL: "https://example.invalid/legacy", Snapshot: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE hongguo_artworks DROP CONSTRAINT chk_hongguo_artwork_owner_v2",
		"DROP INDEX uidx_hongguo_artwork_source",
		"ALTER TABLE hongguo_artworks DROP COLUMN source_id",
		"ALTER TABLE hongguo_artworks ADD CONSTRAINT chk_hongguo_artwork_owner CHECK ((work_id IS NULL) <> (person_id IS NULL))",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropIndex(&model.HongGuoWork{}, "SourceID"); err != nil {
		t.Fatal(err)
	}
	duplicateWork := model.HongGuoWork{SourceID: work.SourceID, Kind: model.MetadataKindSeries, Title: "重复来源", Tags: "[]", RefreshedAt: time.Now()}
	if err := db.Create(&duplicateWork).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HongGuoArtwork{WorkID: &duplicateWork.ID, SourceURL: "https://example.invalid/duplicate"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err == nil {
		t.Fatal("duplicate source posters passed migration")
	}
	if db.Migrator().HasColumn(&model.HongGuoArtwork{}, "SourceID") {
		t.Fatal("failed artwork migration left source_id behind")
	}
	if err := db.Delete(&model.HongGuoArtwork{}, "work_id = ?", duplicateWork.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&duplicateWork).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var migrated model.HongGuoArtwork
	if err := db.First(&migrated, "work_id = ?", work.ID).Error; err != nil || migrated.SourceID == nil || *migrated.SourceID != work.SourceID {
		t.Fatalf("legacy poster source ID not restored: %+v %v", migrated, err)
	}
	sourceID := "9300000000000000002"
	if err := db.Create(&model.HongGuoArtwork{SourceID: &sourceID, SourceURL: "https://example.invalid/discovery"}).Error; err != nil {
		t.Fatalf("source-only discovery poster rejected: %v", err)
	}
	if err := db.Create(&model.HongGuoArtwork{SourceURL: "https://example.invalid/orphan"}).Error; err == nil {
		t.Fatal("ownerless artwork accepted")
	}
	legacyWork := model.HongGuoWork{SourceID: "9300000000000000003", Kind: model.MetadataKindSeries, Title: "回滚兼容", Tags: "[]", RefreshedAt: time.Now()}
	if err := db.Create(&legacyWork).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HongGuoArtwork{WorkID: &legacyWork.ID, SourceURL: "https://example.invalid/work-only"}).Error; err != nil {
		t.Fatalf("legacy work-only poster rejected: %v", err)
	}
}
