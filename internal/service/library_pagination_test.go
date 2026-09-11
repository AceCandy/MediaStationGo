package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm/logger"
)

func TestLibrarySeriesPagePreservesDirectFilesAndTies(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items (id,kind,title,source,release_date) VALUES
('page-a','series','A','local','2024-01-01'),('page-b','series','B','local','2024-01-01'),
('page-c','series','C','local',''),('page-empty','series','Empty','local','')`,
		`INSERT INTO metadata_items (id,kind,parent_id,title,source,season_num,release_date) VALUES
('page-season','season','page-a','Season','local',0,'2024-01-01')`,
		`INSERT INTO metadata_items (id,kind,parent_id,title,source,episode_num,year) VALUES
('page-episode','episode','page-season','Episode','local',1,2023)`,
		`INSERT INTO media (id,library_id,metadata_id,path,created_at,updated_at) VALUES
('file-b','page-library','page-a','/fixture/page/b','2020-01-01','2020-01-01'),
('file-a','page-library','page-a','/fixture/page/a','2020-01-01','2020-01-01'),
('file-season','page-library','page-season','/fixture/page/season','2020-01-01','2020-01-01'),
('file-episode','page-library','page-episode','/fixture/page/episode','2020-01-01','2020-01-01'),
('file-other','page-other','page-episode','/fixture/page/other','2020-01-01','2025-01-01'),
('file-show-b','page-library','page-b','/fixture/page/show-b','2020-01-01','2020-01-01'),
('file-show-c','page-library','page-c','/fixture/page/show-c','2020-01-01','2025-01-01')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 文件时间兜底优先；相同发布日期按作品 ID 倒序；代表文件按 ID 正序打破平局。
	want := []repository.LibraryMetadataSummary{
		{MetadataID: "page-c", MediaID: "file-show-c", Count: 1, VersionCount: 1},
		{MetadataID: "page-b", MediaID: "file-show-b", Count: 1, VersionCount: 1},
		{MetadataID: "page-a", MediaID: "file-a", Count: 3, VersionCount: 4},
	}
	for _, filter := range []repository.MediaQueryFilter{
		{IncludeNSFW: true},
		{IncludeNSFW: true, MissingPoster: true},
		{IncludeNSFW: true, MissingChineseTitle: true},
		{IncludeNSFW: true, MissingPoster: true, MissingChineseTitle: true},
	} {
		for offset := 0; offset <= len(want); offset++ {
			_, rows, total, err := svc.repo.MediaView.ListLibraryMetadataPage(t.Context(), "page-library", "series", "", offset, 1, filter)
			if err != nil || total != int64(len(want)) {
				t.Fatalf("offset=%d total=%d err=%v", offset, total, err)
			}
			if offset == len(want) {
				if len(rows) != 0 {
					t.Fatalf("past-end rows=%+v", rows)
				}
			} else if !reflect.DeepEqual(rows, want[offset:offset+1]) {
				t.Fatalf("offset=%d rows=%+v want=%+v", offset, rows, want[offset:offset+1])
			}
		}
	}
}

type paginationReadLog struct {
	logger.Interface
	fileRows         int64
	viewSQL          string
	seriesSQL        string
	moviePageQueries int
}

func (l *paginationReadLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.HasPrefix(sql, "WITH scoped AS MATERIALIZED") {
		l.seriesSQL = sql
	}
	if strings.HasPrefix(sql, "SELECT work.id AS metadata_id") {
		l.moviePageQueries++
	}
	if strings.Contains(sql, "view_title") || strings.Contains(sql, `"media"."path"`) {
		l.viewSQL = sql
		if rows > 0 {
			l.fileRows += rows
		}
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestLibrarySeriesEpisodesScopesProjectionBeforeJoins(t *testing.T) {
	emby := newTestEmbyService(t)
	db := emby.repo.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items (id, kind, title, source) VALUES ('bounded-series', 'series', 'Series', 'local')`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num)
		 VALUES ('bounded-season', 'season', 'bounded-series', 'Season', 'local', 1)`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, episode_num)
		 SELECT 'bounded-episode-' || n, 'episode', 'bounded-season', 'Episode', 'local', n FROM generate_series(1,12) n`,
		`INSERT INTO metadata_items (id, kind, title, source)
		 SELECT 'other-metadata-' || n, 'movie', 'Other', 'local' FROM generate_series(1,10000) n`,
		`INSERT INTO media (id, library_id, metadata_id, path, created_at, updated_at)
		 SELECT 'other-file-' || n, 'bounded-library', 'other-metadata-' || n, '/fixture/other/' || n, NOW(), NOW() FROM generate_series(1,10000) n`,
		`INSERT INTO media (id, library_id, metadata_id, path, season_num, episode_num, created_at, updated_at)
		 SELECT 'bounded-file-' || n, 'bounded-library', 'bounded-episode-' || n, '/fixture/series/' || n, 1, n, NOW(), NOW() FROM generate_series(1,12) n`,
		`ANALYZE media`,
		`ANALYZE metadata_items`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	reads := &paginationReadLog{Interface: db.Logger}
	db.Logger = reads
	rows, err := emby.repo.MediaView.ListLibrarySeriesViews(t.Context(), "bounded-library", "bounded-series", repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || len(rows) != 12 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	query := reads.viewSQL
	for i, row := range rows {
		if row.SeriesID != "bounded-series" || row.EpisodeNum != i+1 {
			t.Fatal("series scope or episode order changed")
		}
	}
	var raw string
	if err := db.Raw("EXPLAIN (ANALYZE, FORMAT JSON) " + query).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		Relation string     `json:"Relation Name"`
		Loops    float64    `json:"Actual Loops"`
		Plans    []planNode `json:"Plans"`
	}
	var plans []struct{ Plan planNode }
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid plan: %v", err)
	}
	var check func(planNode)
	check = func(node planNode) {
		if node.Relation == "metadata_identifiers" && node.Loops > float64(len(rows)) {
			t.Errorf("identifier lookups expanded to %.0f for %d selected files", node.Loops, len(rows))
		}
		for _, child := range node.Plans {
			check(child)
		}
	}
	check(plans[0].Plan)
}

func TestLibraryMetadataPaginationBoundsFileReads(t *testing.T) {
	emby := newTestEmbyService(t)
	db, repo := emby.repo.DB, emby.repo
	web := NewMediaService(emby.cfg, emby.log, repo)
	lib := model.Library{Name: "分页测试", Type: "tv", Path: "/fixture/pagination", Enabled: true}
	other := model.Library{Name: "另一个库", Type: "tv", Path: "/fixture/other", Enabled: true}
	for _, library := range []*model.Library{&lib, &other} {
		if err := repo.Library.Create(t.Context(), library); err != nil {
			t.Fatal(err)
		}
	}
	const episodeCount = 24
	for show := 1; show <= 2; show++ {
		seriesID := fmt.Sprintf("page-series-%d", show)
		series := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: model.MetadataKindSeries, Title: fmt.Sprintf("Series %d", show), ReleaseDate: fmt.Sprintf("202%d-01-01", show)})
		season := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: seriesID + "-season"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "第一季"})
		for ep := 1; ep <= episodeCount; ep++ {
			episode := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("%s-e%d", seriesID, ep)}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: ep, Title: "分集", ReleaseDate: series.ReleaseDate})
			for version := 0; version < 3; version++ {
				libraryID := lib.ID
				if version == 2 {
					libraryID = other.ID
				}
				media := model.Media{LibraryID: libraryID, MetadataID: episode.ID, Path: fmt.Sprintf("/fixture/pagination/%s/Season 1/S01E%02d-v%d.mkv", seriesID, ep, version), SeasonNum: 1, EpisodeNum: ep}
				if err := db.Create(&media).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	reads := &paginationReadLog{Interface: db.Logger}
	db.Logger = reads
	visibility := MediaVisibility{AllowedLibraryIDs: []string{lib.ID}, HiddenLibraryIDs: []string{other.ID}}
	cards, total, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", visibility)
	if err != nil || total != 2 || len(cards) != 1 || cards[0].Count != episodeCount || cards[0].Rep.Title != "Series 2" {
		t.Fatalf("web page=%+v total=%d err=%v", cards, total, err)
	}
	if reads.fileRows != 1 {
		t.Fatalf("one card loaded %d file rows", reads.fileRows)
	}
	first := cards[0]
	if empty, n, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 3, 1, "", "", visibility); err != nil || n != 2 || len(empty) != 0 {
		t.Fatalf("past-end series page=%+v total=%d err=%v", empty, n, err)
	}
	if empty, n, err := web.ListLibrarySeriesCards(t.Context(), "missing-library", 1, 1, "", "", MediaVisibility{}); err != nil || n != 0 || len(empty) != 0 {
		t.Fatalf("empty library page=%+v total=%d err=%v", empty, n, err)
	}
	cards, total, err = web.ListLibrarySeriesCards(t.Context(), lib.ID, 2, 1, "", "", visibility)
	if err != nil || total != 2 || len(cards) != 1 || cards[0].Key == first.Key {
		t.Fatalf("second page=%+v total=%d err=%v", cards, total, err)
	}
	rows, err := web.ListLibrarySeriesEpisodes(t.Context(), lib.ID, first.Key, visibility)
	if err != nil || len(rows) != episodeCount*2 {
		t.Fatalf("selected series rows=%d err=%v", len(rows), err)
	}
	for _, row := range rows {
		if row.LibraryID != lib.ID || row.SeriesID != first.Rep.SeriesID {
			t.Fatal("series scope leaked")
		}
	}
	legacy := compactSeriesKey("series:" + first.Rep.SeriesID)
	legacyCards, _, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", legacy, visibility)
	if err != nil || len(legacyCards) != 1 || legacyCards[0].Key != legacy {
		t.Fatalf("legacy link=%+v err=%v", legacyCards, err)
	}
	if _, err := web.GetMediaSeriesVisible(t.Context(), first.Rep.ID, visibility); err != nil {
		t.Fatal(err)
	}
	createServiceTestArtwork(t, db, first.Rep.SeriesID, model.ArtworkTypePoster, "pagination-poster")
	for _, tc := range []struct {
		name   string
		filter MediaVisibility
		total  int64
	}{
		{name: "ordinary", total: 2},
		{name: "missing poster", filter: MediaVisibility{MissingPoster: true}, total: 1},
		{name: "missing Chinese title", filter: MediaVisibility{MissingChineseTitle: true}, total: 2},
		{name: "combined", filter: MediaVisibility{MissingPoster: true, MissingChineseTitle: true}, total: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads.seriesSQL = ""
			cards, total, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", tc.filter)
			if err != nil || total != tc.total || len(cards) != 1 {
				t.Fatalf("cards=%+v total=%d err=%v", cards, total, err)
			}
			filtered := tc.filter.MissingPoster || tc.filter.MissingChineseTitle
			if reads.seriesSQL == "" {
				t.Fatal("missing series page query")
			}
			if filtered {
				cards, total, err = web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, cards[0].Rep.SeriesID, "", tc.filter)
				if err != nil || total != 1 || len(cards) != 1 {
					t.Fatalf("explicit series cards=%+v total=%d err=%v", cards, total, err)
				}
			}
		})
	}
	filtered, n, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", MediaVisibility{MissingPoster: true, MissingChineseTitle: true})
	if err != nil || n != 1 || len(filtered) != 1 || filtered[0].Rep.SeriesID == first.Rep.SeriesID {
		t.Fatalf("filtered=%+v total=%d err=%v", filtered, n, err)
	}
	empty, n, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", MediaVisibility{LibraryRestricted: true})
	if err != nil || n != 0 || len(empty) != 0 {
		t.Fatal("restricted empty scope leaked")
	}

	for _, parent := range []string{lib.ID, first.Rep.SeriesID} {
		reads.fileRows = 0
		result, err := emby.Items(t.Context(), ItemsParams{ParentID: parent, Limit: 1, Fields: []string{"Overview"}})
		if err != nil {
			t.Fatal(err)
		}
		items := result["Items"].([]map[string]any)
		if len(items) != 1 || reads.fileRows != 0 {
			t.Fatalf("parent=%s items=%d loaded=%d", parent, len(items), reads.fileRows)
		}
		if parent == lib.ID {
			if items[0]["RecursiveItemCount"] != episodeCount || items[0]["ChildCount"] != 1 {
				t.Fatalf("series counts=%+v", items[0])
			}
		} else if items[0]["ChildCount"] != episodeCount {
			t.Fatalf("season count=%+v", items[0])
		}
	}
	reads.fileRows = 0
	result, err := emby.Items(t.Context(), ItemsParams{ParentID: first.Rep.SeriesID + "-season", Limit: 1, StartIndex: 1, Fields: []string{"Overview"}})
	if err != nil {
		t.Fatal(err)
	}
	items := result["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Id"] != first.Rep.SeriesID+"-e2" || result["TotalRecordCount"] != episodeCount || reads.fileRows > 9 {
		t.Fatalf("episode page=%+v loaded=%d", result, reads.fileRows)
	}
	// 摘要列表不能污染后续完整详情。
	detail, err := emby.Item(t.Context(), first.Rep.SeriesID, "")
	if err != nil || detail["RecursiveItemCount"] != episodeCount {
		t.Fatalf("series detail=%+v err=%v", detail, err)
	}

	// 电影库混合列表仍先按作品统一分页，第一页只包含整剧时不读取文件详情。
	if err := db.Model(&lib).Update("type", "movie").Error; err != nil {
		t.Fatal(err)
	}
	movie := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "电影", ReleaseDate: "2020-01-01"})
	for v := 0; v < 3; v++ {
		if err := db.Create(&model.Media{LibraryID: lib.ID, MetadataID: movie.ID, Path: fmt.Sprintf("/fixture/pagination/movie-v%d.mkv", v)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	reads.fileRows = 0
	result, err = emby.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 1, Fields: []string{"Overview"}})
	if err != nil || result["TotalRecordCount"] != 3 || len(result["Items"].([]map[string]any)) != 1 || reads.fileRows != 0 {
		t.Fatalf("mixed=%+v files=%d err=%v", result, reads.fileRows, err)
	}
	result, err = emby.Items(t.Context(), ItemsParams{ParentID: lib.ID, StartIndex: 2, Limit: 1, Fields: []string{"Overview"}})
	if err != nil || result["Items"].([]map[string]any)[0]["Id"] != movie.ID {
		t.Fatalf("mixed last=%+v err=%v", result, err)
	}
	reads.fileRows = 0
	movies, count, err := web.ListMediaVisibleGrouped(t.Context(), lib.ID, 1, 1, visibility)
	if err != nil || count != 1 || len(movies) != 1 || movies[0].VersionCount != 3 || reads.fileRows != 1 || len(movies[0].Versions) != 0 {
		t.Fatalf("movie cards=%+v total=%d files=%d err=%v", movies, count, reads.fileRows, err)
	}
	for part := 1; part <= 2; part++ {
		if err := db.Create(&model.Media{LibraryID: lib.ID, MetadataID: movie.ID, Path: fmt.Sprintf("/fixture/pagination/movie-part%d.mkv", part), PartGroupKey: "pagination-parts", PartIndex: part}).Error; err != nil {
			t.Fatal(err)
		}
	}
	movies, count, err = web.ListMediaVisibleGrouped(t.Context(), lib.ID, 1, 1, visibility)
	if err != nil || count != 1 || len(movies) != 1 || movies[0].VersionCount != 4 || movies[0].PartIndex == 2 {
		t.Fatalf("multipart cards=%+v total=%d err=%v", movies, count, err)
	}
	wantMovie := movies[0]
	reads.moviePageQueries = 0
	movies, count, err = web.ListMediaVisibleGrouped(t.Context(), lib.ID, 1, 1, MediaVisibility{MissingChineseTitle: true})
	if err != nil || count != 0 || len(movies) != 0 || reads.moviePageQueries != 0 {
		t.Fatalf("Chinese movie passed missing-title filter: count=%d err=%v", count, err)
	}
	if err := db.Model(&model.MetadataItem{}).Where("id = ?", movie.ID).Update("title", "Movie").Error; err != nil {
		t.Fatal(err)
	}
	for _, filter := range []MediaVisibility{
		{MissingPoster: true}, {MissingChineseTitle: true}, {MissingPoster: true, MissingChineseTitle: true},
	} {
		filter.AllowedLibraryIDs, filter.HiddenLibraryIDs = visibility.AllowedLibraryIDs, visibility.HiddenLibraryIDs
		movies, count, err = web.ListMediaVisibleGrouped(t.Context(), lib.ID, 1, 1, filter)
		if err != nil || count != 1 || len(movies) != 1 || movies[0].ID != wantMovie.ID || movies[0].VersionCount != 4 {
			t.Fatalf("filtered multipart cards=%+v total=%d err=%v", movies, count, err)
		}
		filter.HiddenLibraryIDs = []string{lib.ID}
		movies, count, err = web.ListMediaVisibleGrouped(t.Context(), lib.ID, 1, 1, filter)
		if err != nil || count != 0 || len(movies) != 0 {
			t.Fatalf("hidden filtered movies=%+v total=%d err=%v", movies, count, err)
		}
	}
	reads.fileRows = 0
	movies, count, err = web.ListMediaVisibleGrouped(t.Context(), lib.ID, 2, 1, visibility)
	if err != nil || count != 1 || len(movies) != 0 || reads.fileRows != 0 {
		t.Fatalf("empty page=%+v total=%d reads=%d err=%v", movies, count, reads.fileRows, err)
	}
	if err := db.Model(&model.MetadataItem{}).Where("id = ?", first.Rep.SeriesID).Update("nsfw", true).Error; err != nil {
		t.Fatal(err)
	}
	cards, total, err = web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", visibility)
	if err != nil || total != 1 || len(cards) != 1 || cards[0].Rep.SeriesID == first.Rep.SeriesID {
		t.Fatalf("hidden series=%+v total=%d err=%v", cards, total, err)
	}
	for _, filter := range []MediaVisibility{{MissingPoster: true}, {MissingChineseTitle: true}, {MissingPoster: true, MissingChineseTitle: true}} {
		filter.AllowedLibraryIDs = visibility.AllowedLibraryIDs
		cards, total, err = web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", filter)
		if err != nil || total != 1 || len(cards) != 1 || cards[0].Rep.SeriesID == first.Rep.SeriesID {
			t.Fatalf("filtered NSFW series=%+v total=%d err=%v", cards, total, err)
		}
		filter.HiddenLibraryIDs = []string{lib.ID}
		cards, total, err = web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, "", "", filter)
		if err != nil || total != 0 || len(cards) != 0 {
			t.Fatalf("hidden filtered series=%+v total=%d err=%v", cards, total, err)
		}
	}
}
