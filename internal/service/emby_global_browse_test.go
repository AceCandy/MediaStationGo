package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// 保留优化前的全层级候选作对照，不以新查询生成期望结果。
func originalGlobalBrowseCandidates(t *testing.T, e *EmbyService, p ItemsParams) *gorm.DB {
	ctx, db := t.Context(), e.repo.DB
	files := e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID).
		Select("media.metadata_id, media.created_at")
	legacy := db.Table("(?) AS f", files).
		Joins("JOIN metadata_items leaf ON leaf.id = f.metadata_id").
		Joins("LEFT JOIN metadata_items parent ON parent.id = leaf.parent_id").
		Joins("LEFT JOIN metadata_items grandparent ON grandparent.id = parent.parent_id").
		Joins("JOIN metadata_items item ON item.id IN (leaf.id, parent.id, grandparent.id)").
		Joins("LEFT JOIN (?) h ON h.metadata_id = leaf.id", repository.PlaybackStates(ctx, db, "legacy", p.UserID, e.mediaQueryFilter(ctx, p.UserID))).
		Joins("LEFT JOIN favorites fav ON fav.metadata_id = item.id AND fav.user_id = ? AND fav.deleted_at IS NULL", p.UserID).
		Select(`item.id, CASE WHEN item.kind='episode' THEN 'legacy:'||COALESCE(grandparent.id,item.id) ELSE 'legacy:'||item.id END AS resume_key,
item.kind,item.title,MAX(f.created_at) AS created_at,item.latest_media_added_at AS latest_at,
COALESCE(MAX(h.watched_at),MAX(f.created_at)) AS played_at,BOOL_AND(COALESCE(h.completed,FALSE)) AS played,
BOOL_OR(fav.id IS NOT NULL) AS favorite,MAX(COALESCE(h.position_ms,0)) AS position_ms,
item.rating,COALESCE(item.release_date,'') AS release_date,item.year`).Group("item.id,grandparent.id")
	if len(p.PersonIDs) > 0 {
		legacy = legacy.Where(`EXISTS (SELECT 1 FROM metadata_credits c WHERE c.person_id IN ?
AND c.metadata_id=CASE WHEN item.kind='episode' THEN item.parent_id ELSE item.id END)`, p.PersonIDs)
	}
	source := e.hongGuoPersonFilter(ctx, e.hongGuoNodes(ctx, p.UserID, ""), p.UserID, "", p.PersonIDs).
		Select("id,resume_key,LOWER(kind) AS kind,title,file_latest_at AS created_at,latest_at,COALESCE(played_at,file_latest_at) AS played_at,played,favorite,position_ms,rating,'' AS release_date,0 AS year")
	local := e.nfoNodes(ctx, p.UserID, "").Select("id,resume_key,LOWER(kind) AS kind,title,created_at,latest_at,COALESCE(played_at,file_latest_at) AS played_at,played,favorite,position_ms,rating,release_date,year")
	if len(p.PersonIDs) > 0 {
		local = local.Where("FALSE")
	}
	return filterGlobalItems(db.Table("(?) combined", db.Raw("? UNION ALL ? UNION ALL ?", legacy, source, local)), p)
}

// 使用六十万文件的既有 fixture，分别核验候选和页内实际访问范围。
func assertGlobalBrowsePlan(t *testing.T, e *EmbyService) {
	t.Helper()
	db := e.repo.DB
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:global-plan", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") {
			queries = append(queries, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Row().Remove("test:global-plan") })
	p := ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, Filters: []string{"IsUnplayed"}, Limit: 20, Fields: []string{"BasicSyncInfo"}}
	for _, sortBy := range []string{"", "SortName", "DateLastContentAdded", "Random"} {
		p.SortBy, queries = sortBy, nil
		page, handled, err := e.hongGuoGlobalItems(t.Context(), p)
		if err != nil || !handled || len(queries) != 2 || page["TotalRecordCount"] != int64(2000) || len(page["Items"].([]map[string]any)) != 20 {
			t.Fatalf("global plan queries=%d handled=%v err=%v", len(queries), handled, err)
		}
		for phase, query := range append([]statement(nil), queries...) {
			sql, vars := query.sql, query.vars
			if strings.Contains(sql, "hongguo_artworks") || strings.Contains(sql, "unplayed_item_count") {
				t.Fatal("global candidates load poster/display aggregates")
			}
			var raw []byte
			if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+sql, vars...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			type planNode struct {
				Relation string     `json:"Relation Name"`
				Subplan  string     `json:"Subplan Name"`
				Rows     float64    `json:"Actual Rows"`
				Removed  float64    `json:"Rows Removed by Filter"`
				Loops    float64    `json:"Actual Loops"`
				Plans    []planNode `json:"Plans"`
			}
			var plans []struct {
				Plan          planNode
				ExecutionTime float64 `json:"Execution Time"`
				JIT           struct{ Timing struct{ Total float64 } }
			}
			if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
				t.Fatalf("global plan decode: %v", err)
			}
			materialized := 0
			mediaVisits, bindingVisits := 0.0, 0.0
			var inspectRandomCandidate func(planNode)
			inspectRandomCandidate = func(n planNode) {
				// 无文件且归属未知的作品保留空索引探测；已维护归属的作品不得读取文件。
				if (n.Relation == "media" || n.Relation == "hongguo_media_bindings") && (n.Rows+n.Removed)*n.Loops > 0 {
					t.Fatalf("random candidate reads files before LIMIT: %s rows=%.0f removed=%.0f loops=%.0f", n.Relation, n.Rows, n.Removed, n.Loops)
				}
				for _, child := range n.Plans {
					inspectRandomCandidate(child)
				}
			}
			var inspect func(planNode)
			inspect = func(n planNode) {
				if n.Subplan == "CTE work_batch" {
					materialized++
					if sortBy == "Random" {
						inspectRandomCandidate(n)
					}
					if n.Loops != 1 {
						t.Fatal("candidate evaluated repeatedly")
					}
				}
				if n.Relation == "hongguo_works" && (n.Rows+n.Removed)*n.Loops > 30000 {
					t.Fatalf("candidate repeats work projection: sort=%s phase=%d rows=%.0f removed=%.0f loops=%.0f", sortBy, phase, n.Rows, n.Removed, n.Loops)
				}
				if n.Relation == "media" {
					mediaVisits += (n.Rows + n.Removed) * n.Loops
				}
				if n.Relation == "hongguo_media_bindings" {
					bindingVisits += (n.Rows + n.Removed) * n.Loops
				}
				for _, child := range n.Plans {
					inspect(child)
				}
			}
			inspect(plans[0].Plan)
			if materialized != 1 {
				t.Fatalf("candidate materializations=%d", materialized)
			}
			maxVisits := 700000.0 // 日期排序保留一次必要的全量日期读取及资格检查。
			if sortBy != "" {
				maxVisits = 50000
			}
			if sortBy == "Random" && phase == 1 {
				maxVisits = 1000
			}
			if mediaVisits > maxVisits || bindingVisits > maxVisits {
				t.Fatalf("sort=%s candidate file visits media=%.0f bindings=%.0f", sortBy, mediaVisits, bindingVisits)
			}
			t.Logf("global sort=%s phase=%d: execution=%.3f ms JIT=%.3f ms media=%.0f bindings=%.0f", sortBy, phase, plans[0].ExecutionTime, plans[0].JIT.Timing.Total, mediaVisits, bindingVisits)
		}
	}
}

// 少量收藏不应在准确计数或取页时扫描全目录，也不应触发昂贵的 JIT 编译。
func assertGlobalFavoriteBrowsePlan(t *testing.T, e *EmbyService) {
	t.Helper()
	db := e.repo.DB
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:global-favorite-plan", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") {
			queries = append(queries, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Row().Remove("test:global-favorite-plan")
	for _, sortBy := range []string{"DateLastContentAdded,SortName", "SortName", "DateCreated"} {
		queries = nil
		p := ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Series"}, Filters: []string{"IsFavorite"},
			SortBy: sortBy, SortOrder: "Descending", Limit: 30, Fields: []string{"BasicSyncInfo"}}
		result, err := e.Items(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		items := result["Items"].([]map[string]any)
		if result["TotalRecordCount"] != int64(1) || len(items) != 1 || items[0]["Id"] != "hg-group-2000" || len(queries) != 2 {
			t.Fatalf("favorite sort=%s total=%v items=%v queries=%d", sortBy, result["TotalRecordCount"], items, len(queries))
		}
		for phase, query := range append([]statement(nil), queries...) {
			var raw []byte
			if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+query.sql, query.vars...).Scan(&raw); err != nil {
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
				JIT           struct{ Functions int }
			}
			if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
				t.Fatalf("favorite plan decode: %v", err)
			}
			visits := map[string]float64{}
			var inspect func(planNode)
			inspect = func(n planNode) {
				if n.Relation != "" {
					visits[n.Relation] += (n.Rows + n.Removed) * n.Loops
				}
				for _, child := range n.Plans {
					inspect(child)
				}
			}
			inspect(plans[0].Plan)
			t.Logf("favorite sort=%s phase=%d execution=%.3f ms JIT functions=%d visits=%v", sortBy, phase, plans[0].ExecutionTime, plans[0].JIT.Functions, visits)
			if visits["hongguo_works"] == 0 || visits["hongguo_works"] > 100 || visits["media"] > 1000 || visits["hongguo_media_bindings"] > 1000 || plans[0].JIT.Functions != 0 {
				t.Fatal("favorite count/page scans unrelated catalog or triggers JIT")
			}
		}
	}
}

func assertGlobalBrowseMatchesHierarchy(t *testing.T, e *EmbyService, user string) {
	t.Helper()
	ctx := t.Context()
	for _, mode := range []struct {
		sort, direction, filter, search string
		types                           []string
	}{
		{"", "", "IsUnplayed", "", nil},
		{"SortName", "Ascending", "", "", []string{"Movie", "Series"}},
		{"Random", "", "IsUnplayed", "", []string{"Movie", "Series"}},
		{"DateCreated", "Ascending", "", "", []string{"Movie", "Series"}},
		{"DateCreated", "Descending", "IsPlayed", "", []string{"Movie", "Series"}},
		{"DatePlayed", "Descending", "IsUnplayed", "", []string{"Movie", "Series"}},
		{"PremiereDate", "Ascending", "", "", []string{"Movie", "Series"}},
		{"DateCreated", "Descending", "IsPlayed", "", nil},
		{"DateLastContentAdded", "Descending", "IsUnplayed", "", []string{"Series"}},
		{"DateLastContentAdded,SortName", "Descending", "IsFavorite", "", []string{"Series"}},
		{"DateCreated", "Descending", "IsFavorite", "", []string{"Movie", "Series"}},
		{"SortName", "Ascending", "IsFavorite", "", []string{"Movie", "Series"}},
		{"DatePlayed", "Ascending", "", "", []string{"Season", "Episode"}},
		{"CommunityRating", "Descending", "IsFavorite", "", nil},
		{"PremiereDate", "Descending", "IsUnplayed", "同名", []string{"Movie", "Series"}},
		{"SortName", "Descending", "", "not-found", nil},
	} {
		p := ItemsParams{UserID: user, Recursive: true, SortBy: mode.sort, SortOrder: mode.direction, IncludeItemTypes: mode.types,
			SearchTerm: mode.search, Limit: 3, Fields: []string{"BasicSyncInfo"}}
		if mode.filter != "" {
			p.Filters = []string{mode.filter}
		}
		var expected []string
		if err := originalGlobalBrowseCandidates(t, e, p).Order(globalItemsOrder(p)).Pluck("id", &expected).Error; err != nil {
			t.Fatal(err)
		}
		for _, offset := range []int{0, 2, len(expected), len(expected) + 1} {
			p.StartIndex = offset
			got, handled, err := e.hongGuoGlobalItems(ctx, p)
			if err != nil || !handled {
				t.Fatalf("global mode=%+v: handled=%v err=%v", mode, handled, err)
			}
			var ids []string
			for _, item := range got["Items"].([]map[string]any) {
				ids = append(ids, item["Id"].(string))
			}
			want := pageSlice(expected, offset, p.Limit)
			if got["TotalRecordCount"] != int64(len(expected)) || len(ids) != len(want) || len(ids) > 0 && !reflect.DeepEqual(ids, want) {
				t.Fatalf("global mode=%+v offset=%d: ids=%v want=%v total=%v expected total=%d", mode, offset, ids, want, got["TotalRecordCount"], len(expected))
			}
		}
	}
	// 父子混合输入必须与完整节点相同，不能重复文件或拿部分父节点作整剧状态。
	for _, source := range []string{"hongguo", "nfo"} {
		q := e.hongGuoNodes(ctx, user, "")
		if source == "nfo" {
			q = e.nfoNodes(ctx, user, "")
		}
		var expected []hongGuoNode
		if err := q.Order("id").Scan(&expected).Error; err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, row := range expected {
			ids = append(ids, row.ID)
		}
		if len(ids) == 0 {
			continue
		}
		q = e.hongGuoItemNodes(ctx, user, ids...)
		if source == "nfo" {
			q = e.nfoItemNodes(ctx, user, ids...)
		}
		var got []hongGuoNode
		if err := q.Where("id IN ?", ids).Order("id").Scan(&got).Error; err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatalf("%s batch nodes differ: err=%v", source, err)
		}
	}
}

func TestEmbyGlobalLatestDoesNotCount(t *testing.T) {
	e := nfoBrowseFixture(t, 3, 4)
	var sql string
	if err := e.repo.DB.Callback().Row().After("gorm:row").Register("test:latest-count", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") {
			if strings.Contains(tx.Statement.SQL.String(), "SELECT COUNT(*) FROM") {
				t.Error("Latest must not count candidates")
			}
			sql = tx.Statement.SQL.String()
		}
	}); err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{UserID: "viewer", Recursive: true, Limit: 3, Filters: []string{"IsUnplayed"}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
	var expected []string
	if err := originalGlobalBrowseCandidates(t, e, p).Order(globalItemsOrder(p)).Limit(p.Limit).Pluck("id", &expected).Error; err != nil {
		t.Fatal(err)
	}
	items, err := e.LatestItems(t.Context(), p.UserID, "", p.Limit, false, "BasicSyncInfo")
	if err != nil || len(items) != len(expected) || sql == "" || strings.Contains(sql, "COUNT(*) AS total FROM candidates") {
		t.Fatalf("latest count or result mismatch: items=%d err=%v sql=%s", len(items), err, sql)
	}
	for i := range items {
		if items[i]["Id"] != expected[i] {
			t.Fatalf("latest order %v want %v", items[i]["Id"], expected[i])
		}
	}
}

func TestEmbyGlobalBrowseCandidateBatches(t *testing.T) {
	e := nfoBrowseFixture(t, 3, 4)
	queries := 0
	capture := func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") {
			queries++
		}
	}
	if err := e.repo.DB.Callback().Row().After("gorm:row").Register("test:global-candidates", capture); err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{UserID: "viewer", Recursive: true, IncludeItemTypes: []string{"Series"}, Limit: 2}
	got, _, err := e.hongGuoGlobalItems(t.Context(), p)
	if err != nil || queries != 2 || got["TotalRecordCount"] != int64(3) {
		t.Fatalf("queries=%d page=%v err=%v", queries, got, err)
	}
	assertGlobalBrowseMatchesHierarchy(t, e, "viewer")
	for _, visibility := range []MediaVisibility{
		{LibraryRestricted: true},
		{HiddenLibraryIDs: []string{"library-nfo"}},
		{AllowedLibraryIDs: []string{"missing-library"}},
	} {
		e.visibilityCache[e.repo.ReadCacheKey()+"viewer"] = embyVisibilityCacheEntry{visibility: visibility, expiresAt: time.Now().Add(time.Hour)}
		got, _, err := e.hongGuoGlobalItems(t.Context(), p)
		if err != nil || got["TotalRecordCount"] != int64(0) || len(got["Items"].([]map[string]any)) != 0 {
			t.Fatalf("invisible global page: %v err=%v", got, err)
		}
	}
}

func TestEmbyGlobalWorkDirectBindingsPreserveGroups(t *testing.T) {
	e := nfoBrowseFixture(t, 1, 2)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('direct-series','series','Series','local'),('direct-movie','movie','Movie','local')`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id) VALUES ('direct-season','season','Season','local','direct-series'),('direct-episode','episode','Episode','local','direct-season')`,
		`INSERT INTO media(id,library_id,metadata_id,path,created_at) SELECT 'file-'||id,'direct-library',id,'/direct/'||id,'2026-01-01' FROM metadata_items WHERE id LIKE 'direct-%'`,
		`INSERT INTO playback_histories(id,user_id,media_id,metadata_id,completed) VALUES ('direct-played','viewer','file-direct-episode','direct-episode',true)`,
	} {
		if err := e.repo.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, sortBy := range []string{"SortName", "DateCreated", "DateLastContentAdded", "CommunityRating"} {
		for _, filter := range []string{"", "IsPlayed", "IsUnplayed"} {
			p := ItemsParams{UserID: "viewer", IncludeItemTypes: []string{"Movie", "Series"}, SortBy: sortBy, Filters: []string{filter}}
			var want, got []string
			if err := originalGlobalBrowseCandidates(t, e, p).Order(globalItemsOrder(p)).Pluck("id", &want).Error; err != nil {
				t.Fatal(err)
			}
			result, _, err := e.globalItemsWithCount(t.Context(), p, true)
			if err == nil {
				for _, item := range result["Items"].([]map[string]any) {
					got = append(got, item["Id"].(string))
				}
			}
			if err != nil || len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) || result["TotalRecordCount"] != int64(len(want)) {
				t.Fatalf("sort=%s filter=%s got=%v want=%v err=%v", sortBy, filter, got, want, err)
			}
		}
	}
}
