package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbyLibraryWorkTimeSort(t *testing.T) {
	for _, mode := range []string{"movie", "tv", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			svc := newTestEmbyService(t)
			db := svc.repo.DB
			libraryType := "movie"
			if mode == "tv" {
				libraryType = "tv"
			}
			lib := model.Library{Name: "Visible", Path: "/fixture/visible", Type: libraryType, Enabled: true}
			foreign := model.Library{Name: "Other", Path: "/fixture/other", Type: "movie", Enabled: true}
			for _, l := range []*model.Library{&lib, &foreign} {
				if err := svc.repo.Library.Create(t.Context(), l); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{"a", "b"} {
				kind := "movie"
				if mode == "tv" || mode == "mixed" && id == "b" {
					kind = "series"
				}
				title, release, rating, year := "Zed", "2040-01-01", 3, 1990
				if id == "b" {
					title, release, rating, year = "Alpha", "2000-01-01", 9, 2030
				}
				if err := db.Exec(`INSERT INTO metadata_items(id,kind,title,source,release_date,rating,year) VALUES (?,?,?,'local',?,?,?)`, id, kind, title, release, rating, year).Error; err != nil {
					t.Fatal(err)
				}
				itemID := id
				season, episode := 0, 0
				path := "/fixture/visible/" + id + ".mkv"
				if kind == "series" {
					if err := db.Exec(`INSERT INTO metadata_items(id,kind,parent_id,title,source,season_num) VALUES (?,'season',?,'Season','local',1)`, id+"-season", id).Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Exec(`INSERT INTO metadata_items(id,kind,parent_id,title,source,episode_num) VALUES (?,'episode',?,'Episode','local',1)`, id+"-episode", id+"-season").Error; err != nil {
						t.Fatal(err)
					}
					itemID = id + "-episode"
					season, episode = 1, 1
					path = "/fixture/国产剧/" + id + "/Season 01/episode.mkv"
				}
				date := "2026-01-01"
				if id == "b" {
					date = "2025-01-01"
				}
				if err := db.Exec(`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num,created_at) VALUES (?,?,?,?,?,?,?)`, id+"-file", itemID, lib.ID, path, season, episode, date).Error; err != nil {
					t.Fatal(err)
				}
				if id == "b" {
					if err := db.Exec(`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num,created_at) VALUES ('b-other',?,?,'/fixture/other/b.mkv',?,?,'2029-01-01')`, itemID, foreign.ID, season, episode).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			var queries []string
			var bindings [][]any
			if err := db.Callback().Row().After("gorm:row").Register("test:library-work-sort", func(tx *gorm.DB) {
				q := tx.Statement.SQL.String()
				if strings.HasPrefix(q, "WITH work_batch AS MATERIALIZED") || strings.HasPrefix(q, "WITH works AS MATERIALIZED") {
					queries = append(queries, q)
					bindings = append(bindings, append([]any(nil), tx.Statement.Vars...))
				}
			}); err != nil {
				t.Fatal(err)
			}
			if mode != "mixed" {
				svc.visibilityCache = map[string]embyVisibilityCacheEntry{svc.repo.ReadCacheKey() + "work-viewer": {visibility: MediaVisibility{AllowedLibraryIDs: []string{lib.ID}}, expiresAt: time.Now().Add(time.Hour)}}
				kind := "Movie"
				if mode == "tv" {
					kind = "Series"
				}
				for _, direction := range []string{"Ascending", "Descending"} {
					for _, skip := range []bool{false, true} {
						p := ItemsParams{UserID: "work-viewer", Recursive: true, IncludeItemTypes: []string{kind}, SortBy: "DateLastContentAdded", SortOrder: direction, Limit: 1, SkipTotalRecordCount: skip}
						latest, err := svc.Items(t.Context(), p)
						if err != nil {
							t.Fatal(err)
						}
						p.SortBy = "DateCreated,SortName"
						created, err := svc.Items(t.Context(), p)
						if err != nil || !reflect.DeepEqual(created, latest) {
							t.Fatalf("global DateCreated differs from work latest: direction=%s skip=%v err=%v", direction, skip, err)
						}
						want := "b"
						if direction == "Ascending" {
							want = "a"
						}
						items := created["Items"].([]map[string]any)
						if len(items) != 1 || items[0]["Id"] != want {
							t.Fatalf("global work sort: want=%s items=%v", want, items)
						}
					}
				}
			}
			for _, sortBy := range []string{"", "DateCreated", "DateLastContentAdded"} {
				for _, count := range []bool{true, false} {
					for _, order := range []string{"", "Ascending", "Descending"} {
						queries = nil
						bindings = nil
						p := ItemsParams{ParentID: lib.ID, SortBy: sortBy, SortOrder: order, Limit: 1, SkipTotalRecordCount: !count}
						result, err := svc.Items(t.Context(), p)
						if err != nil {
							t.Fatal(err)
						}
						items := result["Items"].([]map[string]any)
						first := "b"
						if order == "Ascending" {
							first = "a"
						}
						if len(items) != 1 || items[0]["Id"] != first {
							t.Fatalf("sort=%s order=%s count=%v items=%+v", sortBy, order, count, items)
						}
						if len(queries) == 0 {
							t.Fatal("candidate SQL not captured")
						}
						for n, q := range queries {
							if strings.Contains(q, "sort_values") || strings.Contains(q, "to_char(media.created_at") {
								t.Fatalf("library work sort still aggregates file dates: %s", q)
							}
							var raw []byte
							if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE,FORMAT JSON,TIMING OFF) "+q, bindings[n]...).Scan(&raw); err != nil {
								t.Fatal(err)
							}
							var plan []struct{ Plan map[string]any }
							if err := json.Unmarshal(raw, &plan); err != nil || len(plan) == 0 {
								t.Fatalf("plan invalid: %v", err)
							}
						}
						p.StartIndex = 2
						out, err := svc.Items(t.Context(), p)
						if err != nil || len(out["Items"].([]map[string]any)) != 0 {
							t.Fatalf("empty page=%+v err=%v", out, err)
						}
						if count && fmt.Sprint(out["TotalRecordCount"]) != "2" {
							t.Fatalf("out of range lost total: %+v", out)
						}
					}
				}
			}
			if mode == "tv" {
				out, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Recursive: true, SortBy: "DateCreated", SortOrder: "Descending", Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				items := out["Items"].([]map[string]any)
				if len(items) != 1 || items[0]["Id"] != "a-episode" {
					t.Fatalf("recursive leaf listing changed: %+v", items)
				}
			}
			for _, tc := range []struct{ sort, order, want string }{{"PremiereDate", "Descending", "a"}, {"Name", "Ascending", "b"}, {"Name", "Descending", "a"}, {"CommunityRating", "Descending", "b"}, {"ProductionYear", "Descending", "b"}, {"ProductionYear", "Ascending", "a"}} {
				queries = nil
				bindings = nil
				out, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, SortBy: tc.sort, SortOrder: tc.order, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				items := out["Items"].([]map[string]any)
				if len(items) != 1 || items[0]["Id"] != tc.want {
					t.Fatalf("explicit sort=%s items=%+v", tc.sort, items)
				}
				if tc.sort == "ProductionYear" {
					for n, q := range queries {
						var raw []byte
						if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE,FORMAT JSON,TIMING OFF) "+q, bindings[n]...).Scan(&raw); err != nil {
							t.Fatal(err)
						}
						var plan []struct {
							ExecutionTime float64 `json:"Execution Time"`
						}
						if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
							t.Fatalf("year plan invalid: %v", err)
						}
						t.Logf("year order=%s execution_ms=%.3f", tc.order, plan[0].ExecutionTime)
						if strings.Contains(q, "sort_values") {
							t.Fatalf("year sort should not aggregate file dates: %s", q)
						}
					}
					out, err = svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, SortBy: tc.sort, SortOrder: tc.order, StartIndex: 1, Limit: 1})
					if err != nil {
						t.Fatal(err)
					}
					tail := out["Items"].([]map[string]any)
					if len(tail) != 1 || tail[0]["Id"] == tc.want || fmt.Sprint(out["TotalRecordCount"]) != "2" {
						t.Fatalf("year tail=%+v", out)
					}
				}
			}
		})
	}
}
