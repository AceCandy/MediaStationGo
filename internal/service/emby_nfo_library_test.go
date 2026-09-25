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
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func nfoBrowseFixture(t *testing.T, works, episodes int) *EmbyService {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "NFO", Type: model.LibraryTypeNFOTV, Path: "/fixture/nfo"}
	lib.ID = "library-nfo"
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO nfo_items (id,library_id,local_key,kind,title) SELECT 'show-'||n,'library-nfo','show-'||n,'series','Title '||n FROM generate_series(1,?) n`, []any{works}},
		{`INSERT INTO nfo_items (id,library_id,local_key,kind,title,parent_id,season_num) SELECT 'season-'||n,'library-nfo','season-'||n,'season','Season','show-'||n,1 FROM generate_series(1,?) n`, []any{works}},
		{`INSERT INTO nfo_items (id,library_id,local_key,kind,title,parent_id,episode_num) SELECT 'ep-'||s||'-'||n,'library-nfo','ep-'||s||'-'||n,'episode','Episode','season-'||s,n FROM generate_series(1,?) s CROSS JOIN generate_series(1,?) n`, []any{works, episodes}},
		{`INSERT INTO media (id,library_id,path,catalog_source,created_at,scan_title) SELECT 'file-'||i.id||'-'||v,'library-nfo','/fixture/'||i.id||'-'||v,'nfo',TIMESTAMP '2026-01-01' + i.episode_num * INTERVAL '1 hour',repeat('metadata ',20) FROM nfo_items i CROSS JOIN generate_series(1,2) v WHERE kind='episode'`, nil},
		{`INSERT INTO nfo_media_bindings (media_id,item_id,title,fingerprint) SELECT 'file-'||i.id||'-'||v,i.id,'Episode','fixture' FROM nfo_items i CROSS JOIN generate_series(1,2) v WHERE kind='episode'`, nil},
	} {
		if err := db.Exec(query.sql, query.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	return e
}

func TestNFOLibraryPagingMatchesHierarchy(t *testing.T) {
	e := nfoBrowseFixture(t, 3, 2)
	ctx, db := t.Context(), e.repo.DB
	if err := db.Exec(`UPDATE media SET created_at = created_at + CASE WHEN id LIKE '%ep-1-%' THEN INTERVAL '10 days' WHEN id LIKE '%ep-2-2-%' THEN INTERVAL '20 days' ELSE INTERVAL '0 days' END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO nfo_user_states (user_id,item_id,completed) SELECT 'viewer',id,TRUE FROM nfo_items WHERE parent_id='season-1'`).Error; err != nil {
		t.Fatal(err)
	}
	for _, visibility := range []MediaVisibility{{IncludeNSFW: true}, {}, {HiddenLibraryIDs: []string{"library-nfo"}}, {LibraryRestricted: true}, {AllowedLibraryIDs: []string{"library-nfo"}}} {
		e.visibilityCache["viewer"] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		if err := db.Model(&model.NFOItem{}).Where("id = 'season-3'").Update("nsfw", true).Error; err != nil {
			t.Fatal(err)
		}
		for _, sortBy := range []string{"SortName", "DateCreated", "DateLastContentAdded"} {
			for _, filters := range [][]string{nil, {"IsPlayed"}, {"IsUnplayed"}} {
				q := e.nfoNodes(ctx, "viewer", "library-nfo").Where("parent_id = ''")
				if containsEmbyFilter(filters, "IsPlayed") {
					q = q.Where("played")
				}
				if containsEmbyFilter(filters, "IsUnplayed") {
					q = q.Where("NOT played")
				}
				order := "title DESC"
				if sortBy == "DateCreated" {
					order = "created_at DESC"
				}
				var old []hongGuoNode
				if err := q.Order("season_number, episode_number").Order(order).Order("id").Scan(&old).Error; err != nil {
					t.Fatal(err)
				}
				for offset := 0; offset <= len(old); offset++ {
					end := min(offset+1, len(old))
					want, err := e.nfoNodePayloads(ctx, old[offset:end], "viewer", nil)
					if err != nil {
						t.Fatal(err)
					}
					p := ItemsParams{UserID: "viewer", ParentID: "library-nfo", SortBy: sortBy, SortOrder: "Descending", Filters: filters, StartIndex: offset, Limit: 1}
					got, err := e.Items(ctx, p)
					if err != nil || got["TotalRecordCount"] != int64(len(old)) || !reflect.DeepEqual(got["Items"], want) {
						t.Fatalf("sort=%s filter=%v offset=%d got=%v want=%v err=%v", sortBy, filters, offset, got, want, err)
					}
				}
				if sortBy == "DateCreated" && len(filters) > 0 {
					want, err := e.nfoNodePayloads(ctx, old[:min(1, len(old))], "viewer", nil)
					if err != nil {
						t.Fatal(err)
					}
					got, err := e.LatestItems(ctx, "viewer", "library-nfo", 1, filters[0] == "IsPlayed")
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("Latest got=%v want=%v err=%v", got, want, err)
					}
				}
			}
		}
	}
	for offset := 0; offset < 4; offset++ {
		views, summaries, total, err := e.repo.MediaView.ListLibraryMetadataPage(ctx, "library-nfo", "series", "", offset, 1, repository.MediaQueryFilter{IncludeNSFW: true})
		if err != nil || total != 3 || len(views) != len(summaries) {
			t.Fatalf("Web page %d total=%d err=%v", offset, total, err)
		}
		if offset < 3 && (len(summaries) != 1 || summaries[0].Count != 2 || summaries[0].VersionCount != 4) || offset == 3 && len(summaries) != 0 {
			t.Fatalf("Web summaries=%v", summaries)
		}
	}
}

func TestNFOLibraryMovieVersionsAndFilters(t *testing.T) {
	e := nfoBrowseFixture(t, 0, 0)
	db, ctx := e.repo.DB, t.Context()
	for _, sql := range []string{
		`INSERT INTO nfo_items (id,library_id,local_key,kind,title) VALUES ('movie','library-nfo','movie','movie','Movie')`,
		`INSERT INTO media (id,library_id,path,catalog_source,created_at) VALUES ('file-a','library-nfo','/fixture/a','nfo','2026-01-01'),('file-b','library-nfo','/fixture/b','nfo','2026-02-01')`,
		`INSERT INTO nfo_media_bindings (media_id,item_id,title,fingerprint) VALUES ('file-a','movie','Movie','fixture'),('file-b','movie','电影','fixture')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var old []hongGuoNode
	if err := e.nfoNodes(ctx, "viewer", "library-nfo").Where("parent_id = ''").Scan(&old).Error; err != nil {
		t.Fatal(err)
	}
	want, err := e.nfoNodePayloads(ctx, old, "viewer", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, total, err := e.nfoLibraryItems(ctx, ItemsParams{UserID: "viewer", ParentID: "library-nfo", Limit: 1, IncludeItemTypes: []string{"Movie"}}, true)
	if err != nil || total != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("movie got=%v total=%d want=%v err=%v", got, total, want, err)
	}
	for _, missingChinese := range []bool{false, true} {
		views, summaries, total, err := e.repo.MediaView.ListLibraryMetadataPage(ctx, "library-nfo", "movie", "", 0, 1, repository.MediaQueryFilter{IncludeNSFW: true, MissingPoster: true, MissingChineseTitle: missingChinese})
		versions := 2
		if missingChinese {
			versions = 1
		}
		if err != nil || total != 1 || len(views) != 1 || len(summaries) != 1 || summaries[0].VersionCount != versions {
			t.Fatalf("missingChinese=%v views=%v summaries=%v total=%d err=%v", missingChinese, views, summaries, total, err)
		}
	}
}

func TestNFOLibraryPagePlans(t *testing.T) {
	e := nfoBrowseFixture(t, 1000, 50)
	db := e.repo.DB
	for _, table := range []string{"media", "nfo_items", "nfo_media_bindings"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:nfo-pages", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if !strings.HasPrefix(sql, "EXPLAIN") && (strings.HasPrefix(sql, "WITH candidates") || strings.HasPrefix(sql, "WITH works") || strings.Contains(sql, "COUNT(DISTINCT ni.id)")) {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"title", "latest", "web"} {
		queries = nil
		if mode == "web" {
			_, _, _, err := e.repo.MediaView.ListLibraryMetadataPage(t.Context(), "library-nfo", "series", "", 0, 3, repository.MediaQueryFilter{IncludeNSFW: true})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			p := ItemsParams{UserID: "viewer", ParentID: "library-nfo", Limit: 3}
			if mode == "latest" {
				p.SortBy = "DateCreated"
				p.Filters = []string{"IsUnplayed"}
			}
			if _, _, err := e.nfoLibraryItems(t.Context(), p, mode != "latest"); err != nil {
				t.Fatal(err)
			}
		}
		if len(queries) != 2 {
			t.Fatalf("%s queries=%d", mode, len(queries))
		}
		if mode == "latest" && strings.Contains(queries[0].sql, "COUNT(*) AS total FROM candidates") {
			t.Fatal("Latest counted total")
		}
		for i, q := range queries {
			var raw []byte
			if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+q.sql, q.vars...).Row().Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan          map[string]any
				ExecutionTime float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(raw, &plans); err != nil {
				t.Fatal(err)
			}
			if i == 1 || mode == "title" {
				var inspect func(map[string]any)
				inspect = func(n map[string]any) {
					if n["Relation Name"] == "media" || n["Relation Name"] == "nfo_media_bindings" {
						rows, _ := n["Actual Rows"].(float64)
						loops, _ := n["Actual Loops"].(float64)
						removed, _ := n["Rows Removed by Filter"].(float64)
						if (rows+removed)*loops > 3000 {
							t.Fatalf("%s query %d scanned unrelated bindings: relation=%v alias=%v node=%v rows=%v loops=%v removed=%v", mode, i, n["Relation Name"], n["Alias"], n["Node Type"], rows, loops, removed)
						}
					}
					children, _ := n["Plans"].([]any)
					for _, child := range children {
						inspect(child.(map[string]any))
					}
				}
				inspect(plans[0].Plan)
			}
			t.Logf("%s query=%d %.3f ms", mode, i, plans[0].ExecutionTime)
		}
	}
}
