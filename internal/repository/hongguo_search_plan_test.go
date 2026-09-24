package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestHongGuoSearchBoundsFileWork(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	db := repos.DB
	// 200 部作品各有 200 个文件；搜索只命中一个合集，不能展开全部 40,000 个文件。
	// 文件保留名称载荷，避免全空窄行让小型测试库偏向廉价顺序扫描。
	for _, sql := range []string{
		`INSERT INTO hongguo_works (id,source_id,kind,title,related_album_id,season_index,refreshed_at)
SELECT 'work-'||n,n::text,'series',CASE WHEN n=1 THEN 'Plan target' ELSE 'Other' END,n::text,1,now() FROM generate_series(1,200) n`,
		`INSERT INTO hongguo_works (id,source_id,kind,title,refreshed_at)
SELECT 'work-'||n,n::text,'series','Unavailable',now() FROM generate_series(201,10000) n`,
		`INSERT INTO media (id,library_id,catalog_source,path,episode_num,scan_title)
SELECT 'file-'||n||'-'||e,'library','hongguo','/test/'||n||'/'||e,e,repeat('File metadata ',16) FROM generate_series(1,200) n CROSS JOIN generate_series(1,200) e`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id)
SELECT 'file-'||n||'-'||e,'work-'||n FROM generate_series(1,200) n CROSS JOIN generate_series(1,200) e`,
		`ANALYZE hongguo_works`, `ANALYZE hongguo_media_bindings`, `ANALYZE media`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var statements []statement
	capture := func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.HasPrefix(sql, "SELECT") && strings.Contains(sql, "hongguo_media_bindings") &&
			(strings.Contains(sql, "AS search_metadata") || strings.HasPrefix(sql, "SELECT DISTINCT ON (CASE WHEN g.id")) {
			statements = append(statements, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:hongguo-search-plan", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:hongguo-search-plan", capture); err != nil {
		t.Fatal(err)
	}
	backend := &hongGuoSearchTestBackend{ids: []string{"hg-group-1"}}
	repos.HongGuo.SetSearchBackend(backend)
	filter := MediaQueryFilter{IncludeNSFW: true}
	rows, err := repos.HongGuo.SearchCandidates(t.Context(), "Plan target", MetadataSearchFilter{MediaQueryFilter: filter, Kinds: []string{"series"}})
	if err != nil || len(rows) != 1 || rows[0].ID != "hg-group-1" {
		t.Fatalf("candidates=%v err=%v", rows, err)
	}
	if len(backend.filters) != 1 || len(backend.filters[0].CandidateIDs) != 200 {
		t.Fatal("OpenSearch lost the pre-limit visible work scope")
	}
	views, err := repos.MediaView.hongGuoSearchRepresentatives(t.Context(), backend.ids, filter)
	if err != nil || len(views) != 1 || views[0].ID != "file-1-1" || views[0].SeriesID != "hg-group-1" {
		t.Fatalf("representatives=%v err=%v", views, err)
	}
	if len(statements) != 3 {
		t.Fatalf("captured %d queries, want visible IDs, revalidation and representatives", len(statements))
	}
	for i, query := range statements {
		t.Run([]string{"visible_ids", "revalidation", "representatives"}[i], func(t *testing.T) {
			// 保留原始参数绑定，不能执行日志中不可用的 Go 数组插值。
			var raw []byte
			if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query.sql, query.vars...).Row().Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan map[string]any
			}
			if err := json.Unmarshal(raw, &plans); err != nil {
				t.Fatal(err)
			}
			var inspect func(map[string]any)
			inspect = func(plan map[string]any) {
				if relation := plan["Relation Name"]; relation == "media" || relation == "hongguo_media_bindings" {
					rows, _ := plan["Actual Rows"].(float64)
					loops, _ := plan["Actual Loops"].(float64)
					removed, _ := plan["Rows Removed by Filter"].(float64)
					if (rows+removed)*loops > 1000 {
						t.Fatalf("expanded unrelated files: relation=%v rows=%v loops=%v removed=%v plan=%s", relation, rows, loops, removed, raw)
					}
				}
				children, _ := plan["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0].Plan)
		})
	}
}
