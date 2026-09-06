package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/logger"
)

type paginationReadLog struct {
	logger.Interface
	fileRows int64
}

func (l *paginationReadLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.Contains(sql, "view_title") || strings.Contains(sql, `"media"."path"`) {
		if rows > 0 {
			l.fileRows += rows
		}
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
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
}
