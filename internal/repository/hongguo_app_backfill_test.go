package repository

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoAppBackfillPreservesExistingAndFillsCategory(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := New(db).HongGuo
	ctx := t.Context()
	for _, id := range []string{"100", "200"} {
		if _, err := repo.SaveDetail(ctx, hongguo.Work{SourceID: id, Title: "正式资料", EpisodeCount: 1, Snapshot: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SetSourceCategory(ctx, "200", "ai-drama"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HongGuoDiscovery{SourceID: "100", Title: "已有摘要"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSyncState(ctx, model.HongGuoSyncState{Category: "real-drama", NextPage: 7, AfterID: "500"}); err != nil {
		t.Fatal(err)
	}
	works := []hongguo.Work{{SourceID: "100", Title: "App摘要"}, {SourceID: "200", Title: "App摘要"}, {SourceID: "300", Title: "新作品"}, {SourceID: "300", Title: "重复"}}
	stats, err := repo.SaveAppDiscoveryPage(ctx, "real-drama", works)
	if err != nil {
		t.Fatal(err)
	}
	if stats.NewWorks != 1 || stats.ClassifiedWorks != 2 {
		t.Fatalf("stats=%+v", stats)
	}
	stats, err = repo.SaveAppDiscoveryPage(ctx, "real-drama", works)
	if err != nil || stats.NewWorks != 0 || stats.ClassifiedWorks != 0 {
		t.Fatalf("repeat=%+v err=%v", stats, err)
	}
	for _, tc := range []struct{ id, category, title string }{{"100", "real-drama", "已有摘要"}, {"200", "ai-drama", "App摘要"}, {"300", "real-drama", "新作品"}} {
		var d model.HongGuoDiscovery
		if err := db.First(&d, "source_id = ?", tc.id).Error; err != nil {
			t.Fatal(err)
		}
		if d.SourceCategory != tc.category || d.Title != tc.title {
			t.Fatalf("discovery=%+v", d)
		}
	}
	for _, tc := range []struct{ id, category string }{{"100", "real-drama"}, {"200", "ai-drama"}} {
		var w model.HongGuoWork
		if err := db.First(&w, "source_id = ?", tc.id).Error; err != nil {
			t.Fatal(err)
		}
		if w.SourceCategory != tc.category || w.Title != "正式资料" {
			t.Fatalf("canonical changed=%+v", w)
		}
	}
	state, err := repo.SyncState(ctx, "real-drama")
	if err != nil || state.NextPage != 7 || state.AfterID != "500" {
		t.Fatalf("web checkpoint=%+v err=%v", state, err)
	}
}
