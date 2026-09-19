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
			Order("MAX(m.created_at) DESC, logical_id DESC").Limit(2).Scan(&ids).Error; err != nil {
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
