package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestContinuationLongWatchedSeriesLookup(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	// 当前用户看过九部长剧的前 239 集；另有大量无关作品及其他用户状态。
	for _, sql := range []string{
		`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at)
SELECT 'work-'||n,n::text,'series','Show',CASE WHEN n=9 THEN '' ELSE n::text END,1,now() FROM generate_series(1,200) n`,
		`INSERT INTO hongguo_episodes(id,work_id,number) SELECT 'ep-'||n||'-'||e,'work-'||n,e FROM generate_series(1,200) n CROSS JOIN generate_series(1,240) e`,
		`INSERT INTO media(id,catalog_source,library_id,path) SELECT 'file-'||id,'hongguo','visible','/test/'||id FROM hongguo_episodes`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) SELECT 'file-'||id,work_id,id FROM hongguo_episodes`,
		`INSERT INTO hongguo_user_states(user_id,source_id,episode_number,media_id,completed,watched_at)
SELECT 'viewer',n::text,e,'file-ep-'||n||'-'||e,true,'2026-01-01'::timestamptz+n*interval '1 day' FROM generate_series(1,9) n CROSS JOIN generate_series(1,239) e`,
		`INSERT INTO hongguo_user_states(user_id,source_id,episode_number,media_id,completed,watched_at)
SELECT 'other',w.source_id,e.number,'file-'||e.id,true,now() FROM hongguo_episodes e JOIN hongguo_works w ON w.id=e.work_id`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) VALUES ('short','9001','series','Short',now())`,
		`INSERT INTO media(id,catalog_source,library_id,path) VALUES ('short-file','hongguo','visible','/test/short')`,
		`INSERT INTO hongguo_episodes(id,work_id,number) VALUES ('short-ep','short',1)`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) VALUES ('short-file','short','short-ep')`,
		`INSERT INTO hongguo_user_states(user_id,source_id,episode_number,media_id,position_ms,duration_ms,watched_at)
VALUES ('viewer','9001',1,'short-file',60000,120000,'2026-02-01')`,
		`ANALYZE`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var querySQL string
	var queryVars []any
	if err := db.Callback().Row().After("gorm:row").Register("test:continuation-lookup", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.Contains(sql, "watched AS MATERIALIZED") && !strings.HasPrefix(sql, "EXPLAIN") {
			querySQL, queryVars = sql, append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	r := New(db).History
	for _, tc := range []struct {
		name, series, first string
		mode                ContinuationMode
		count, visits       int
	}{
		{"resume", "", "hg-episode-short-ep", ContinuationResume, 10, 20000},
		{"next", "", "hg-episode-ep-9-240", ContinuationNextUp, 9, 20000},
		{"web", "", "hg-episode-short-ep", ContinuationWeb, 10, 20000},
		{"group", "hg-group-1", "hg-episode-ep-1-240", ContinuationNextUp, 1, 2500},
		{"work", "hg-work-work-9", "hg-episode-ep-9-240", ContinuationNextUp, 1, 2500},
		{"empty", "hg-group-200", "", ContinuationNextUp, 0, 0},
	} {
		querySQL, queryVars = "", nil
		rows, total, err := r.Continuations(t.Context(), "viewer", MediaQueryFilter{}, tc.mode, tc.series, 0, 30)
		if err != nil || len(rows) != tc.count || (tc.mode != ContinuationWeb && total != int64(tc.count)) {
			t.Fatalf("%s rows=%d total=%d err=%v", tc.name, len(rows), total, err)
		}
		if len(rows) > 0 && rows[0].ItemID != tc.first {
			t.Fatalf("%s first=%s want=%s", tc.name, rows[0].ItemID, tc.first)
		}
		if querySQL == "" {
			t.Fatal("missing actual continuation query")
		}
		for _, generic := range []bool{false, true} {
			var raw []byte
			if generic {
				if err := db.Exec("SET plan_cache_mode = force_generic_plan").Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Exec("PREPARE continuation_lookup AS " + querySQL).Error; err != nil {
					t.Fatal(err)
				}
				args := make([]string, len(queryVars))
				for i := range args {
					args[i] = fmt.Sprintf("$%d", i+1)
				}
				execute := db.Dialector.Explain("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE continuation_lookup("+strings.Join(args, ",")+")", queryVars...)
				err = db.Raw(execute).Row().Scan(&raw)
			} else {
				err = db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+querySQL, queryVars...).Scan(&raw)
			}
			if err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan map[string]any
				JIT  json.RawMessage
				Time float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(raw, &plans); err != nil {
				t.Fatal(err)
			}
			visits := map[string]float64{}
			joinChecks := float64(0)
			var inspect func(map[string]any)
			inspect = func(node map[string]any) {
				loops, _ := node["Actual Loops"].(float64)
				joinRemoved, _ := node["Rows Removed by Join Filter"].(float64)
				joinChecks += joinRemoved * loops
				if relation, ok := node["Relation Name"].(string); ok {
					rows, _ := node["Actual Rows"].(float64)
					removed, _ := node["Rows Removed by Filter"].(float64)
					rechecked, _ := node["Rows Removed by Index Recheck"].(float64)
					visits[relation] += (rows + removed + rechecked) * loops
				}
				children, _ := node["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0].Plan)
			t.Logf("%s generic=%t time=%.3fms visits=%v join_checks=%.0f jit=%s", tc.name, generic, plans[0].Time, visits, joinChecks, plans[0].JIT)
			for _, table := range []string{"hongguo_episodes", "hongguo_media_bindings", "media"} {
				if visits[table] > float64(tc.visits) {
					t.Fatalf("%s generic=%t %s visits=%.0f exceeds %d", tc.name, generic, table, visits[table], tc.visits)
				}
			}
			if joinChecks > float64(tc.visits) {
				t.Fatalf("%s generic=%t join checks=%.0f exceeds %d", tc.name, generic, joinChecks, tc.visits)
			}
			// 全局查询受用户分布和预编译估算影响；指定单剧必须先收窄到不触发 JIT。
			if tc.series != "" && len(plans[0].JIT) != 0 {
				t.Fatalf("%s generic=%t scoped lookup triggered JIT", tc.name, generic)
			}
			if generic {
				if err := db.Exec("DEALLOCATE continuation_lookup").Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Exec("SET plan_cache_mode = auto").Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
