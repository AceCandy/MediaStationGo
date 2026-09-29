package repository

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestRecentLogicalWorksPreservesBatchTiesAndFilters(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('newest','movie','Newest','local'),('a-tie','movie','A','local'),('z-tie','movie','Z','local'),('old','movie','Old','local'),('nsfw','movie','Hidden','local')`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'new-'||i,'newest','recent','/new/'||i,'2026-01-03' FROM generate_series(1,127) i`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) VALUES ('zzz-tie','a-tie','recent','/tie/a','2026-01-02'),('aaa-tie','z-tie','recent','/tie/z','2026-01-02'),('hidden','nsfw','hidden','/hidden','2026-01-04')`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'old-'||i,'old','recent','/old/'||i,'2020-01-01' FROM generate_series(1,10000) i`,
		`ANALYZE media`, `ANALYZE metadata_items`,
	} {
		if err := repos.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	capture := func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); !tx.DryRun && strings.Contains(sql, "recent_works") {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := repos.DB.Callback().Row().After("gorm:row").Register("test:recent-page", capture); err != nil {
		t.Fatal(err)
	}
	compare := func(filter MediaQueryFilter) {
		t.Helper()
		logicalID := "CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.id, mi.id) ELSE mi.id END"
		var ids []string
		if err := applyMediaViewFilter(repos.MediaView.query(t.Context()), filter).
			Select(logicalID + " AS logical_id").Group(logicalID).
			Order("MAX(m.created_at) DESC NULLS LAST, logical_id DESC").Limit(2).Scan(&ids).Error; err != nil {
			t.Fatal(err)
		}
		want, err := repos.MediaView.FindByLogicalMetadataIDs(t.Context(), ids, filter)
		if err != nil {
			t.Fatal(err)
		}
		queries = nil
		got, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), 2, filter)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("filter=%+v got=%d want=%d err=%v", filter, len(got), len(want), err)
		}
		if len(queries) != 1 || !strings.HasPrefix(queries[0].sql, "WITH work_batch AS MATERIALIZED") {
			t.Fatalf("recent candidates queried %d times; dates must travel with the selected page", len(queries))
		}
		var raw string
		if err := repos.DB.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+queries[0].sql, queries[0].vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		type node struct {
			Subplan  string  `json:"Subplan Name"`
			Relation string  `json:"Relation Name"`
			Rows     float64 `json:"Actual Rows"`
			Filtered float64 `json:"Rows Removed by Filter"`
			Loops    float64 `json:"Actual Loops"`
			Plans    []node  `json:"Plans"`
		}
		var plans []struct{ Plan node }
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid plan: %v", err)
		}
		var visits float64
		found := false
		var check func(node, bool)
		check = func(n node, candidate bool) {
			if n.Subplan == "CTE work_batch" {
				candidate, found = true, true
			}
			if candidate && n.Relation == "media" {
				visits += (n.Rows + n.Filtered) * n.Loops
			}
			for _, child := range n.Plans {
				check(child, candidate)
			}
		}
		check(plans[0].Plan, false)
		// 候选枚举只读作品；缺海报等文件资格属于独立 qualified 阶段。
		if !found || visits != 0 {
			t.Fatalf("filter=%+v recent candidate file visits=%.0f inspected=%v", filter, visits, found)
		}
	}
	for _, filter := range []MediaQueryFilter{{}, {}, {AllowedLibraryIDs: []string{"recent"}}, {HiddenLibraryIDs: []string{"hidden"}}, {HiddenLibraryIDs: []string{"recent", "hidden"}}, {MissingPoster: true}, {MissingChineseTitle: true}} {
		compare(filter)
	}
	// 混合 NULL/非 NULL 的作品仍沿用 MAX 语义，不可将 NULL 文件当作最新作品。
	if err := repos.DB.Exec(`UPDATE media SET created_at=NULL WHERE id='aaa-tie'`).Error; err != nil {
		t.Fatal(err)
	}
	compare(MediaQueryFilter{})
	// 严格筛选下没有命中，超过游标批次上限仍须返回与全量聚合相同的结果。
	if err := repos.DB.Exec(`UPDATE media SET created_at='2026-01-02' WHERE id='aaa-tie'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Exec(`UPDATE metadata_items SET title='中文标题'`).Error; err != nil {
		t.Fatal(err)
	}
	compare(MediaQueryFilter{MissingChineseTitle: true})
}

func TestRecentWorkTimeSharedAcrossLibraries(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('shared','movie','Shared','local'),('other','movie','Other','local')`,
		`INSERT INTO media(id,path,library_id,metadata_id,created_at) VALUES ('a','/test/a','a','shared','2026-01-01'),('b','/test/b','b','shared','2026-01-03'),('c','/test/c','a','other','2026-01-02')`,
	} {
		if err := repos.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"shared", "other"} {
		rows, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), 1, MediaQueryFilter{AllowedLibraryIDs: []string{"a"}})
		if err != nil || len(rows) != 1 || rows[0].MetadataID != want || rows[0].LibraryID != "a" {
			t.Fatalf("shared latest want=%s rows=%d err=%v", want, len(rows), err)
		}
		if err := repos.DB.Exec("DELETE FROM media WHERE id='b'").Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecentHongGuoAlbumsAggregateOnce(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	db := repos.DB
	for _, sql := range []string{
		`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at)
SELECT 'work-'||n,n::text,'series','Show',((n-1)/3+1000)::text,(n-1)%3+1,now() FROM generate_series(1,6000) n`,
		`INSERT INTO hongguo_episodes(id,work_id,number) VALUES ('ep-1','work-1',1),('ep-2','work-2',1),('ep-4','work-4',1)`,
		`INSERT INTO media(id,path,library_id,catalog_source,created_at) VALUES ('f1','/fixture/f1','visible','hongguo','2026-01-01'),('f2','/fixture/f2','hidden','hongguo','2026-02-01'),('f4','/fixture/f4','visible','hongguo','2026-01-02')`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) VALUES ('f1','work-1','ep-1'),('f2','work-2','ep-2'),('f4','work-4','ep-4')`,
		`ANALYZE hongguo_works`, `ANALYZE hongguo_episodes`, `ANALYZE hongguo_media_bindings`, `ANALYZE media`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var query string
	var vars []any
	if err := db.Callback().Row().After("gorm:row").Register("test:recent-albums", func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); !tx.DryRun && strings.HasPrefix(sql, "WITH work_batch AS MATERIALIZED") {
			query, vars = sql, append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), 2, MediaQueryFilter{AllowedLibraryIDs: []string{"visible"}})
	if err != nil || len(rows) != 2 {
		t.Fatalf("recent rows=%d err=%v", len(rows), err)
	}
	for i, want := range []struct{ id, date string }{{"f1", "2026-02-01"}, {"f4", "2026-01-02"}} {
		if rows[i].ID != want.id || rows[i].LatestMediaAddedAt == nil || rows[i].LatestMediaAddedAt.Format("2006-01-02") != want.date {
			t.Fatalf("row %d lost global album date/order: id=%s date=%v", i, rows[i].ID, rows[i].LatestMediaAddedAt)
		}
	}
	if query == "" {
		t.Fatal("did not capture actual recent candidates")
	}
	var raw string
	if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+query, vars...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type node struct {
		Relation string  `json:"Relation Name"`
		Rows     float64 `json:"Actual Rows"`
		Filtered float64 `json:"Rows Removed by Filter"`
		Rejected float64 `json:"Rows Removed by Join Filter"`
		Loops    float64 `json:"Actual Loops"`
		Plans    []node  `json:"Plans"`
	}
	var plans []struct {
		Plan node
		Time float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid plan: %v", err)
	}
	var workVisits, rejected float64
	var inspect func(node)
	inspect = func(n node) {
		if n.Relation == "hongguo_works" {
			workVisits += (n.Rows + n.Filtered) * n.Loops
		}
		rejected += n.Rejected * n.Loops
		for _, child := range n.Plans {
			inspect(child)
		}
	}
	inspect(plans[0].Plan)
	// 一次作品候选和一次全局合集聚合，不能再按每个作品重算全部合集成员。
	if workVisits == 0 || workVisits > 12100 || rejected > 12100 {
		t.Errorf("album candidate work visits=%.0f join rejections=%.0f", workVisits, rejected)
	}
	t.Logf("recent album candidates %.3f ms work visits=%.0f", plans[0].Time, workVisits)
}

func TestRecentLogicalWorksRefillsWithDates(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	db := repos.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'work-'||n,'movie',CASE WHEN n IN (51,103,155) THEN 'Show' ELSE '中文' END,'local' FROM generate_series(1,155) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'file-'||n,'work-'||n,'visible','/fixture/'||n,TIMESTAMP '2026-01-01' - n * INTERVAL '1 hour' FROM generate_series(1,155) n`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	batches, dateQueries := 0, 0
	if err := db.Callback().Row().After("gorm:row").Register("test:recent-refill", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if tx.DryRun || !strings.Contains(sql, "recent_works") {
			return
		}
		if strings.HasPrefix(sql, "WITH work_batch AS MATERIALIZED") {
			batches++
		} else {
			dateQueries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), 3, MediaQueryFilter{MissingChineseTitle: true})
	if err != nil || len(rows) != 3 || batches != 4 || dateQueries != 0 {
		t.Fatalf("refill rows=%d batches=%d dateQueries=%d err=%v", len(rows), batches, dateQueries, err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, want := range []struct {
		id    string
		hours int
	}{{"work-51", 51}, {"work-103", 103}, {"work-155", 155}} {
		if rows[i].MetadataID != want.id || rows[i].LatestMediaAddedAt == nil || !rows[i].LatestMediaAddedAt.Equal(base.Add(-time.Duration(want.hours)*time.Hour)) {
			t.Fatalf("refill row %d id=%s date=%v", i, rows[i].MetadataID, rows[i].LatestMediaAddedAt)
		}
	}
}

func TestRecentLibraryMembershipAcrossSources(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	db := repos.DB
	exec := func(sql string) {
		t.Helper()
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('shared','movie','Shared','local')`,
		`INSERT INTO nfo_items(id,library_id,local_key,kind,title) VALUES ('local-a','a','local','movie','Local A'),('local-b','b','local','movie','Local B')`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at) VALUES ('first','101','series','First','1000',1,now()),('second','102','series','Second','1000',2,now()),('third','103','series','Third','1000',3,now())`,
		`INSERT INTO hongguo_episodes(id,work_id,number) VALUES ('second-ep','second',1),('third-ep','third',1)`,
		`INSERT INTO media(id,path,library_id,metadata_id,created_at) VALUES ('ordinary-a','/test/ordinary-a','a','shared','2026-01-02'),('ordinary-b','/test/ordinary-b','b','shared','2026-01-05')`,
		`INSERT INTO media(id,path,library_id,catalog_source,created_at) VALUES ('local-a','/test/local-a','a','nfo','2026-01-03'),('local-b','/test/local-b','b','nfo','2026-01-04'),('source-a','/test/source-a','a','hongguo','2026-01-01'),('source-b','/test/source-b','b','hongguo','2026-01-06')`,
		`INSERT INTO nfo_media_bindings(media_id,item_id,title,fingerprint) VALUES ('local-a','local-a','Local A','test'),('local-b','local-b','Local B','test')`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) VALUES ('source-a','second','second-ep'),('source-b','third','third-ep')`,
	} {
		exec(sql)
	}
	// 索引集合与尚未补算的 NULL 必须返回相同完整结果；受限空权限、隐藏库仍拒绝。
	filters := []MediaQueryFilter{
		{}, {AllowedLibraryIDs: []string{"a"}}, {AllowedLibraryIDs: []string{"b"}},
		{AllowedLibraryIDs: []string{"a", "b"}}, {AllowedLibraryIDs: []string{"__locked__"}},
		{AllowedLibraryIDs: []string{"a", "b"}, HiddenLibraryIDs: []string{"b"}},
		{AllowedLibraryIDs: []string{"a"}, HiddenLibraryIDs: []string{"a"}},
		{AllowedLibraryIDs: []string{`a",b`, "a"}},
	}
	for _, limit := range []int{1, 3, 10} {
		for _, filter := range filters {
			known, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), limit, filter)
			if err != nil {
				t.Fatal(err)
			}
			// 只清空部分来源，覆盖混合已初始化/未知归属。
			exec(`UPDATE metadata_items SET library_ids=NULL`)
			exec(`UPDATE hongguo_works SET library_ids=NULL WHERE id='second'`)
			unknown, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), limit, filter)
			if err != nil || !reflect.DeepEqual(known, unknown) {
				t.Fatalf("membership limit=%d filter=%+v known=%d unknown=%d err=%v", limit, filter, len(known), len(unknown), err)
			}
			exec(`SELECT latest_media_refresh('metadata_items', ARRAY['shared'])`)
			exec(`SELECT latest_media_refresh('hongguo_works', ARRAY['second'])`)
		}
	}
	album, title := "hg-group-1000", "First"
	check := func(library string, want []string) {
		t.Helper()
		rows, err := repos.MediaView.ListRecentByLibraries(t.Context(), []string{library}, 10, MediaQueryFilter{})
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, row := range rows {
			if row.LibraryID != library {
				t.Fatalf("file leaked from another library: %s", row.LibraryID)
			}
			if row.ID == "source-a" || row.ID == "source-b" {
				if row.SeriesID != album || row.SeriesTitle != title {
					t.Fatalf("album representative: id=%s title=%s want=%s/%s", row.SeriesID, row.SeriesTitle, album, title)
				}
			}
			ids = append(ids, row.ID)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("library=%s ids=%v want=%v", library, ids, want)
		}
	}
	check("a", []string{"source-a", "ordinary-a", "local-a"})
	check("b", []string{"source-b", "ordinary-b", "local-b"})
	exec(`DELETE FROM media WHERE id IN ('source-a','ordinary-a')`)
	check("a", []string{"local-a"})
	check("b", []string{"source-b", "ordinary-b", "local-b"})
	exec(`UPDATE media SET library_id='a' WHERE id IN ('source-b','ordinary-b')`)
	check("a", []string{"source-b", "ordinary-b", "local-a"})
	check("b", []string{"local-b"})
	// 首季无文件不影响合集代表；关系变更无需将合集归属写回首季。
	exec(`UPDATE hongguo_works SET related_album_id='2000' WHERE id='third'`)
	album, title = "hg-group-2000", "Third"
	check("a", []string{"source-b", "ordinary-b", "local-a"})
}

func TestRecentWorksMergeAllSourcesBeforePaging(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('ordinary','movie','Ordinary','local')`,
		`INSERT INTO nfo_items(id,library_id,local_key,kind,title) VALUES ('local','a','local','movie','Local')`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) VALUES ('source','100','movie','Source',now())`,
		`INSERT INTO media(id,path,library_id,metadata_id,created_at) VALUES ('ordinary-file','/test/ordinary','a','ordinary','2026-01-02')`,
		`INSERT INTO media(id,path,library_id,catalog_source,created_at) VALUES ('nfo-file','/test/nfo','a','nfo','2026-01-03'),('hg-file','/test/hg','a','hongguo','2026-01-01')`,
		`INSERT INTO nfo_media_bindings(media_id,item_id,title,fingerprint) VALUES ('nfo-file','local','Local','test')`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id) VALUES ('hg-file','source')`,
	} {
		if err := repos.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := repos.MediaView.ListRecentByLibraries(t.Context(), []string{"a"}, 3, MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if !reflect.DeepEqual(ids, []string{"nfo-file", "ordinary-file", "hg-file"}) {
		t.Fatalf("cross-source order: %v", ids)
	}
	rows, err = repos.MediaView.ListRecentByLibraries(t.Context(), []string{"a"}, 1, MediaQueryFilter{})
	if err != nil || len(rows) != 1 || rows[0].ID != "nfo-file" {
		t.Fatalf("cross-source limit: rows=%d err=%v", len(rows), err)
	}
}

func TestMediaViewLogicalScopePreservesResults(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	library := model.Library{Name: "logical", Path: "/logical", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	seriesID, seasonID := "logical-series", "logical-season"
	items := []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"},
		{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: model.MetadataKindSeason, ParentID: &seriesID, SeasonNum: 1, Title: "Season", Source: "tmdb"},
		{PermanentBase: model.PermanentBase{ID: "logical-episode"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1, Title: "Episode", Source: "tmdb"},
		{PermanentBase: model.PermanentBase{ID: "logical-movie"}, Kind: model.MetadataKindMovie, Title: "电影", Source: "tmdb"},
	}
	for _, item := range items {
		if err := repos.DB.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"a", "b"} {
			m := model.Media{PermanentBase: model.PermanentBase{ID: item.ID + version}, LibraryID: library.ID, MetadataID: item.ID, Path: "/logical/" + item.ID + version}
			if err := repos.DB.Create(&m).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, ids := range [][]string{{seriesID}, {seasonID}, {"logical-episode"}, {"logical-movie"}, {seriesID, "logical-episode", seriesID, "logical-movie"}, {"missing"}} {
		for _, filter := range []MediaQueryFilter{{}, {}, {HiddenLibraryIDs: []string{library.ID}}, {AllowedLibraryIDs: []string{library.ID}}, {AllowedLibraryIDs: []string{"missing"}}, {MissingPoster: true}, {MissingChineseTitle: true}} {
			var want []model.MediaView
			old := repos.MediaView.query(t.Context()).Where("m.metadata_id IN ? OR CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.id, mi.id) ELSE mi.id END IN ?", ids, ids)
			if err := scanMediaViews(applyMediaViewFilter(old, filter).Order("m.created_at DESC, m.id DESC"), &want); err != nil {
				t.Fatal(err)
			}
			got, err := repos.MediaView.FindByLogicalMetadataIDs(t.Context(), ids, filter)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ids=%v filter=%+v: got %d rows, want %d (including complete projection and order)", ids, filter, len(got), len(want))
			}
		}
	}
}
