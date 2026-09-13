package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
	if err := r.SaveDiscoveryPage(ctx, []hongguo.Work{{SourceID: "9000000000000000001", Title: "测试剧"}}, model.HongGuoSyncState{Category: "real-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
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
	rows, total, err := r.List(ctx, "更新", "real-drama", "", "", 1, 1)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("list total=%d rows=%d err=%v", total, len(rows), err)
	}
	if rows[0].ArtworkID == "" || len(rows[0].TagList) != 1 || rows[0].TagList[0] != "短剧" {
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
