package service

import (
	"encoding/json"
	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"strings"
	"testing"
	"time"
)

func TestHuangGuoAIWorkPagePlanAndExactCounts(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "Synthetic plan", Path: "/synthetic/plan", Type: model.LibraryTypeHuangGuoAI}
	if err = db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO huangguoai_works(id,source_id,source_category,kind,title,refreshed_at) SELECT 'hga-w-'||n,n::text,'ai-duanju','series','Synthetic '||lpad(n::text,4,'0'),now() FROM generate_series(1,2000) n`,
		`INSERT INTO huangguoai_episodes(id,work_id,number,page_path) SELECT 'hga-ep-'||n||'-'||ep,'hga-w-'||n,ep,'/synthetic/' FROM generate_series(1,2000) n CROSS JOIN generate_series(1,2) ep`,
		`INSERT INTO media(id,library_id,catalog_source,lookup_catalog_id,path,season_num,episode_num) SELECT 'hga-file-'||n||'-'||ep,?,'huangguoai',n::text,'/synthetic/'||n||'-'||ep,1,ep FROM generate_series(1,2000) n CROSS JOIN generate_series(1,2) ep`,
		`INSERT INTO huangguoai_media_bindings(media_id,work_id,episode_id) SELECT 'hga-file-'||n||'-'||ep,'hga-w-'||n,'hga-ep-'||n||'-'||ep FROM generate_series(1,2000) n CROSS JOIN generate_series(1,2) ep`,
	} {
		var args []any
		if strings.Contains(q, "?") {
			args = []any{lib.ID}
		}
		if err = db.Exec(q, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"huangguoai_works", "huangguoai_episodes", "huangguoai_media_bindings", "media"} {
		if err = db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	e.visibilityCache = map[string]embyVisibilityCacheEntry{repos.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	type query struct {
		sql  string
		vars []any
	}
	queries := []query{}
	if err = db.Callback().Row().After("gorm:row").Register("test:hga-plan", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.HasPrefix(sql, "WITH work_batch AS MATERIALIZED") || strings.HasPrefix(sql, "SELECT * FROM (SELECT n.id") {
			queries = append(queries, query{sql, append([]any{}, tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Row().Remove("test:hga-plan")
	for _, offset := range []int{0, 50, 1999, 2000} {
		page, err := e.Items(t.Context(), ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 50, StartIndex: offset, SortBy: "SortName"})
		if err != nil {
			t.Fatal(err)
		}
		want := min(50, 2000-offset)
		if page["TotalRecordCount"] != int64(2000) || len(page["Items"].([]map[string]any)) != want {
			t.Fatalf("offset=%d total=%v", offset, page["TotalRecordCount"])
		}
	}
	global, err := e.Items(t.Context(), ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, Limit: 50, StartIndex: 50, SortBy: "SortName"})
	if err != nil || global["TotalRecordCount"] != int64(2000) || len(global["Items"].([]map[string]any)) != 50 {
		t.Fatal("global work page", err)
	}
	latest, err := e.LatestItems(t.Context(), "viewer", lib.ID, 50, false)
	if err != nil || len(latest) != 50 {
		t.Fatal("latest work page", err)
	}
	media := &MediaService{repo: repos}
	cards, total, err := media.ListLibrarySeriesCards(t.Context(), lib.ID, 2, 50, "", "", MediaVisibility{IncludeNSFW: true})
	if err != nil || total != 2000 || len(cards) != 50 {
		t.Fatal("Web work page", total, err)
	}
	if len(queries) == 0 {
		t.Fatal("no public page queries captured")
	}
	for _, q := range queries {
		var raw []byte
		if err = db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+q.sql, q.vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plans []struct {
			Plan map[string]any
			JIT  json.RawMessage
		}
		if err = json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
			t.Fatal("invalid plan", err)
		}
		var visits float64
		var inspect func(map[string]any)
		inspect = func(n map[string]any) {
			if relation, _ := n["Relation Name"].(string); relation == "media" || relation == "huangguoai_media_bindings" || relation == "huangguoai_episodes" {
				rows, _ := n["Actual Rows"].(float64)
				loops, _ := n["Actual Loops"].(float64)
				visits += rows * loops
			}
			if children, ok := n["Plans"].([]any); ok {
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
		}
		inspect(plans[0].Plan)
		if strings.HasPrefix(q.sql, "SELECT * FROM (SELECT n.id") && visits > 1000 {
			t.Fatalf("page hydration scanned beyond 50 works: %.0f visits", visits)
		}
		t.Logf("public page plan: visits=%.0f, JIT=%t", visits, len(plans[0].JIT) > 0)
	}
}
