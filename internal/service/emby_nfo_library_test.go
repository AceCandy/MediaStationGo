package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
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
	if err := database.EnsureLatestMediaAddedTriggers(db); err != nil {
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
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	return e
}

func TestNFOPosterUnplayedCounts(t *testing.T) {
	e := nfoBrowseFixture(t, 1, 3)
	db, ctx := e.repo.DB, t.Context()
	// 资料存在但尚无文件的集不计入角标。
	if err := db.Exec(`INSERT INTO nfo_items (id,library_id,local_key,kind,title,parent_id,episode_num)
 VALUES ('ep-new','library-nfo','ep-new','episode','New episode','season-1',4)`).Error; err != nil {
		t.Fatal(err)
	}
	check := func(user string, want int) {
		t.Helper()
		for _, tc := range []struct{ id, parent, kind string }{
			{"nfo-show-1", "library-nfo", "Series"},
			{"nfo-season-1", "nfo-show-1", "Season"},
		} {
			item, err := e.Item(ctx, tc.id, user)
			if err != nil || item == nil {
				t.Fatalf("detail: %v", err)
			}
			assertEmbyUnplayedCount(t, item, want)
			page, err := e.Items(ctx, ItemsParams{UserID: user, ParentID: tc.parent, IncludeItemTypes: []string{tc.kind}, Limit: 10, Fields: []string{"BasicSyncInfo"}})
			if err != nil {
				t.Fatal(err)
			}
			items := page["Items"].([]map[string]any)
			if len(items) != 1 {
				t.Fatalf("list=%v", page)
			}
			assertEmbyUnplayedCount(t, items[0], want)
		}
		items, err := e.LatestItems(ctx, user, "library-nfo", 1, want == 0, "BasicSyncInfo")
		if err != nil || len(items) != 1 {
			t.Fatalf("latest=%v err=%v", items, err)
		}
		assertEmbyUnplayedCount(t, items[0], want)
	}
	mark := func(id string, played bool) {
		t.Helper()
		if err := e.MarkPlayed(ctx, "viewer", id, played); err != nil {
			t.Fatal(err)
		}
	}
	check("viewer", 3) // 六个文件，三个逻辑分集。
	mark("nfo-ep-1-1", true)
	check("viewer", 2)
	mark("nfo-show-1", true)
	check("viewer", 0)
	check("other-user", 3)
	for _, query := range []string{
		`INSERT INTO media (id,library_id,path,catalog_source) VALUES ('file-new','library-nfo','/fixture/new','nfo')`,
		`INSERT INTO nfo_media_bindings (media_id,item_id,title,fingerprint) VALUES ('file-new','ep-new','New episode','fixture')`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	check("viewer", 1)
	mark("nfo-ep-1-1", false)
	check("viewer", 2)
	// 已删版本的断点按可见替代版本推导完成，角标不能直接读取原始 completed。
	if err := db.Create(&model.MediaProbeMetadata{MediaID: "file-ep-1-1-1", DurationMS: 100_000, Width: 1920}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.NFOUserState{}).Where("user_id='viewer' AND item_id='ep-1-1'").Updates(map[string]any{
		"media_id": "removed", "position_ms": 200_000, "resume_position_ms": 200_000, "duration_ms": 1_000_000,
	}).Error; err != nil {
		t.Fatal(err)
	}
	check("viewer", 1)
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
		e.visibilityCache[e.repo.ReadCacheKey()+"viewer"] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		for _, id := range []string{"nfo-show-1", "nfo-season-1", "nfo-ep-1-1", "nfo-show-3", "nfo-season-3", "nfo-ep-3-1", "nfo-missing"} {
			var want, got []hongGuoNode
			if err := e.nfoNodes(ctx, "viewer", "").Where("id = ? OR parent_id = ?", id, id).Order("id").Scan(&want).Error; err != nil {
				t.Fatal(err)
			}
			if err := e.nfoItemNodes(ctx, "viewer", id).Where("id = ? OR parent_id = ?", id, id).Order("id").Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("scoped nodes id=%s visibility=%+v got=%v want=%v", id, visibility, got, want)
			}
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
				} else if sortBy == "DateLastContentAdded" {
					order = "latest_at DESC NULLS LAST"
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
					if sortBy == "DateLastContentAdded" {
						latest, total, err := e.nfoLibraryItems(ctx, p, false)
						if err != nil || total != 0 || !reflect.DeepEqual(latest, want) {
							t.Fatalf("Latest offset=%d filter=%v got=%v want=%v err=%v", offset, filters, latest, want, err)
						}
					}
				}
				if sortBy == "DateLastContentAdded" && len(filters) > 0 {
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
		views, summaries, total, err := e.repo.MediaView.ListLibraryMetadataPage(ctx, "library-nfo", "series", "", offset, 1, repository.MediaQueryFilter{})
		if err != nil || total != 3 || len(views) != len(summaries) {
			t.Fatalf("Web page %d total=%d err=%v", offset, total, err)
		}
		if offset < 3 && (len(summaries) != 1 || summaries[0].Count != 2 || summaries[0].VersionCount != 4) || offset == 3 && len(summaries) != 0 {
			t.Fatalf("Web summaries=%v", summaries)
		}
	}
}

func TestNFOLibraryCreatedAtUsesFirstItemCreation(t *testing.T) {
	e := nfoBrowseFixture(t, 2, 1)
	db, ctx := e.repo.DB, t.Context()
	first := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 1, 0)
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE nfo_items SET created_at=? WHERE id IN ('show-1','season-1','ep-1-1')`, []any{first}},
		{`UPDATE nfo_items SET created_at=? WHERE id IN ('show-2','season-2','ep-2-1')`, []any{second}},
		{`UPDATE media SET created_at='2026-05-01' WHERE id LIKE 'file-ep-1-%'`, nil},
	} {
		if err := db.Exec(query.sql, query.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, parent := range []string{"library-nfo", ""} {
		for _, descending := range []bool{false, true} {
			p := ItemsParams{UserID: "viewer", ParentID: parent, Recursive: true, IncludeItemTypes: []string{"Series"}, SortBy: "DateCreated", Limit: 1}
			want, date := "nfo-show-1", first
			if descending {
				p.SortOrder, want, date = "Descending", "nfo-show-2", second
			}
			page, err := e.Items(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			items := page["Items"].([]map[string]any)
			if len(items) != 1 || items[0]["Id"] != want || items[0]["DateCreated"] != formatEmbyDateTime(date) {
				t.Fatalf("params=%+v items=%v", p, items)
			}
		}
	}
	for _, id := range []string{"nfo-show-1", "nfo-season-1", "nfo-ep-1-1", "file-ep-1-1-1"} {
		item, err := e.Item(ctx, id, "viewer")
		if err != nil || item == nil || item["DateCreated"] != formatEmbyDateTime(first) {
			t.Fatalf("id=%s item=%v err=%v", id, item, err)
		}
	}
	// 先入文件、后补 NFO，再增版本和重扫：条目的首次时间不随文件变化。
	file := model.Media{LibraryID: "library-nfo", Path: "/fixture/late-nfo.mkv", CatalogSource: model.CatalogSourceNFO}
	file.CreatedAt = first.AddDate(0, -1, 0)
	if _, err := e.repo.NFO.Ingest(ctx, &file, nil); err != nil {
		t.Fatal(err)
	}
	input := repository.NFOIngest{Items: []model.NFOItem{{LocalKey: "late-movie", Kind: "movie", NFOFields: model.NFOFields{Title: "Movie"}}}, Binding: model.NFOMediaBinding{Fingerprint: "first"}}
	input.Items[0].CreatedAt = first
	if _, err := e.repo.NFO.Ingest(ctx, &file, &input); err != nil {
		t.Fatal(err)
	}
	itemID := input.Items[0].ID
	newFile := model.Media{LibraryID: file.LibraryID, Path: "/fixture/late-nfo-v2.mkv", CatalogSource: model.CatalogSourceNFO}
	newFile.CreatedAt = second
	input.Items[0].CreatedAt, input.Binding.Fingerprint = second, "second"
	if _, err := e.repo.NFO.Ingest(ctx, &newFile, &input); err != nil {
		t.Fatal(err)
	}
	input.Binding.Fingerprint = "rescan"
	if _, err := e.repo.NFO.Ingest(ctx, &newFile, &input); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.Media{}, "id=?", file.ID).Error; err != nil {
		t.Fatal(err)
	}
	views, err := e.repo.MediaView.NFOItemViews(ctx, itemID, repository.MediaQueryFilter{})
	if err != nil || len(views) != 1 || !views[0].CreatedAt.Equal(second) || !views[0].CatalogCreatedAt.Equal(first) || views[0].LatestMediaAddedAt == nil || !views[0].LatestMediaAddedAt.Equal(second) {
		t.Fatalf("versions=%+v err=%v", views, err)
	}
	item, err := e.Item(ctx, "nfo-"+itemID, "viewer")
	if err != nil || item == nil || item["DateCreated"] != formatEmbyDateTime(first) {
		t.Fatalf("movie date=%v err=%v", item, err)
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
		views, summaries, total, err := e.repo.MediaView.ListLibraryMetadataPage(ctx, "library-nfo", "movie", "", 0, 1, repository.MediaQueryFilter{MissingPoster: true, MissingChineseTitle: missingChinese})
		versions := 2
		if missingChinese {
			versions = 1
		}
		if err != nil || total != 1 || len(views) != 1 || len(summaries) != 1 || summaries[0].VersionCount != versions {
			t.Fatalf("missingChinese=%v views=%v summaries=%v total=%d err=%v", missingChinese, views, summaries, total, err)
		}
	}
	// 原版本已删除时，候选筛选必须复用替代版本推导的已看状态，不能只读 completed。
	if err := db.Create(&model.MediaProbeMetadata{MediaID: "file-b", DurationMS: 100_000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NFOUserState{UserID: "viewer", ItemID: "movie", MediaID: "removed", PositionMs: 200_000, DurationMs: 1_000_000}).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"viewer", "other-user"} {
		items, err := e.LatestItems(ctx, user, "library-nfo", 1, true)
		want := 0
		if user == "viewer" {
			want = 1
		}
		if err != nil || len(items) != want || len(items) > 0 && items[0]["Id"] != "nfo-movie" {
			t.Fatalf("effective played user=%s items=%v err=%v", user, items, err)
		}
		for _, kind := range []string{"Movie", "Series"} {
			for _, filter := range []string{"IsPlayed", "IsUnplayed"} {
				want := 0
				if kind == "Movie" && (filter == "IsPlayed") == (user == "viewer") {
					want = 1
				}
				items, total, err := e.nfoLibraryItems(ctx, ItemsParams{UserID: user, ParentID: "library-nfo", Limit: 1, IncludeItemTypes: []string{kind}, Filters: []string{filter}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}, false)
				if err != nil || total != 0 || len(items) != want {
					t.Fatalf("typed Latest kind=%s user=%s filter=%s count=%d err=%v", kind, user, filter, len(items), err)
				}
			}
		}
	}
}

func TestNFOLibraryPagePlans(t *testing.T) {
	e := nfoBrowseFixture(t, 1000, 50)
	db := e.repo.DB
	// 大量有效状态也不能让最近添加在分页前展开全部文件版本。
	if err := db.Exec(`INSERT INTO nfo_user_states (user_id,item_id,media_id,completed)
SELECT 'viewer',id,'file-'||id||'-1',TRUE FROM nfo_items WHERE kind='episode' AND episode_num % 2 = 0`).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"media", "nfo_items", "nfo_media_bindings", "nfo_user_states"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	detailPlan := false
	capture := func(tx *gorm.DB) {
		if tx.DryRun {
			return
		}
		sql := tx.Statement.SQL.String()
		if !strings.HasPrefix(sql, "EXPLAIN") && (strings.HasPrefix(sql, "WITH work_batch") || strings.HasPrefix(sql, "WITH candidates") || strings.HasPrefix(sql, "WITH works") || strings.Contains(sql, "COUNT(DISTINCT ni.id)") || detailPlan && strings.Contains(sql, "nfo_media_bindings")) {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:nfo-pages", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:nfo-pages", capture); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"title", "latest", "created", "web", "detail", "season-detail", "episode-detail", "seasons", "episodes", "season-episodes", "versions", "global-payload"} {
		queries = nil
		detailPlan = mode != "title" && mode != "latest" && mode != "created" && mode != "web"
		if detailPlan {
			id := "nfo-show-1"
			if strings.HasPrefix(mode, "season-") {
				id = "nfo-season-1"
			}
			if mode == "episode-detail" || mode == "versions" {
				id = "nfo-ep-1-1"
			}
			if mode == "global-payload" {
				ids := []string{"nfo-show-1", "nfo-season-1", "nfo-ep-1-1", "nfo-show-2"}
				items, err := e.globalItemPayloads(t.Context(), ids, ItemsParams{UserID: "viewer", Fields: []string{"MediaSources"}})
				if err != nil || len(items) != len(ids) {
					t.Fatalf("mixed NFO parent/child page len=%d err=%v", len(items), err)
				}
			} else if strings.HasSuffix(mode, "detail") {
				item, err := e.Item(t.Context(), id, "viewer")
				if err != nil || item == nil {
					t.Fatalf("%s: %v", mode, err)
				}
			} else if mode == "versions" {
				views, err := e.repo.MediaView.NFOItemViews(t.Context(), id, e.mediaQueryFilter(t.Context(), "viewer"))
				if err != nil || len(views) != 2 {
					t.Fatalf("versions=%d err=%v", len(views), err)
				}
			} else {
				result, err := e.Items(t.Context(), ItemsParams{UserID: "viewer", ParentID: id, Recursive: mode != "seasons", Limit: 3})
				want := int64(50)
				if mode == "seasons" {
					want = 1
				}
				if err != nil || result["TotalRecordCount"] != want {
					t.Fatalf("%s: %v %v", mode, result, err)
				}
			}
		} else if mode == "web" {
			_, summaries, total, err := e.repo.MediaView.ListLibraryMetadataPage(t.Context(), "library-nfo", "series", "", 0, 3, repository.MediaQueryFilter{})
			if err != nil || total != 1000 || len(summaries) != 3 || summaries[0].MetadataID != "nfo-show-1" || summaries[0].Count != 50 || summaries[0].VersionCount != 100 {
				t.Fatalf("Web page total=%d summaries=%v err=%v", total, summaries, err)
			}
		} else {
			p := ItemsParams{UserID: "viewer", ParentID: "library-nfo", Limit: 3}
			if mode == "latest" || mode == "created" {
				p.SortBy = "DateLastContentAdded"
				if mode == "created" {
					p.SortBy = "DateCreated"
				}
				p.SortOrder = "Descending"
				p.Filters = []string{"IsUnplayed"}
			}
			items, total, err := e.nfoLibraryItems(t.Context(), p, mode != "latest")
			wantTotal := int64(1000)
			if mode == "latest" {
				wantTotal = 0
			}
			if err != nil || total != wantTotal || len(items) != 3 || items[0]["Id"] != "nfo-show-1" || items[2]["Id"] != "nfo-show-100" {
				t.Fatalf("%s page total=%d items=%v err=%v", mode, total, items, err)
			}
		}
		wantQueries := 2
		if mode == "created" {
			wantQueries = 3
		}
		if !detailPlan && len(queries) != wantQueries || detailPlan && len(queries) == 0 {
			t.Fatalf("%s queries=%d", mode, len(queries))
		}
		if mode == "title" || mode == "latest" || mode == "created" {
			for _, unused := range []string{"ARRAY_AGG(m.id", "MAX(st.watched_at)", "MAX(COALESCE(st.position_ms", "MAX(m.created_at)"} {
				if strings.Contains(queries[len(queries)-1].sql, unused) {
					t.Fatalf("Series detail still aggregates %s", unused)
				}
			}
			if strings.Contains(queries[0].sql, "MIN(m.created_at)") || strings.Contains(queries[0].sql, "MAX(m.created_at)") {
				t.Fatal("NFO candidate still aggregates file creation dates")
			}
		}
		if mode == "latest" && strings.Contains(queries[0].sql, "SELECT COUNT(*)") {
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
			if detailPlan || i > 0 || mode == "title" || mode == "latest" {
				var inspect func(map[string]any)
				inspect = func(n map[string]any) {
					if n["Relation Name"] == "media" || n["Relation Name"] == "nfo_media_bindings" || !detailPlan && n["Relation Name"] == "nfo_user_states" || (detailPlan || i == 0) && n["Relation Name"] == "nfo_items" {
						rows, _ := n["Actual Rows"].(float64)
						loops, _ := n["Actual Loops"].(float64)
						removed, _ := n["Rows Removed by Filter"].(float64)
						maxRows := 3000.0
						if n["Relation Name"] == "nfo_items" {
							maxRows = 10000
						}
						if detailPlan {
							maxRows = 1000
						}
						if mode == "latest" && i == 0 && n["Relation Name"] != "nfo_items" {
							maxRows = 500 // 最多 50 个作品的资格，不是最终 3 张卡片。
						}
						if (rows+removed)*loops > maxRows || mode == "latest" && i == 0 && n["Relation Name"] != "nfo_items" && loops > maxRows {
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

func TestNFOLibraryMetadataSorts(t *testing.T) {
	e := nfoBrowseFixture(t, 3, 1)
	if err := e.repo.DB.Exec(`UPDATE nfo_items SET
 year=CASE id WHEN 'show-1' THEN 2000 WHEN 'show-2' THEN 2030 ELSE 2020 END,
 rating=CASE id WHEN 'show-1' THEN 9 WHEN 'show-2' THEN 7 ELSE 8 END,
 release_date=CASE id WHEN 'show-1' THEN '2020-01-01' WHEN 'show-2' THEN '2010-01-01' ELSE '2030-01-01' END
 WHERE kind='series'`).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sort string
		ids  []string
	}{
		{"ProductionYear", []string{"nfo-show-2", "nfo-show-3", "nfo-show-1"}},
		{"PremiereDate", []string{"nfo-show-3", "nfo-show-1", "nfo-show-2"}},
		{"CommunityRating", []string{"nfo-show-1", "nfo-show-3", "nfo-show-2"}},
	} {
		for _, order := range []string{"Descending", "Ascending"} {
			for offset := 0; offset < 3; offset++ {
				result, err := e.Items(t.Context(), ItemsParams{UserID: "viewer", ParentID: "library-nfo", SortBy: tc.sort, SortOrder: order, StartIndex: offset, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				index := offset
				if order == "Ascending" {
					index = 2 - offset
				}
				items := result["Items"].([]map[string]any)
				if len(items) != 1 || items[0]["Id"] != tc.ids[index] || result["TotalRecordCount"] != int64(3) {
					t.Fatalf("sort=%s order=%s offset=%d got=%+v want=%s", tc.sort, order, offset, result, tc.ids[index])
				}
			}
		}
	}
}
