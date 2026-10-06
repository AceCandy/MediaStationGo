package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoPageNodesPreservePayloadsAndBoundWorkReads(t *testing.T) {
	db := newServiceTestDB(t, model.AllModels()...)
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	e.visibilityCache = map[string]embyVisibilityCacheEntry{}
	for _, user := range []string{"viewer", "other"} {
		e.visibilityCache[e.repo.ReadCacheKey()+user] = embyVisibilityCacheEntry{
			visibility: MediaVisibility{AllowedLibraryIDs: []string{"visible"}, HiddenLibraryIDs: []string{"hidden"}},
			expiresAt:  time.Now().Add(time.Hour),
		}
	}
	// 多季、多版本与隐藏的首季；失效文件断点由可见替代版本推导完成。
	for _, sql := range []string{
		`INSERT INTO hongguo_works (id,source_id,kind,title,related_album_id,season_index,overview,tags,rating,refreshed_at)
SELECT 'w-'||n,n::text,'series','Title '||n,((n-1)/3+1)::text,(n-1)%3+1,'Overview '||n,'["Genre"]',n,now() FROM generate_series(1,90) n`,
		`UPDATE hongguo_works SET related_album_id='90',season_index=1 WHERE id='w-90'`,
		`UPDATE hongguo_works SET related_album_id='',season_index=0 WHERE id='w-89'`,
		`INSERT INTO hongguo_episodes (id,work_id,number)
SELECT 'ep-'||n||'-'||ep,'w-'||n,ep FROM generate_series(1,90) n CROSS JOIN generate_series(1,30) ep`,
		`INSERT INTO media (id,library_id,catalog_source,path,episode_num,created_at)
SELECT 'f-'||n||'-'||ep||'-'||v,CASE WHEN n=1 THEN 'hidden' ELSE 'visible' END,'hongguo','/test/'||n||'/'||ep||'/'||v,ep,
 TIMESTAMP '2026-01-01' + n*INTERVAL '1 day' + v*INTERVAL '1 hour'
FROM generate_series(1,90) n CROSS JOIN generate_series(1,30) ep CROSS JOIN generate_series(1,2) v`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id,episode_id)
SELECT 'f-'||n||'-'||ep||'-'||v,'w-'||n,'ep-'||n||'-'||ep
FROM generate_series(1,90) n CROSS JOIN generate_series(1,30) ep CROSS JOIN generate_series(1,2) v`,
		`INSERT INTO media_probe_metadata (media_id,duration_ms,width,size_bytes,probe_json,schema_version,summary_version,probed_at) SELECT id,90000,1920,1000,'{}',1,1,now() FROM media`,
		`INSERT INTO hongguo_artworks (id,work_id,source_url,local_key)
SELECT 'art-'||n,'w-'||n,'https://example.invalid/poster','test/poster-'||n FROM generate_series(1,90) n`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,media_id,position_ms,duration_ms,completed,watched_at)
SELECT 'viewer',n::text,ep,CASE WHEN ep%3=1 THEN 'gone' ELSE 'f-'||n||'-'||ep||'-1' END,
 CASE WHEN ep%3=1 THEN 80000 ELSE 10000 END,120000,ep%3=0,TIMESTAMP '2026-02-01' + ep*INTERVAL '1 hour'
FROM generate_series(1,90) n CROSS JOIN generate_series(1,30) ep`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,position_ms,duration_ms,completed)
SELECT 'viewer','unrelated-'||n,1,0,0,true FROM generate_series(1,25000) n`,
		`INSERT INTO hongguo_favorites (user_id,item_id,favorite) VALUES ('viewer','hg-group-1',true),('viewer','hg-group-90',true)`,
		`ANALYZE hongguo_works`, `ANALYZE media`, `ANALYZE hongguo_media_bindings`, `ANALYZE hongguo_user_states`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}

	for _, user := range []string{"viewer", "other"} {
		for _, ids := range [][]string{
			{"hg-group-1"}, {"hg-group-90"}, {"hg-season-w-2"}, {"hg-episode-ep-2-1"},
			{"hg-group-1", "hg-season-w-2", "hg-episode-ep-2-1", "hg-group-90"},
			{"hg-work-w-89"}, {"hg-work-w-2"}, {"hg-group-missing"},
		} {
			var original []hongGuoNode
			if err := e.hongGuoItemNodes(t.Context(), user, ids...).Where("id IN ?", ids).Scan(&original).Error; err != nil {
				t.Fatal(err)
			}
			for _, fields := range [][]string{nil, {"BasicSyncInfo"}} {
				expected, err := e.hongGuoNodePayloads(t.Context(), original, user, fields)
				if err != nil {
					t.Fatal(err)
				}
				byID := map[any]map[string]any{}
				for _, item := range expected {
					byID[item["Id"]] = item
				}
				want := []map[string]any{}
				for _, id := range ids {
					if item, ok := byID[id]; ok {
						want = append(want, item)
					}
				}
				got, err := e.globalItemPayloads(t.Context(), ids, ItemsParams{UserID: user, Fields: fields})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("user=%s scope=%v fields=%v payload differs\ngot=%+v\nwant=%+v", user, ids, fields, got, want)
				}
			}
		}
	}
	// 空分集及归属不匹配的绑定仍不能生成有效分集候选。
	for _, sql := range []string{
		`UPDATE hongguo_media_bindings SET episode_id=NULL WHERE media_id='f-2-2-2'`,
		`UPDATE hongguo_media_bindings SET episode_id='ep-3-1' WHERE media_id='f-2-1-2'`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Latest 的分集候选与原全层级投影对照，覆盖多版本和隐藏文件。
	for _, user := range []string{"viewer", "other"} {
		type candidate struct {
			ID, Kind, Title     string
			CreatedAt, LatestAt time.Time
			Favorite            bool
			Rating              float32
		}
		p := ItemsParams{UserID: user, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
		var want, got []candidate
		if err := e.hongGuoNodes(t.Context(), user, "").Where("kind = 'Episode'").
			Select("id,LOWER(kind) AS kind,title,file_latest_at AS created_at,latest_at,favorite,rating").Order("id").Scan(&want).Error; err != nil {
			t.Fatal(err)
		}
		if err := e.hongGuoGlobalCandidates(t.Context(), p).
			Select("id,kind,title,created_at,latest_at,favorite,rating").Order("id").Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("latest candidates differ for user=%s: got=%d want=%d", user, len(got), len(want))
		}
	}
	// 捕获共享补全入口真正执行的作品查询，含大量无关历史，验证读取范围。
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:work-hydration", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH page_works AS MATERIALIZED") {
			queries = append(queries, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Row().Remove("test:work-hydration")
	if _, err := e.globalItemPayloads(t.Context(), []string{"hg-group-1", "hg-group-2"}, ItemsParams{UserID: "viewer", Fields: []string{"BasicSyncInfo"}}); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Fatalf("work hydration queries=%d", len(queries))
	}
	var raw []byte
	if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+queries[0].sql, queries[0].vars...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plans []struct {
		Plan          map[string]any
		ExecutionTime float64 `json:"Execution Time"`
		JIT           struct{ Functions int }
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	visits, loops := map[string]float64{}, map[string]float64{}
	var inspect func(map[string]any)
	inspect = func(n map[string]any) {
		rows, _ := n["Actual Rows"].(float64)
		removed, _ := n["Rows Removed by Filter"].(float64)
		count, _ := n["Actual Loops"].(float64)
		if relation, ok := n["Relation Name"].(string); ok {
			visits[relation] += (rows + removed) * count
			loops[relation] += count
		}
		children, _ := n["Plans"].([]any)
		for _, child := range children {
			inspect(child.(map[string]any))
		}
	}
	inspect(plans[0].Plan)
	if visits["hongguo_user_states"] > 600 || loops["hongguo_user_states"] > 20 || visits["hongguo_artworks"] > 100 || visits["hongguo_works"] > 3000 || plans[0].JIT.Functions != 0 {
		t.Fatalf("unbounded work hydration: visits=%v loops=%v JIT=%d", visits, loops, plans[0].JIT.Functions)
	}
	t.Logf("work hydration %.3f ms visits=%v loops=%v", plans[0].ExecutionTime, visits, loops)
	// 检查 Latest 候选的实际计划，分集资料不应对每个文件逐行探测。
	q := e.hongGuoGlobalCandidates(t.Context(), ItemsParams{UserID: "viewer", SortBy: "DateLastContentAdded"}).
		Order("latest_at DESC NULLS LAST,id DESC").Limit(50)
	stmt := q.Session(&gorm.Session{DryRun: true}).Find(&[]hongGuoNode{}).Statement
	if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+stmt.SQL.String(), stmt.Vars...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	plans = nil
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode latest plan: %v", err)
	}
	visits, loops = map[string]float64{}, map[string]float64{}
	inspect(plans[0].Plan)
	if visits["hongguo_episodes"] > 5400 || loops["hongguo_episodes"] > 4 {
		t.Fatalf("latest probes episodes per file: visits=%v loops=%v", visits, loops)
	}
	t.Logf("latest candidates %.3f ms visits=%v loops=%v", plans[0].ExecutionTime, visits, loops)
}
