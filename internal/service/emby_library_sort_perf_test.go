package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbyLibrarySortPlans(t *testing.T) {
	for _, series := range []bool{false, true} {
		t.Run(fmt.Sprint(series), func(t *testing.T) {
			e := newTestEmbyService(t)
			db := e.repo.DB
			kind := "movie"
			if series {
				kind = "series"
			}
			lib := model.Library{Name: "Sort fixture", Type: kind, Path: "/fixture/sort"}
			if err := db.Create(&lib).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`INSERT INTO metadata_items(id,kind,title,source,release_date,year)
SELECT 'work-'||n,?, LPAD(n::text,5,'0'),'local',to_char(DATE '2000-01-01'+n,'YYYY-MM-DD'),2000+n%20 FROM generate_series(1,4000) n`, kind).Error; err != nil {
				t.Fatal(err)
			}
			identity := "'work-'||n"
			season, episode := 0, 0
			if series {
				for _, sql := range []string{
					`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) SELECT 'season-'||n,'season','Season','local','work-'||n,1 FROM generate_series(1,4000) n`,
					`INSERT INTO metadata_items(id,kind,title,source,parent_id,episode_num) SELECT 'episode-'||n||'-'||v,'episode','Episode','local','season-'||n,v FROM generate_series(1,4000) n CROSS JOIN generate_series(1,20) v`,
				} {
					if err := db.Exec(sql).Error; err != nil {
						t.Fatal(err)
					}
				}
				identity = "'episode-'||n||'-'||v"
				season, episode = 1, 1
			}
			sql := `INSERT INTO media(id,metadata_id,library_id,path,created_at,season_num,episode_num,scan_title)
SELECT 'file-'||n||'-'||v,` + identity + `,?,'/fixture/sort/'||n||'/'||v,TIMESTAMP '2026-01-01'+n*INTERVAL '1 day'+v*INTERVAL '1 hour',?,?,LPAD(n::text,5,'0') FROM generate_series(1,4000) n CROSS JOIN generate_series(1,20) v`
			if err := db.Exec(sql, lib.ID, season, episode).Error; err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"media", "metadata_items"} {
				if err := db.Exec("ANALYZE " + table).Error; err != nil {
					t.Fatal(err)
				}
			}
			type query struct {
				sql  string
				args []any
			}
			var queries []query
			if err := db.Callback().Row().After("gorm:row").Register("test:sort-plans", func(tx *gorm.DB) {
				sql := tx.Statement.SQL.String()
				if strings.HasPrefix(sql, "WITH work_batch") || strings.HasPrefix(sql, "WITH work_candidates") {
					queries = append(queries, query{sql, append([]any(nil), tx.Statement.Vars...)})
				}
			}); err != nil {
				t.Fatal(err)
			}
			type node struct {
				Kind     string  `json:"Node Type"`
				Relation string  `json:"Relation Name"`
				Rows     float64 `json:"Actual Rows"`
				Filtered float64 `json:"Rows Removed by Filter"`
				Loops    float64 `json:"Actual Loops"`
				Plans    []node  `json:"Plans"`
			}
			for _, sortBy := range []string{"SortName", "PremiereDate"} {
				p := ItemsParams{ParentID: lib.ID, SortBy: sortBy, SortOrder: "Ascending", SkipTotalRecordCount: true, Limit: 120}
				queries = nil
				files := e.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}).Where("media.library_id=?", lib.ID), p.UserID)
				begin := time.Now()
				if series {
					rows, _, err := e.seriesWorkPage(t.Context(), files, p, 0, 120)
					if err != nil || len(rows) != 120 {
						t.Fatalf("series page: %d %v", len(rows), err)
					}
				} else {
					rows, _, err := e.metadataWorkPage(t.Context(), files, p, true)
					if err != nil || len(rows) != 120 {
						t.Fatalf("movie page: %d %v", len(rows), err)
					}
				}
				t.Logf("sort=%s service_ms=%.3f candidate_queries=%d", sortBy, float64(time.Since(begin).Microseconds())/1000, len(queries))
				if sortBy == "SortName" && len(queries) != 1 {
					t.Errorf("name ordering replayed candidates: %d", len(queries))
				}
				for i, q := range queries {
					var raw []byte
					if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE,FORMAT JSON,TIMING OFF) "+q.sql, q.args...).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var plan []struct {
						Plan node
						Time float64 `json:"Execution Time"`
					}
					if err := json.Unmarshal(raw, &plan); err != nil {
						t.Fatal(err)
					}
					mediaVisits, metadataVisits := 0.0, 0.0
					var walk func(node)
					walk = func(n node) {
						if n.Relation == "media" {
							mediaVisits += (n.Rows + n.Filtered) * n.Loops
						}
						if n.Relation == "metadata_items" {
							metadataVisits += (n.Rows + n.Filtered) * n.Loops
						}
						for _, p := range n.Plans {
							walk(p)
						}
					}
					walk(plan[0].Plan)
					t.Logf("sort=%s query=%d plan_ms=%.3f media_visits=%.0f metadata_visits=%.0f", sortBy, i, plan[0].Time, mediaVisits, metadataVisits)
					if sortBy == "PremiereDate" && mediaVisits > 8000 {
						t.Errorf("distinct release/year ordering reads all file dates: %.0f", mediaVisits)
					}
				}
			}
		})
	}
}
