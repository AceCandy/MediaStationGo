package repository

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestContinuationPlansAndMixedPagination(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	// 每个目录 2,000 部剧、4,000 集，当前用户只看过一部；其他用户有完整状态。
	statements := []string{
		`CREATE INDEX test_season_parent ON metadata_items(parent_id,season_num) WHERE kind='season'`,
		`CREATE INDEX test_episode_parent ON metadata_items(parent_id,episode_num) WHERE kind='episode'`,
		`INSERT INTO metadata_items (id,kind,title,source) SELECT 'series-'||n,'series','Show','test' FROM generate_series(1,2000) n`,
		`INSERT INTO metadata_items (id,kind,parent_id,season_num,title,source) SELECT 'season-'||n,'season','series-'||n,1,'Season','test' FROM generate_series(1,2000) n`,
		`INSERT INTO metadata_items (id,kind,parent_id,episode_num,title,source) SELECT 'ep-'||n||'-'||e,'episode','season-'||n,e,'Episode','test' FROM generate_series(1,2000) n CROSS JOIN generate_series(1,2) e`,
		`INSERT INTO media (id,metadata_id,path,created_at) SELECT 'file-'||id,id,'/test/'||id,now() FROM metadata_items WHERE kind='episode'`,
		`INSERT INTO playback_histories (id,user_id,metadata_id,media_id,completed,watched_at) VALUES ('viewer-state','viewer','ep-1-1','file-ep-1-1',true,'2026-01-01')`,
		`INSERT INTO playback_histories (id,user_id,metadata_id,media_id,position_ms,completed,watched_at) SELECT 'state-'||id,'other',id,'file-'||id,30000,false,now() FROM metadata_items WHERE kind='episode'`,
		`INSERT INTO nfo_items (id,library_id,local_key,kind,title) SELECT 'series-'||n,'nfo-library','series-'||n,'series','Show' FROM generate_series(1,2000) n`,
		`INSERT INTO nfo_items (id,library_id,local_key,kind,parent_id,season_num,title) SELECT 'season-'||n,'nfo-library','season-'||n,'season','series-'||n,1,'Season' FROM generate_series(1,2000) n`,
		`INSERT INTO nfo_items (id,library_id,local_key,kind,parent_id,episode_num,title) SELECT 'ep-'||n||'-'||e,'nfo-library','ep-'||n||'-'||e,'episode','season-'||n,e,'Episode' FROM generate_series(1,2000) n CROSS JOIN generate_series(1,2) e`,
		`INSERT INTO media (id,catalog_source,path,created_at) SELECT 'nfo-file-'||id,'nfo','/test/nfo/'||id,now() FROM nfo_items WHERE kind='episode'`,
		`INSERT INTO nfo_media_bindings (media_id,item_id,fingerprint,title) SELECT 'nfo-file-'||id,id,'','Episode' FROM nfo_items WHERE kind='episode'`,
		`INSERT INTO nfo_user_states (user_id,item_id,completed,watched_at) VALUES ('viewer','ep-1-1',true,'2026-01-02')`,
		`INSERT INTO nfo_user_states (user_id,item_id,position_ms,completed,watched_at) SELECT 'other',id,30000,false,now() FROM nfo_items WHERE kind='episode'`,
		`INSERT INTO hongguo_works (id,source_id,kind,title,related_album_id,season_index,refreshed_at) SELECT 'work-'||n,n::text,'series','Show',n::text,1,now() FROM generate_series(1,2000) n`,
		`INSERT INTO hongguo_episodes (id,work_id,number) SELECT 'ep-'||n||'-'||e,'work-'||n,e FROM generate_series(1,2000) n CROSS JOIN generate_series(1,2) e`,
		`INSERT INTO media (id,catalog_source,path,created_at) SELECT 'hg-file-'||id,'hongguo','/test/hg/'||id,now() FROM hongguo_episodes`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id,episode_id) SELECT 'hg-file-'||id,work_id,id FROM hongguo_episodes`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,completed,watched_at) VALUES ('viewer','1',1,true,'2026-01-03')`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,position_ms,completed,watched_at) SELECT 'other',w.source_id,e.number,30000,false,now() FROM hongguo_episodes e JOIN hongguo_works w ON w.id=e.work_id`,
		`ANALYZE`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := New(db).History
	filter := MediaQueryFilter{IncludeNSFW: true}
	for _, mode := range []ContinuationMode{ContinuationNextUp, ContinuationResume} {
		for _, source := range []string{"legacy", "nfo", "hongguo"} {
			q := r.continuationSource(t.Context(), "viewer", filter, source, mode, "")
			query := q.Session(&gorm.Session{DryRun: true}).Find(&[]Continuation{})
			var raw []byte
			if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.Statement.SQL.String(), query.Statement.Vars...).Row().Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan map[string]any
				Time float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(raw, &plans); err != nil {
				t.Fatal(err)
			}
			var inspect func(map[string]any)
			inspect = func(plan map[string]any) {
				if _, ok := plan["Relation Name"]; ok {
					rows, _ := plan["Actual Rows"].(float64)
					loops, _ := plan["Actual Loops"].(float64)
					removed, _ := plan["Rows Removed by Filter"].(float64)
					if (rows+removed)*loops > 50 {
						t.Fatalf("%s scanned unrelated catalog: relation=%v rows=%v loops=%v removed=%v plan=%s", source, plan["Relation Name"], rows, loops, removed, raw)
					}
				}
				children, _ := plan["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0].Plan)
			t.Logf("%s next episode: %.3f ms", source, plans[0].Time)
		}
		for start, want := range []string{"hg-episode-ep-1-2", "nfo-ep-1-2", "ep-1-2", ""} {
			rows, total, err := r.Continuations(t.Context(), "viewer", filter, mode, "", start, 1)
			if err != nil || total != 3 {
				t.Fatalf("mixed page %d: rows=%v total=%d err=%v", start, rows, total, err)
			}
			if want == "" {
				if len(rows) != 0 {
					t.Fatalf("unexpected end page: %v", rows)
				}
			} else if len(rows) != 1 || rows[0].ItemID != want {
				t.Fatalf("page %d: %v want %s", start, rows, want)
			}
		}
	}
	// 新近但已完结的剧不能占满候选窗口，导致较早的有效下一集消失。
	for i := 2; i <= 30; i++ {
		if err := db.Exec(`INSERT INTO playback_histories (id,user_id,metadata_id,media_id,completed,watched_at) VALUES (?, 'viewer', ?, ?, true, now())`, fmt.Sprint(i), fmt.Sprintf("ep-%d-2", i), fmt.Sprintf("file-ep-%d-2", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := r.Continuations(t.Context(), "viewer", filter, ContinuationNextUp, "series-1", 0, 1)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ItemID != "ep-1-2" {
		t.Fatalf("series filter/exhausted groups: %v total=%d err=%v", rows, total, err)
	}
	rows, total, err = r.Continuations(t.Context(), "viewer", filter, ContinuationNextUp, "", 2, 1)
	if err != nil || total != 3 || len(rows) != 1 || rows[0].ItemID != "ep-1-2" {
		t.Fatalf("premature history limit: %v total=%d err=%v", rows, total, err)
	}
}
