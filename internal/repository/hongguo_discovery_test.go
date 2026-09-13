package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoDiscoveryCheckpointAndRetryIsolation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	r := New(db).HongGuo
	ctx := t.Context()
	works := []hongguo.Work{{SourceID: "96001", Title: "摘要", CoverURL: "https://example.invalid/cover"}}
	state := model.HongGuoSyncState{Category: "real-drama", NextPage: 2}
	bad := state
	bad.Category = strings.Repeat("x", 33)
	if err := r.SaveDiscoveryPage(ctx, works, bad); err == nil {
		t.Fatal("invalid checkpoint accepted")
	}
	var n int64
	if err := db.Model(&model.HongGuoDiscovery{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("page not rolled back: %d %v", n, err)
	}
	cutoff := time.Now()
	if err := r.SaveDiscoveryPage(ctx, works, state); err != nil {
		t.Fatal(err)
	}
	var discovery model.HongGuoDiscovery
	if err := db.First(&discovery, "source_id = ?", "96001").Error; err != nil || discovery.SourceCategory != "real-drama" {
		t.Fatalf("source category not saved: %+v %v", discovery, err)
	}
	rows, err := r.PendingDiscoveries(ctx, "", cutoff)
	if err != nil || len(rows) != 0 {
		t.Fatalf("new rows crossed cutoff: %+v %v", rows, err)
	}
	if err := r.RecordSyncFailure(ctx, "96001"); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveDiscoveryPage(ctx, works, state); err != nil {
		t.Fatal(err)
	}
	rows, err = r.PendingDiscoveries(ctx, "", time.Now())
	if err != nil || len(rows) != 0 {
		t.Fatalf("rediscovery bypassed failure cooldown: %+v %v", rows, err)
	}
	if err := r.ClearSyncFailure(ctx, "96001"); err != nil {
		t.Fatal(err)
	}
	rows, err = r.PendingDiscoveries(ctx, "", time.Now())
	if err != nil || len(rows) != 1 {
		t.Fatalf("pending not recoverable: %+v %v", rows, err)
	}
	hydrated, err := r.SaveDetail(ctx, hongguo.Work{SourceID: "96001", Title: "权威详情", EpisodeCount: 2, Snapshot: []byte(`{}`)})
	if err != nil || hydrated.SourceCategory != "real-drama" {
		t.Fatal(err)
	}
	if err := r.SaveDiscoveryPage(ctx, works, state); err != nil {
		t.Fatal(err)
	}
	rows, err = r.PendingDiscoveries(ctx, "", time.Now())
	if err != nil || len(rows) != 0 {
		t.Fatalf("completed queued again: %+v %v", rows, err)
	}
	if err := db.Model(&model.HongGuoArtwork{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("summary created artwork: %d %v", n, err)
	}
	if err := r.ReplaceRank(ctx, "hot-drama", "", []hongguo.Work{{SourceID: "96001", Title: "总榜摘要"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&discovery, "source_id = ?", "96001").Error; err != nil || discovery.SourceCategory != "real-drama" {
		t.Fatalf("overall rank erased source category: %+v %v", discovery, err)
	}
	if err := r.ReplaceRank(ctx, "hot-real-drama", "real-drama", []hongguo.Work{{SourceID: "96001", Title: "榜单摘要"}, {SourceID: "96002", Title: "新榜单摘要"}}); err != nil {
		t.Fatal(err)
	}
	var rankRows []model.HongGuoRankEntry
	if err := db.Order("position").Find(&rankRows, "rank_key = ?", "hot-real-drama").Error; err != nil || len(rankRows) != 2 || rankRows[0].SourceID != "96001" || rankRows[1].Position != 2 {
		t.Fatalf("rank rows=%+v err=%v", rankRows, err)
	}
	if err := r.ReplaceRank(ctx, "hot-real-drama", "real-drama", []hongguo.Work{{SourceID: "96002", Title: "重复"}, {SourceID: "96002", Title: "重复"}}); err == nil {
		t.Fatal("duplicate rank replaced valid entries")
	}
	rankRows = nil
	if err := db.Order("position").Find(&rankRows, "rank_key = ?", "hot-real-drama").Error; err != nil || len(rankRows) != 2 || rankRows[0].SourceID != "96001" {
		t.Fatalf("failed replacement lost old rank: %+v %v", rankRows, err)
	}
	if err := r.ReplaceRank(ctx, "hot-real-drama", "real-drama", []hongguo.Work{{SourceID: "96002", Title: "新榜单摘要"}}); err != nil {
		t.Fatal(err)
	}
	rankRows = nil
	if err := db.Find(&rankRows, "rank_key = ?", "hot-real-drama").Error; err != nil || len(rankRows) != 1 || rankRows[0].SourceID != "96002" {
		t.Fatalf("stale rank rows retained: %+v %v", rankRows, err)
	}
}
