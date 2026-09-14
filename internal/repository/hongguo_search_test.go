package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoSearchResultsAreReadOnlyAndKeepSourceOrder(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.HongGuoModels()...); err != nil {
		t.Fatal(err)
	}
	r := New(db).HongGuo
	ctx := t.Context()
	known := hongguo.Work{SourceID: "9000000000000000001", Title: "本地资料", Tags: []string{"玄幻"}, Snapshot: []byte(`{}`)}
	if err = r.SaveDiscoveryPage(ctx, []hongguo.Work{known}, model.HongGuoSyncState{Category: "ai-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.SaveDetail(ctx, known); err != nil {
		t.Fatal(err)
	}
	comic := model.HongGuoDiscovery{SourceID: "9000000000000000003", Title: "漫画", SourceCategory: "comic"}
	if err = db.Create(&comic).Error; err != nil {
		t.Fatal(err)
	}
	remote := []hongguo.Work{{SourceID: "9000000000000000002", Title: "新结果", Tags: []string{"剧情"}, CoverURL: "https://example.invalid/private"}, {SourceID: known.SourceID, Title: "官网标题"}, {SourceID: comic.SourceID, Title: comic.Title}}
	rows, err := r.SearchResults(ctx, remote)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].SourceID != remote[0].SourceID || rows[0].Hydrated || rows[0].SourceCategory != "" || !rows[1].Hydrated || rows[1].Title != known.Title || rows[1].SourceCategory != "ai-drama" {
		t.Fatalf("bad projection: %+v", rows)
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), "example.invalid") {
		t.Fatal("upstream URL leaked")
	}
	var count int64
	for _, table := range []string{"hongguo_works", "hongguo_discoveries", "hongguo_artworks"} {
		if err = db.Table(table).Where("source_id = ?", remote[0].SourceID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("search mutated %s: %d %v", table, count, err)
		}
	}
}
