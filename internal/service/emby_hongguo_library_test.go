package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoLibraryCollectionType(t *testing.T) {
	e := &EmbyService{}
	if got := e.libraryAsView(&model.Library{Type: model.LibraryTypeHongGuo})["CollectionType"]; got != "tvshows" {
		t.Fatalf("collection type = %v", got)
	}
}

func TestEmbyLibraryDoesNotClaimUnplayedCount(t *testing.T) {
	e := &EmbyService{}
	for _, kind := range []string{"movie", "tv", "anime", model.LibraryTypeHongGuo, model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		view := e.libraryAsView(&model.Library{Type: kind})
		data := view["UserData"].(map[string]any)
		if _, ok := data["UnplayedItemCount"]; ok || data["Played"] != false || view["Type"] != "CollectionFolder" {
			t.Fatalf("library %s claims playback count: %v", kind, data)
		}
	}
}

func TestHongGuoLibraryPageBoundary(t *testing.T) {
	for _, p := range []ItemsParams{{SearchTerm: "作品"}, {PersonIDs: []string{"person"}}, {Filters: []string{"IsFavorite"}}, {Filters: []string{"IsResumable"}}, {Recursive: true}, {IncludeItemTypes: []string{"Episode"}}, {IncludeItemTypes: []string{"Folder"}}} {
		if hongGuoLibraryPageSupported(p) {
			t.Fatalf("special request entered work page: %+v", p)
		}
	}
	for _, p := range []ItemsParams{{}, {Recursive: true, IncludeItemTypes: []string{"Series"}}, {Filters: []string{"IsUnplayed"}}} {
		if !hongGuoLibraryPageSupported(p) {
			t.Fatalf("work request not handled: %+v", p)
		}
	}
}

func TestHongGuoLibraryPagingAndLatest(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureLatestMediaAddedTriggers(db); err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	ctx := t.Context()
	lib := model.Library{Name: "红果", Type: model.LibraryTypeHongGuo, Path: "/test/hg-browse"}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	works := make([]*model.HongGuoWork, 3)
	for i := range works {
		works[i], err = e.repo.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: fmt.Sprintf("90000000000000000%d", i+1), Title: fmt.Sprintf("作品%d", i), EpisodeCount: 2, Snapshot: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		m := model.Media{LibraryID: lib.ID, Path: fmt.Sprintf("/test/hg-browse/%d.strm", i), CatalogSource: "hongguo", LookupCatalogID: works[i].SourceID, SeasonNum: 1, EpisodeNum: 1}
		m.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		if err := e.repo.Media.Upsert(ctx, &m); err != nil {
			t.Fatal(err)
		}
	}
	// 两个源作品组成同一合集，仍只占一个分页位置。
	for i := 0; i < 2; i++ {
		if err := e.repo.HongGuo.SaveAlbum(ctx, works[i].SourceID, hongguo.Album{ID: "900000000000000099", Season: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	groupID := "hg-group-900000000000000099"
	standalone := "hg-work-" + works[2].ID
	m := model.Media{LibraryID: lib.ID, Path: "/test/hg-browse/new.strm", CatalogSource: "hongguo", LookupCatalogID: works[0].SourceID, SeasonNum: 1, EpisodeNum: 2}
	m.CreatedAt = base.Add(4 * time.Hour)
	if err := e.repo.Media.Upsert(ctx, &m); err != nil {
		t.Fatal(err)
	}
	for _, sortBy := range []string{"SortName", "DateCreated", "DateLastContentAdded"} {
		p := ItemsParams{UserID: "viewer", ParentID: lib.ID, Recursive: true, IncludeItemTypes: []string{"Series"}, Limit: 1, SortBy: sortBy, SortOrder: "Descending"}
		want := standalone
		if sortBy == "DateLastContentAdded" {
			want = groupID
		}
		for offset := 0; offset < 3; offset++ {
			p.StartIndex = offset
			got, err := e.Items(ctx, p)
			if err != nil || got["TotalRecordCount"] != int64(2) {
				t.Fatalf("%s page %d: %v %v", sortBy, offset, got, err)
			}
			items := got["Items"].([]map[string]any)
			if offset == 0 && (len(items) != 1 || items[0]["Id"] != want) || offset == 1 && (len(items) != 1 || items[0]["Id"] == want) || offset == 2 && len(items) != 0 {
				t.Fatalf("%s page %d items: %v", sortBy, offset, items)
			}
		}
	}
	for _, withNFO := range []bool{false, true} {
		if withNFO {
			// HasMedia 只检查来源，足以覆盖此前由其它库存在性选择的 Latest 分支。
			nfo := model.Media{LibraryID: lib.ID, Path: "/test/nfo-sentinel.strm", CatalogSource: "nfo"}
			if err := db.Create(&nfo).Error; err != nil {
				t.Fatal(err)
			}
		}
		items, err := e.LatestItems(ctx, "viewer", lib.ID, 1, false)
		if err != nil || len(items) != 1 || items[0]["Id"] != groupID {
			t.Fatalf("latest NFO=%v: %v %v", withNFO, items, err)
		}
		assertEmbyUnplayedCount(t, items[0], 3)
		// 全局 Latest 默认 Movie/Episode，与库内 Series 层级不同；有无 NFO 均保留旧顺序。
		var old []hongGuoNode
		if err := e.hongGuoNodes(ctx, "viewer", "").Where("kind IN ('Movie','Episode') AND NOT played").Order("latest_at DESC,id").Limit(2).Scan(&old).Error; err != nil {
			t.Fatal(err)
		}
		want, err := e.hongGuoNodePayloads(ctx, old, "viewer", []string{"BasicSyncInfo"})
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.LatestItems(ctx, "viewer", "", 2, false, "BasicSyncInfo")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("global latest NFO=%v differs: got=%v want=%v err=%v", withNFO, got, want, err)
		}
	}
	if err := e.MarkPlayed(ctx, "viewer", groupID, true); err != nil {
		t.Fatal(err)
	}
	// 标题浏览不筛选状态，但当前页仍必须返回真实的已看状态和入库日期。
	page, _, err := e.hongGuoLibraryItems(ctx, ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 1, SortBy: "SortName"}, true)
	if err != nil || len(page) != 1 || page[0]["Id"] != groupID || page[0]["UserData"].(map[string]any)["Played"] != true || page[0]["DateCreated"] != formatEmbyDateTime(base) {
		t.Fatalf("title page lost state/date: %v %v", page, err)
	}
	assertEmbyUnplayedCount(t, page[0], 0)
	for _, played := range []bool{false, true} {
		filter, want := "IsUnplayed", standalone
		if played {
			filter, want = "IsPlayed", groupID
		}
		p := ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 1, Filters: []string{filter}}
		items, total, err := e.hongGuoLibraryItems(ctx, p, true)
		if err != nil || total != 1 || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("played=%v: %v %d %v", played, items, total, err)
		}
		var original []hongGuoNode
		if err := e.hongGuoNodes(ctx, "viewer", lib.ID).Where("parent_id = '' AND played = ?", played).Scan(&original).Error; err != nil {
			t.Fatal(err)
		}
		expected, err := e.hongGuoNodePayloads(ctx, original, "viewer", nil)
		if err != nil || !reflect.DeepEqual(items, expected) {
			t.Fatalf("payload differs from hierarchy: %v %v %v", items, expected, err)
		}
	}
	// 当前库没有文件的第二季也必须更新合集全局时间，但不能泄露该季文件。
	if err := db.Exec("UPDATE media SET created_at=? WHERE lookup_catalog_id=?", base.Add(6*time.Hour), works[2].SourceID).Error; err != nil {
		t.Fatal(err)
	}
	other := model.Library{Name: "另一库", Type: model.LibraryTypeHongGuo, Path: "/test/hg-other"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM media WHERE lookup_catalog_id=?", works[1].SourceID).Error; err != nil {
		t.Fatal(err)
	}
	hidden := model.Media{LibraryID: other.ID, Path: "/test/hg-other/new.strm", CatalogSource: "hongguo", LookupCatalogID: works[1].SourceID, SeasonNum: 1, EpisodeNum: 1}
	hidden.CreatedAt = base.Add(7 * time.Hour)
	if err := e.repo.Media.Upsert(ctx, &hidden); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{groupID, standalone} {
		items, _, err := e.hongGuoLibraryItems(ctx, ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 1, SortBy: "DateLastContentAdded", SortOrder: "Descending"}, false)
		if err != nil || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("cross-library latest: %v %v", items, err)
		}
		if err := db.Delete(&hidden).Error; err != nil {
			t.Fatal(err)
		}
	}
	params := ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 10, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
	known, _, err := e.hongGuoLibraryItems(ctx, params, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE hongguo_works SET library_ids=NULL").Error; err != nil {
		t.Fatal(err)
	}
	unknown, _, err := e.hongGuoLibraryItems(ctx, params, false)
	if err != nil || !reflect.DeepEqual(known, unknown) {
		t.Fatalf("unknown membership changed Latest: got=%v want=%v err=%v", unknown, known, err)
	}
	for _, visibility := range []MediaVisibility{{HiddenLibraryIDs: []string{lib.ID}}, {LibraryRestricted: true}} {
		e.visibilityCache["viewer"] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		items, total, err := e.hongGuoLibraryItems(ctx, ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 1}, true)
		if err != nil || len(items) != 0 || total != 0 {
			t.Fatalf("invisible library: %v %d %v", items, total, err)
		}
		items, err = e.LatestItems(ctx, "viewer", lib.ID, 1, false)
		if err != nil || len(items) != 0 {
			t.Fatalf("invisible latest: %v %v", items, err)
		}
	}
}

func TestHongGuoLibraryPagePlan(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureLatestMediaAddedTriggers(db); err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	// 6,000 个有文件源作品组成 2,000 个多季合集，包含真实分集、封面及无文件作品。
	for _, sql := range []string{
		`INSERT INTO hongguo_works (id,source_id,kind,title,related_album_id,season_index,refreshed_at)
SELECT 'work-'||n,n::text,'series','Title '||n,((n-1)/3+1)::text,(n-1)%3+1,now() FROM generate_series(1,10000) n`,
		`INSERT INTO hongguo_episodes (id,work_id,number)
SELECT 'episode-'||n||'-'||e,'work-'||n,e FROM generate_series(1,6000) n CROSS JOIN generate_series(1,100) e`,
		`INSERT INTO hongguo_artworks (id,work_id,source_url,local_key)
SELECT 'art-'||n,'work-'||n,'https://example.invalid/poster','test/poster-'||n FROM generate_series(1,6000) n`,
		`INSERT INTO media (id,library_id,catalog_source,path,episode_num,scan_title,created_at)
SELECT 'file-'||n||'-'||e,'library','hongguo','/test/'||n||'/'||e,e,repeat('File metadata ',16),TIMESTAMP '2026-01-01' + n * INTERVAL '1 second'
FROM generate_series(1,6000) n CROSS JOIN generate_series(1,100) e`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id,episode_id)
SELECT 'file-'||n||'-'||e,'work-'||n,'episode-'||n||'-'||e FROM generate_series(1,6000) n CROSS JOIN generate_series(1,100) e`,
		`ANALYZE hongguo_works`, `ANALYZE hongguo_media_bindings`, `ANALYZE media`, `ANALYZE hongguo_episodes`, `ANALYZE hongguo_artworks`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	inspectDetail := false
	if err := db.Callback().Row().After("gorm:row").Register("test:hg-library-plan", func(tx *gorm.DB) {
		if tx.DryRun {
			return
		}
		sql := tx.Statement.SQL.String()
		if strings.HasPrefix(sql, "WITH albums AS MATERIALIZED") || strings.HasPrefix(sql, "WITH page_works AS MATERIALIZED") || strings.HasPrefix(sql, "SELECT a.id FROM hongguo_works AS w") || inspectDetail && strings.Contains(sql, "hongguo_media_bindings") && !strings.HasPrefix(sql, "EXPLAIN") {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"title", "date", "latest", "latest-history"} {
		latest := strings.HasPrefix(mode, "latest")
		if mode == "latest-history" {
			if err := db.Exec(`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,completed)
SELECT 'viewer',n::text,e,true FROM generate_series(1,6000) n CROSS JOIN generate_series(2,11) e`).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("ANALYZE hongguo_user_states").Error; err != nil {
				t.Fatal(err)
			}
		}
		queries = nil
		p := ItemsParams{UserID: "viewer", ParentID: "library", Limit: 3, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
		want := "hg-group-2000"
		if mode == "title" {
			p.SortBy, p.SortOrder, want = "SortName", "Ascending", "hg-group-1"
		}
		if latest {
			p.Filters = []string{"IsUnplayed"}
		}
		items, total, err := e.hongGuoLibraryItems(t.Context(), p, !latest)
		if err != nil || len(items) != 3 || items[0]["Id"] != want || !latest && total != 2000 {
			t.Fatalf("latest=%v len=%d total=%d err=%v", latest, len(items), total, err)
		}
		if len(queries) != 2 {
			t.Fatalf("captured %d queries", len(queries))
		}
		if latest && strings.Contains(queries[0].sql, "COUNT(*) AS total FROM works") {
			t.Fatal("Latest counted all works")
		}
		if !strings.Contains(queries[0].sql, "EXISTS (") || strings.Contains(queries[0].sql, "MIN(m.created_at)") || !latest && strings.Contains(queries[0].sql, "hongguo_user_states") {
			t.Fatal("work candidates aggregate file dates or playback states")
		}
		for i, query := range queries {
			var raw []byte
			if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query.sql, query.vars...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan          map[string]any
				ExecutionTime float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
				t.Fatalf("invalid plan: %v", err)
			}
			var inspect func(map[string]any)
			inspect = func(node map[string]any) {
				if i == 0 && node["CTE Name"] == "playback_states" {
					rows, _ := node["Actual Rows"].(float64)
					removed, _ := node["Rows Removed by Filter"].(float64)
					loops, _ := node["Actual Loops"].(float64)
					if (rows+removed)*loops > 120000 {
						t.Fatalf("candidate scans all user states per work: %s", raw)
					}
				}
				if i == 0 && node["Relation Name"] == "hongguo_user_states" && node["Actual Loops"].(float64) > 1 {
					t.Fatalf("candidate repeats effective playback state per work: %s", raw)
				}
				if i == 1 && (node["Relation Name"] == "hongguo_works" || node["Relation Name"] == "hongguo_artworks") {
					rows, _ := node["Actual Rows"].(float64)
					loops, _ := node["Actual Loops"].(float64)
					if rows*loops > 30 {
						t.Fatalf("page repeats work/artwork per file: %s", raw)
					}
				}
				if node["Relation Name"] == "media" || node["Relation Name"] == "hongguo_media_bindings" {
					rows, _ := node["Actual Rows"].(float64)
					removed, _ := node["Rows Removed by Filter"].(float64)
					loops, _ := node["Actual Loops"].(float64)
					bound := float64(1000)
					if i == 0 {
						bound = 6000 // 非空时间预筛后，只探测有文件的源作品。
						if latest {
							bound = 30
						} // Latest 只检查当前页附近的候选成员。
					}
					if (rows+removed)*loops > bound || i == 0 && loops > bound {
						t.Fatalf("page detail scanned unrelated files: %s", raw)
					}
				}
				children, _ := node["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0].Plan)
			t.Logf("mode=%s query=%d execution=%.3f ms", mode, i, plans[0].ExecutionTime)
		}
	}
	for _, id := range []string{"hg-group-2000", "hg-season-work-5998"} {
		queries = nil
		got, err := e.ImageURL(t.Context(), id, "Primary")
		if err != nil || got != "/api/catalogs/hongguo/artwork/art-5998" || len(queries) != 1 {
			t.Fatalf("artwork=%q queries=%d err=%v", got, len(queries), err)
		}
		query := queries[0]
		if strings.Contains(query.sql, "hongguo_user_states") || strings.Contains(query.sql, "hongguo_episodes") {
			t.Fatal("artwork expands episodes or playback state")
		}
		var raw []byte
		if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query.sql, query.vars...).Row().Scan(&raw); err != nil {
			t.Fatal(err)
		}
		type planNode struct {
			Relation string     `json:"Relation Name"`
			Rows     float64    `json:"Actual Rows"`
			Removed  float64    `json:"Rows Removed by Filter"`
			Loops    float64    `json:"Actual Loops"`
			Plans    []planNode `json:"Plans"`
		}
		var plans []struct {
			Plan          planNode
			ExecutionTime float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid artwork plan: %v", err)
		}
		var inspect func(planNode)
		inspect = func(n planNode) {
			if n.Relation != "" && (n.Rows+n.Removed)*n.Loops > 10 {
				t.Fatalf("artwork scans unrelated rows: %s", raw)
			}
			for _, child := range n.Plans {
				inspect(child)
			}
		}
		inspect(plans[0].Plan)
		t.Logf("artwork %s execution=%.3f ms", id, plans[0].ExecutionTime)
	}
	// 点击链路包括节点查询和当前页版本加载，不能只检查详情的第一条 SQL。
	inspectDetail = true
	if err := db.Callback().Query().After("gorm:query").Register("test:hg-detail-plan", func(tx *gorm.DB) {
		if tx.DryRun {
			return
		}
		if sql := tx.Statement.SQL.String(); strings.Contains(sql, "hongguo_media_bindings") {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ id, kind string }{
		{"hg-group-2000", "global-payload"},
		{"hg-group-2000", "detail"}, {"hg-season-work-5998", "detail"}, {"hg-episode-episode-5998-1", "detail"},
		{"hg-group-2000", "children"}, {"hg-group-2000", "episodes"}, {"hg-season-work-5998", "episodes"},
		{"hg-group-2000", "web-seasons"}, {"hg-group-2000", "web-episodes"}, {"hg-group-2000", "logical-versions"},
	} {
		queries = nil
		if request.kind == "global-payload" {
			ids := []string{request.id, "hg-group-1999", "hg-season-work-5998", "hg-episode-episode-5998-1"}
			items, err := e.globalItemPayloads(t.Context(), ids, ItemsParams{UserID: "viewer", Fields: []string{"MediaSources"}})
			if err != nil || len(items) != len(ids) {
				t.Fatalf("mixed parent/child page len=%d err=%v", len(items), err)
			}
		} else if request.kind == "web-seasons" {
			seasons, err := e.repo.MediaView.ListLibrarySeriesSeasons(t.Context(), "library", request.id, e.mediaQueryFilter(t.Context(), "viewer"))
			if err != nil || len(seasons) != 3 {
				t.Fatalf("Web seasons=%v err=%v", seasons, err)
			}
		} else if request.kind == "web-episodes" {
			season := 1
			views, err := e.repo.MediaView.ListLibrarySeriesViewsForSeason(t.Context(), "library", request.id, &season, e.mediaQueryFilter(t.Context(), "viewer"))
			if err != nil || len(views) != 100 {
				t.Fatalf("Web episodes=%d err=%v", len(views), err)
			}
		} else if request.kind == "logical-versions" {
			views, err := e.repo.MediaView.FindByLogicalMetadataIDs(t.Context(), []string{request.id}, e.mediaQueryFilter(t.Context(), "viewer"))
			if err != nil || len(views) != 300 {
				t.Fatalf("logical versions=%d err=%v", len(views), err)
			}
		} else if request.kind == "detail" {
			item, err := e.Item(t.Context(), request.id, "viewer")
			if err != nil || item == nil {
				t.Fatalf("%s detail: %v", request.id, err)
			}
		} else {
			p := ItemsParams{UserID: "viewer", ParentID: request.id, Limit: 3}
			want := int64(3)
			if request.kind == "episodes" {
				p.IncludeItemTypes = []string{"Episode"}
				want = 300
				if strings.HasPrefix(request.id, "hg-season-") {
					want = 100
				}
			}
			result, err := e.Items(t.Context(), p)
			if err != nil || result["TotalRecordCount"] != want || len(result["Items"].([]map[string]any)) != 3 {
				t.Fatalf("%s %s: %v %v", request.id, request.kind, result, err)
			}
		}
		if len(queries) == 0 {
			t.Fatal("no detail queries captured")
		}
		for i, query := range append([]statement(nil), queries...) {
			var raw []byte
			if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query.sql, query.vars...).Row().Scan(&raw); err != nil {
				t.Fatalf("%s %s query=%d sql=%s vars=%v: %v", request.id, request.kind, i, query.sql, query.vars, err)
			}
			type planNode struct {
				Relation string     `json:"Relation Name"`
				Rows     float64    `json:"Actual Rows"`
				Removed  float64    `json:"Rows Removed by Filter"`
				Loops    float64    `json:"Actual Loops"`
				Plans    []planNode `json:"Plans"`
			}
			var plans []struct {
				Plan          planNode
				ExecutionTime float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
				t.Fatalf("invalid detail plan: %v", err)
			}
			var inspect func(planNode)
			inspect = func(n planNode) {
				if (n.Relation == "media" || n.Relation == "hongguo_media_bindings" || n.Relation == "hongguo_episodes") && (n.Rows+n.Removed)*n.Loops > 1000 {
					t.Fatalf("%s %s query %d scans unrelated %s: rows=%v removed=%v loops=%v", request.id, request.kind, i, n.Relation, n.Rows, n.Removed, n.Loops)
				}
				for _, child := range n.Plans {
					inspect(child)
				}
			}
			inspect(plans[0].Plan)
			t.Logf("%s %s query=%d execution=%.3f ms", request.id, request.kind, i, plans[0].ExecutionTime)
		}
	}
	assertGlobalBrowsePlan(t, e)
}

func TestHongGuoLibraryPageMatchesHierarchy(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureLatestMediaAddedTriggers(db); err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	ctx := t.Context()
	libs := []model.Library{{Name: "可见库", Type: model.LibraryTypeHongGuo, Path: "/test/hg-projection"}, {Name: "其它库", Type: model.LibraryTypeHongGuo, Path: "/test/hg-projection-other"}}
	if err := db.Create(&libs).Error; err != nil {
		t.Fatal(err)
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{}
	for _, user := range []string{"viewer", "other"} {
		e.visibilityCache[user] = embyVisibilityCacheEntry{visibility: MediaVisibility{IncludeNSFW: true, AllowedLibraryIDs: []string{libs[0].ID}}, expiresAt: time.Now().Add(time.Hour)}
	}
	works := make([]*model.HongGuoWork, 6)
	for i := range works {
		work := hongguo.Work{SourceID: fmt.Sprintf("91000000000000000%d", i), Title: "同名作品", Overview: "简介", Tags: []string{"剧情"}, Rating: 7.5, EpisodeCount: 2, Snapshot: []byte(`{}`)}
		if i == 0 {
			work.Title = "无文件首季标题"
		}
		if i == 4 {
			work.TotalEpisodes, work.EpisodeCount, work.Completed = 1, 1, true
		}
		works[i], err = e.repo.HongGuo.SaveDetail(ctx, work)
		if err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			season := i + 1
			if i == 2 {
				season = 2
			} // 同季号保留两个源作品的独立分集身份。
			if err := e.repo.HongGuo.SaveAlbum(ctx, work.SourceID, hongguo.Album{ID: "910000000000000099", Season: season}); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&model.HongGuoArtwork{WorkID: &works[i].ID, SourceURL: "https://example.invalid/poster", LocalKey: fmt.Sprintf("test/poster-%d", i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, file := range []struct{ work, episode, library int }{{1, 1, 0}, {1, 1, 0}, {1, 2, 0}, {2, 1, 0}, {2, 2, 1}, {3, 1, 0}, {4, 1, 0}, {5, 1, 1}} {
		m := model.Media{LibraryID: libs[file.library].ID, Path: fmt.Sprintf("/test/hg-projection/%d.strm", i), CatalogSource: "hongguo", LookupCatalogID: works[file.work].SourceID, SeasonNum: 1, EpisodeNum: file.episode}
		m.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		if err := e.repo.Media.Upsert(ctx, &m); err != nil {
			t.Fatal(err)
		}
		if file.work == 4 {
			if err := db.Model(&model.HongGuoMediaBinding{}).Where("media_id = ?", m.ID).Update("episode_id", nil).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.MediaProbeMetadata{MediaID: m.ID, DurationMS: 120_000}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := e.MarkPlayed(ctx, "viewer", "hg-work-"+works[3].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkPlayed(ctx, "viewer", "hg-season-"+works[1].ID, true); err != nil {
		t.Fatal(err)
	}
	// 无分集绑定的电影用第 1 集状态；旧版本删除后，较短替代版本推断为已看。
	if err := db.Create(&model.HongGuoUserState{UserID: "viewer", SourceID: works[4].SourceID, EpisodeNumber: 1,
		MediaID: "removed-version", PositionMs: 100_000, DurationMs: 200_000, WatchedAt: &base}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,favorite) VALUES ('viewer',?,0,true)`, works[1].SourceID).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"viewer", "other"} {
		assertGlobalBrowseMatchesHierarchy(t, e, user)
		// 无文件首季不计数；同季号不同源分别计数，多版本和隐藏库分集不重复/泄露。
		item, err := e.Item(ctx, "hg-group-910000000000000099", user)
		if err != nil || item == nil {
			t.Fatalf("album detail: %v", err)
		}
		want := 3
		if user == "viewer" {
			want = 1
		}
		assertEmbyUnplayedCount(t, item, want)
		for _, sort := range []struct{ field, column string }{{"SortName", "title"}, {"DateCreated", "created_at"}, {"DateLastContentAdded", "latest_at"}} {
			for _, filter := range []string{"", "IsPlayed", "IsUnplayed"} {
				p := ItemsParams{UserID: user, ParentID: libs[0].ID, Limit: 2, SortBy: sort.field, SortOrder: "Descending", Fields: []string{"BasicSyncInfo"}}
				q := e.hongGuoNodes(ctx, user, libs[0].ID).Where("parent_id = ''")
				if filter != "" {
					p.Filters = []string{filter}
					q = q.Where("played = ?", filter == "IsPlayed")
				}
				var original []hongGuoNode
				if err := q.Order(sort.column + " DESC NULLS LAST, id").Scan(&original).Error; err != nil {
					t.Fatal(err)
				}
				expected, err := e.hongGuoNodePayloads(ctx, original, user, p.Fields)
				if err != nil {
					t.Fatal(err)
				}
				for offset := 0; offset <= len(expected)+1; offset++ {
					p.StartIndex = offset
					got, total, err := e.hongGuoLibraryItems(ctx, p, true)
					want := pageSlice(expected, offset, p.Limit)
					if err != nil || total != int64(len(expected)) || len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
						t.Fatalf("user=%s sort=%s filter=%s offset=%d total=%d got=%v want=%v err=%v", user, sort.field, filter, offset, total, got, want, err)
					}
					if sort.field == "DateLastContentAdded" {
						got, total, err = e.hongGuoLibraryItems(ctx, p, false)
						if err != nil || total != 0 || len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
							t.Fatalf("Latest user=%s filter=%s offset=%d got=%v want=%v err=%v", user, filter, offset, got, want, err)
						}
						for _, kind := range []string{"Movie", "Series"} {
							typed := []map[string]any{}
							for _, item := range expected {
								if item["Type"] == kind {
									typed = append(typed, item)
								}
							}
							want = pageSlice(typed, offset, p.Limit)
							typedParams := p
							typedParams.IncludeItemTypes = []string{kind}
							got, total, err = e.hongGuoLibraryItems(ctx, typedParams, false)
							if err != nil || total != 0 || len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
								t.Fatalf("typed Latest kind=%s user=%s filter=%s offset=%d err=%v", kind, user, filter, offset, err)
							}
						}
					}
				}
			}
		}
	}
	// 单项范围与原全局投影一致；同时覆盖多版本、用户状态、隐藏库与空权限。
	for _, user := range []string{"viewer", "other", "hidden", "restricted"} {
		if user == "hidden" || user == "restricted" {
			v := MediaVisibility{HiddenLibraryIDs: []string{libs[0].ID}}
			if user == "restricted" {
				v = MediaVisibility{LibraryRestricted: true}
			}
			e.visibilityCache[user] = embyVisibilityCacheEntry{visibility: v, expiresAt: time.Now().Add(time.Hour)}
		}
		var original []hongGuoNode
		if err := e.hongGuoNodes(ctx, user, "").Scan(&original).Error; err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{"hg-group-910000000000000099": true, "hg-work-" + works[1].ID: true, "hg-group-missing": true}
		for _, node := range original {
			ids[node.ID] = true
		}
		for id := range ids {
			var expected []hongGuoNode
			for _, node := range original {
				if node.ID == id {
					expected = append(expected, node)
				}
			}
			var got []hongGuoNode
			if err := e.hongGuoItemNodes(ctx, user, id).Where("id = ?", id).Scan(&got).Error; err != nil || len(got) != len(expected) || len(got) > 0 && !reflect.DeepEqual(got, expected) {
				t.Fatalf("scoped node user=%s id=%s got=%v want=%v err=%v", user, id, got, expected, err)
			}
			var want map[string]any
			if len(expected) > 0 {
				if expected[0].Kind == "Movie" || expected[0].Kind == "Episode" {
					want, err = e.hongGuoNodePayload(ctx, expected[0], user)
				} else {
					var items []map[string]any
					items, err = e.hongGuoNodePayloads(ctx, expected, user, nil)
					if len(items) > 0 {
						want = items[0]
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			item, err := e.Item(ctx, id, user)
			if err != nil || !reflect.DeepEqual(item, want) {
				t.Fatalf("detail user=%s id=%s got=%v want=%v err=%v", user, id, item, want, err)
			}
			for _, recursive := range []bool{false, true} {
				parents := map[string]bool{id: true}
				if recursive {
					for _, node := range original {
						if node.Kind == "Season" && node.ParentID == id {
							parents[node.ID] = true
						}
					}
				}
				for _, filter := range []string{"", "IsPlayed", "IsUnplayed"} {
					children := []hongGuoNode{}
					for _, node := range original {
						if parents[node.ParentID] && (!recursive || node.Kind == "Episode") && (filter == "" || node.Played == (filter == "IsPlayed")) {
							children = append(children, node)
						}
					}
					sort.Slice(children, func(i, j int) bool {
						a, b := children[i], children[j]
						if a.SeasonNumber != b.SeasonNumber {
							return a.SeasonNumber < b.SeasonNumber
						}
						if a.EpisodeNumber != b.EpisodeNumber {
							return a.EpisodeNumber < b.EpisodeNumber
						}
						if a.Title != b.Title {
							return a.Title < b.Title
						}
						return a.ID < b.ID
					})
					expectedItems, err := e.hongGuoNodePayloads(ctx, children, user, nil)
					if err != nil {
						t.Fatal(err)
					}
					for _, offset := range []int{0, 1, len(children) + 1} {
						result, err := e.Items(ctx, ItemsParams{UserID: user, ParentID: id, Recursive: recursive, Filters: []string{filter}, Limit: 1, StartIndex: offset})
						if err != nil {
							t.Fatal(err)
						}
						got := result["Items"].([]map[string]any)
						want := pageSlice(expectedItems, offset, 1)
						if result["TotalRecordCount"] != int64(len(children)) || len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
							t.Fatalf("children user=%s id=%s recursive=%v filter=%s offset=%d got=%v want=%v", user, id, recursive, filter, offset, result, want)
						}
					}
				}
			}
		}
	}
	// 公共图片沿用原可见性和封面顺序，不能给已并入合集的旧 work 身份补图。
	ids := []string{"hg-group-910000000000000099", "hg-group-", "hg-group-missing", "hg-work-missing", "hg-episode-missing"}
	for _, work := range works {
		ids = append(ids, "hg-work-"+work.ID, "hg-season-"+work.ID)
	}
	for _, visibility := range []MediaVisibility{{IncludeNSFW: true}, {HiddenLibraryIDs: []string{libs[0].ID}}, {AllowedLibraryIDs: []string{libs[0].ID}}, {LibraryRestricted: true}} {
		e.visibilityCache[""] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		var nodes []hongGuoNode
		if err := e.hongGuoNodes(ctx, "", "").Scan(&nodes).Error; err != nil {
			t.Fatal(err)
		}
		want := map[string]string{}
		for _, node := range nodes {
			if node.ArtworkID != "" {
				want[node.ID] = "/api/catalogs/hongguo/artwork/" + node.ArtworkID
			}
			ids = append(ids, node.ID)
		}
		for _, id := range ids {
			for _, imageType := range []string{"Primary", "Backdrop"} {
				expected := want[id]
				if imageType != "Primary" {
					expected = ""
				}
				got, err := e.ImageURL(ctx, id, imageType)
				if err != nil || got != expected {
					t.Fatalf("artwork %s %s: got=%q want=%q err=%v", id, imageType, got, expected, err)
				}
			}
		}
	}
	e.visibilityCache[""] = embyVisibilityCacheEntry{visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}
	if err := db.Model(&model.HongGuoArtwork{}).Where("work_id = ?", works[3].ID).Update("local_key", "").Error; err != nil {
		t.Fatal(err)
	}
	if got, err := e.ImageURL(ctx, "hg-work-"+works[3].ID, "Primary"); err != nil || got != "" {
		t.Fatalf("missing local artwork: %q %v", got, err)
	}
}
