package repository

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestRecentLogicalWorksPreservesBatchTiesAndFilters(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source,nsfw) VALUES ('newest','movie','Newest','local',false),('a-tie','movie','A','local',false),('z-tie','movie','Z','local',false),('old','movie','Old','local',false),('nsfw','movie','Hidden','local',true)`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'new-'||i,'newest','recent','/new/'||i,'2026-01-03' FROM generate_series(1,127) i`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) VALUES ('zzz-tie','a-tie','recent','/tie/a','2026-01-02'),('aaa-tie','z-tie','recent','/tie/z','2026-01-02'),('hidden','nsfw','hidden','/hidden','2026-01-04')`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'old-'||i,'old','recent','/old/'||i,'2020-01-01' FROM generate_series(1,10000) i`,
		`ANALYZE media`, `ANALYZE metadata_items`,
	} {
		if err := repos.DB.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
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
		got, err := repos.MediaView.ListRecentLogicalWorks(t.Context(), 2, filter)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("filter=%+v got=%d want=%d err=%v", filter, len(got), len(want), err)
		}
	}
	for _, filter := range []MediaQueryFilter{{}, {IncludeNSFW: true}, {AllowedLibraryIDs: []string{"recent"}}, {HiddenLibraryIDs: []string{"hidden"}}, {HiddenLibraryIDs: []string{"recent", "hidden"}}, {MissingPoster: true}, {MissingChineseTitle: true}} {
		compare(filter)
	}
	for _, cursor := range []string{"", " AND (created_at,id) < ('2020-01-01','old-9000')"} {
		var raw string
		if err := repos.DB.Raw(`EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) SELECT id,created_at FROM media WHERE metadata_id IS NOT NULL` + cursor + ` ORDER BY created_at DESC,id DESC LIMIT 128`).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		type node struct {
			Index string `json:"Index Name"`
			Type  string `json:"Node Type"`
			Plans []node `json:"Plans"`
		}
		var plans []struct{ Plan node }
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid plan: %s err=%v", raw, err)
		}
		found := false
		var check func(node)
		check = func(n node) {
			found = found || n.Index == "idx_media_recent_metadata"
			if n.Type == "Sort" || n.Type == "Seq Scan" {
				t.Fatalf("recent candidate page scanned/sorted all files: %s", raw)
			}
			for _, child := range n.Plans {
				check(child)
			}
		}
		check(plans[0].Plan)
		if !found {
			t.Fatalf("recent index not used: %s", raw)
		}
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
		{IncludeNSFW: true}, {AllowedLibraryIDs: []string{"a"}}, {AllowedLibraryIDs: []string{"b"}},
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
		for _, filter := range []MediaQueryFilter{{}, {IncludeNSFW: true}, {HiddenLibraryIDs: []string{library.ID}}, {AllowedLibraryIDs: []string{library.ID}}, {AllowedLibraryIDs: []string{"missing"}}, {MissingPoster: true}, {MissingChineseTitle: true}} {
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
