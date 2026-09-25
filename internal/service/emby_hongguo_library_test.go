package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
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
	}
	if err := e.MarkPlayed(ctx, "viewer", groupID, true); err != nil {
		t.Fatal(err)
	}
	// 标题浏览不筛选状态，但当前页仍必须返回真实的已看状态和入库日期。
	page, _, err := e.hongGuoLibraryItems(ctx, ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 1, SortBy: "SortName"}, true)
	if err != nil || len(page) != 1 || page[0]["Id"] != groupID || page[0]["UserData"].(map[string]any)["Played"] != true || page[0]["DateCreated"] != formatEmbyDateTime(base) {
		t.Fatalf("title page lost state/date: %v %v", page, err)
	}
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
	for _, visibility := range []MediaVisibility{{HiddenLibraryIDs: []string{lib.ID}}, {LibraryRestricted: true}} {
		e.visibilityCache["viewer"] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		items, total, err := e.hongGuoLibraryItems(ctx, ItemsParams{UserID: "viewer", ParentID: lib.ID, Limit: 1}, true)
		if err != nil || len(items) != 0 || total != 0 {
			t.Fatalf("invisible library: %v %d %v", items, total, err)
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
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	// 接近实际库的 6,000 部作品、60 万绑定，另有无文件作品。
	for _, sql := range []string{
		`INSERT INTO hongguo_works (id,source_id,kind,title,related_album_id,season_index,refreshed_at)
SELECT 'work-'||n,n::text,'series','Title '||n,n::text,1,now() FROM generate_series(1,10000) n`,
		`INSERT INTO media (id,library_id,catalog_source,path,episode_num,scan_title,created_at)
SELECT 'file-'||n||'-'||e,'library','hongguo','/test/'||n||'/'||e,e,repeat('File metadata ',16),TIMESTAMP '2026-01-01' + n * INTERVAL '1 second'
FROM generate_series(1,6000) n CROSS JOIN generate_series(1,100) e`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id)
SELECT 'file-'||n||'-'||e,'work-'||n FROM generate_series(1,6000) n CROSS JOIN generate_series(1,100) e`,
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
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:hg-library-plan", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.HasPrefix(sql, "WITH scoped AS MATERIALIZED") || strings.HasPrefix(sql, "SELECT * FROM (\nSELECT n.id") {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"title", "date", "latest"} {
		latest := mode == "latest"
		queries = nil
		p := ItemsParams{UserID: "viewer", ParentID: "library", Limit: 3, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
		want := "hg-group-6000"
		if mode == "title" {
			p.SortBy, p.SortOrder, want = "SortName", "Ascending", "hg-group-1"
		}
		if latest {
			p.Filters = []string{"IsUnplayed"}
		}
		items, total, err := e.hongGuoLibraryItems(t.Context(), p, !latest)
		if err != nil || len(items) != 3 || items[0]["Id"] != want || !latest && total != 6000 {
			t.Fatalf("latest=%v len=%d total=%d err=%v", latest, len(items), total, err)
		}
		if len(queries) != 2 {
			t.Fatalf("captured %d queries", len(queries))
		}
		if latest && strings.Contains(queries[0].sql, "COUNT(*) AS total FROM works") {
			t.Fatal("Latest counted all works")
		}
		if mode == "title" && (!strings.Contains(queries[0].sql, "EXISTS (") || strings.Contains(queries[0].sql, "MIN(m.created_at)") || strings.Contains(queries[0].sql, "hongguo_user_states")) {
			t.Fatal("title candidates aggregate file dates or playback states")
		}
		for i, query := range queries {
			var raw []byte
			if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query.sql, query.vars...).Row().Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan          map[string]any
				ExecutionTime float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
				t.Fatalf("invalid plan: %v", err)
			}
			if i == 1 || mode == "title" {
				var inspect func(map[string]any)
				inspect = func(node map[string]any) {
					if node["Relation Name"] == "media" || node["Relation Name"] == "hongguo_media_bindings" {
						rows, _ := node["Actual Rows"].(float64)
						removed, _ := node["Rows Removed by Filter"].(float64)
						loops, _ := node["Actual Loops"].(float64)
						bound := float64(1000)
						if i == 0 {
							bound = 10000 // 每部作品最多一次存在性探测，不读取全部 60 万绑定。
						}
						if (rows+removed)*loops > bound {
							t.Fatalf("page detail scanned unrelated files: %s", raw)
						}
					}
					children, _ := node["Plans"].([]any)
					for _, child := range children {
						inspect(child.(map[string]any))
					}
				}
				inspect(plans[0].Plan)
			}
			t.Logf("mode=%s query=%d execution=%.3f ms", mode, i, plans[0].ExecutionTime)
		}
	}
}
