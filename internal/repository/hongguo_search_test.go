package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	if err = db.AutoMigrate(append(model.HongGuoModels(), &model.HongGuoDownload{})...); err != nil {
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
	var saved model.HongGuoWork
	if err = db.First(&saved, "source_id = ?", known.SourceID).Error; err != nil {
		t.Fatal(err)
	}
	groupID := "9000000000000000099"
	if err = r.SaveAlbum(ctx, saved.SourceID, hongguo.Album{ID: groupID, Season: 2}); err != nil {
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
	if rows[0].GroupID != "" || rows[1].GroupID != groupID || rows[1].RelatedAlbumID != groupID || rows[1].SeasonIndex != 2 {
		t.Fatal("search group membership missing")
	}
	local, _, err := r.List(ctx, known.Title, "", "", "", 1, 50)
	if err != nil || len(local) != 1 || local[0].GroupID != groupID || local[0].RelatedAlbumID != groupID || local[0].SeasonIndex != 2 {
		t.Fatalf("list membership: %+v %v", local, err)
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

func TestHongGuoSearchQueuesMissingDetails(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.HongGuoModels()...); err != nil {
		t.Fatal(err)
	}
	r, ctx := New(db).HongGuo, t.Context()
	results := []HongGuoListWork{
		{HongGuoWork: model.HongGuoWork{SourceID: "90001", Title: "待补齐"}},
		{HongGuoWork: model.HongGuoWork{SourceID: "90002", Title: "已收录"}, Hydrated: true},
	}
	if err := r.QueueMissingSearchResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.HongGuoDiscovery{}).Where("source_id = ?", "90001").Updates(map[string]any{"source_category": "ai-drama", "title": "已分类"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.QueueMissingSearchResults(ctx, append(results, results[0])); err != nil {
		t.Fatal(err)
	}
	pending, err := r.PendingDiscoveries(ctx, "", time.Now().Add(time.Second))
	if err != nil || len(pending) != 1 || pending[0].SourceID != "90001" || pending[0].Title != "已分类" || pending[0].SourceCategory != "ai-drama" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if err := r.RecordSyncFailure(ctx, "90001", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.QueueMissingSearchResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	pending, err = r.PendingDiscoveries(ctx, "", time.Now().Add(time.Second))
	if err != nil || len(pending) != 0 {
		t.Fatalf("cooling item requeued: %+v %v", pending, err)
	}
	for _, table := range []string{"hongguo_works", "hongguo_episodes", "hongguo_artworks"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("unexpected %s writes: %d %v", table, count, err)
		}
	}
}
