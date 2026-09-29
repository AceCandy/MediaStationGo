package service

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestEmbySeasonCandidatesRefillBeforeDetails(t *testing.T) {
	e := newTestEmbyService(t)
	db, ctx := e.repo.DB, t.Context()
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('show','series','Show','local')`,
		`INSERT INTO metadata_items(id,kind,parent_id,title,source,season_num) SELECT 'season-'||n,'season','show','Season','local',n FROM generate_series(1,155) n`,
		`INSERT INTO metadata_items(id,kind,parent_id,title,source,episode_num) SELECT 'ep-'||n,'episode','season-'||n,'Episode','local',1 FROM unnest(ARRAY[51,103,155]) n`,
		// 直接季文件会维护季的库归属，却不满足这里的分集资格。
		`INSERT INTO media(id,metadata_id,library_id,path,created_at,season_num,episode_num) SELECT 'file-'||n,CASE WHEN n IN (51,103,155) THEN 'ep-'||n ELSE 'season-'||n END,'visible','/fixture/season/'||n,'2026-01-01',n,1 FROM generate_series(1,155) n`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true, AllowedLibraryIDs: []string{"visible"}}, expiresAt: time.Now().Add(time.Hour)}}
	batches := 0
	if err := db.Callback().Row().After("gorm:row").Register("test:season-batches", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") && strings.Contains(tx.Statement.SQL.String(), "LEFT JOIN qualified") {
			batches++
		}
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"season-51", "season-103", "season-155"}
	for start := 0; start <= len(want); start++ {
		batches = 0
		result, handled, err := e.hierarchyItems(ctx, ItemsParams{UserID: "viewer", ParentID: "show", StartIndex: start, Limit: 2})
		if err != nil || !handled || result["TotalRecordCount"] != 3 {
			t.Fatalf("start=%d result=%v handled=%v err=%v", start, result, handled, err)
		}
		ids := []string{}
		for _, item := range result["Items"].([]map[string]any) {
			ids = append(ids, item["Id"].(string))
		}
		if !reflect.DeepEqual(ids, want[start:min(start+2, len(want))]) {
			t.Fatalf("start=%d ids=%v", start, ids)
		}
		if start == 0 && batches != 3 {
			t.Fatalf("first 100 raw candidates contain only one qualified season: batches=%d", batches)
		}
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true, AllowedLibraryIDs: []string{"other"}}, expiresAt: time.Now().Add(time.Hour)}}
	result, _, err := e.hierarchyItems(ctx, ItemsParams{UserID: "viewer", ParentID: "show", Limit: 2})
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("hidden seasons: result=%v err=%v", result, err)
	}
}
