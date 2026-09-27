package service

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// 保留旧候选批次边界的多版本回归规模。
const latestMediaCandidateBatchSize = 128

func TestEmbyLatestItemsFilterPlayedBeforeLimitAndByUser(t *testing.T) {
	svc := newTestEmbyService(t)
	svc.SetRuntimeCache(NewRuntimeCacheService(&config.Config{}, zap.NewNop()))
	lib := model.Library{Name: "电影", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for i, id := range []string{"newest", "next", "oldest"} {
		metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "metadata-" + id}, Kind: model.MetadataKindMovie,
			Title: id, Source: "local",
		})
		if err := svc.repo.DB.Create(&model.Media{
			PermanentBase: model.PermanentBase{ID: "media-" + id, CreatedAt: now.Add(-time.Duration(i) * time.Hour)},
			LibraryID:     lib.ID, MetadataID: metadata.ID, Title: id, Path: "/media/movies/" + id + ".mkv",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.repo.DB.Create(&[]model.PlaybackHistory{
		{UserID: "user-2", MetadataID: "metadata-next", MediaID: "media-next", Completed: true, WatchedAt: now},
		{UserID: "user-1", MetadataID: "metadata-oldest", MediaID: "media-oldest", Completed: false, WatchedAt: now},
	}).Error; err != nil {
		t.Fatal(err)
	}

	items, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 1, false)
	if err != nil || len(items) != 1 || items[0]["Id"] != "metadata-newest" {
		t.Fatalf("initial unplayed latest = %#v, err=%v", items, err)
	}
	if err := svc.repo.DB.Create(&model.PlaybackHistory{
		UserID: "user-1", MetadataID: "metadata-newest", MediaID: "media-newest", Completed: true, WatchedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	items, err = svc.LatestItems(t.Context(), "user-1", lib.ID, 1, false)
	if err != nil || len(items) != 1 || items[0]["Id"] != "metadata-next" {
		t.Fatalf("unplayed latest after completion = %#v, err=%v", items, err)
	}
	items, err = svc.LatestItems(t.Context(), "user-1", lib.ID, 10, true)
	if err != nil || len(items) != 1 || items[0]["Id"] != "metadata-newest" {
		t.Fatalf("user-1 played latest = %#v, err=%v", items, err)
	}
	items, err = svc.LatestItems(t.Context(), "user-2", lib.ID, 10, true)
	if err != nil || len(items) != 1 || items[0]["Id"] != "metadata-next" {
		t.Fatalf("user-2 played latest = %#v, err=%v", items, err)
	}
	assertLatestFieldsAndQueries(t, svc, lib.ID)
	// 另一库新增同一作品的文件只改变全局排序时间，不改变本库可见版本。
	shared := model.Media{PermanentBase: model.PermanentBase{ID: "other-version", CreatedAt: now.Add(time.Hour)}, LibraryID: "other-library", MetadataID: "metadata-oldest", Path: "/other/oldest.mkv"}
	if err := svc.repo.DB.Create(&shared).Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"metadata-oldest", "metadata-next"} {
		items, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 1, false)
		if err != nil || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("cross-library Latest want=%s items=%v err=%v", want, items, err)
		}
		if err := svc.repo.DB.Delete(&shared).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestEmbyLatestSeriesFiltersEpisodesBeforeGrouping(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "剧集", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for i, state := range []string{"played", "unplayed"} {
		series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "series-" + state}, Kind: model.MetadataKindSeries,
			Title: state, Source: "local",
		})
		season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "season-" + state}, Kind: model.MetadataKindSeason,
			ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "local",
		})
		episode := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "episode-" + state}, Kind: model.MetadataKindEpisode,
			ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Source: "local",
		})
		if err := svc.repo.DB.Create(&model.Media{
			PermanentBase: model.PermanentBase{ID: "media-" + state, CreatedAt: now.Add(-time.Duration(i) * time.Hour)},
			LibraryID:     lib.ID, MetadataID: episode.ID, Title: series.Title,
			Path: fmt.Sprintf("/media/tv/%s/S01E01.mkv", state), SeasonNum: 1, EpisodeNum: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.repo.DB.Create(&model.PlaybackHistory{
		UserID: "user-1", MetadataID: "episode-played", MediaID: "media-played", Completed: true, WatchedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	unplayed, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 10, false)
	if err != nil || len(unplayed) != 1 || unplayed[0]["Id"] != "series-unplayed" {
		t.Fatalf("unplayed series = %#v, err=%v", unplayed, err)
	}
	played, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 10, true)
	if err != nil || len(played) != 1 || played[0]["Id"] != "series-played" {
		t.Fatalf("played series = %#v, err=%v", played, err)
	}
	assertLatestFieldsAndQueries(t, svc, lib.ID)
}

func TestEmbyLatestSeriesKeepsPartiallyPlayedWorkInBothLists(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "剧集", Path: "/fixture/partial", Type: "tv"}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "local"})
	for n := 1; n <= 2; n++ {
		season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: n, Title: "Season", Source: "local"})
		ep := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "local"})
		for v := 1; v <= 2; v++ {
			file := model.Media{LibraryID: lib.ID, MetadataID: ep.ID, SeasonNum: n, EpisodeNum: 1, Path: fmt.Sprintf("/fixture/partial/%d-%d.mkv", n, v)}
			if err := svc.repo.DB.Create(&file).Error; err != nil {
				t.Fatal(err)
			}
			if n == 1 && v == 1 {
				if err := svc.repo.DB.Create(&model.PlaybackHistory{UserID: "viewer", MetadataID: ep.ID, MediaID: file.ID, Completed: true}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, played := range []bool{false, true} {
		items, err := svc.LatestItems(t.Context(), "viewer", lib.ID, 1, played)
		if err != nil || len(items) != 1 || items[0]["Id"] != series.ID || items[0]["RecursiveItemCount"] != 1 || items[0]["ChildCount"] != 1 {
			t.Fatalf("played=%v items=%v err=%v", played, items, err)
		}
		assertEmbyUnplayedCount(t, items[0], 1)
	}
}

func TestEmbyLatestSeriesItemsContinueThroughCandidateTimeTie(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "剧集", Path: "/media/latest-tv", Type: "tv", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	createEpisode := func(seriesID string) string {
		series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: seriesID}, Kind: model.MetadataKindSeries, Title: seriesID, Source: "local",
		})
		season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: seriesID + "-season"}, Kind: model.MetadataKindSeason,
			ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "local",
		})
		episode := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: seriesID + "-episode"}, Kind: model.MetadataKindEpisode,
			ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Source: "local",
		})
		return episode.ID
	}
	newestEpisode := createEpisode("series-newest")
	aEpisode := createEpisode("series-a")
	zEpisode := createEpisode("series-z")
	now := time.Now()
	media := make([]model.Media, 0, latestMediaCandidateBatchSize+1)
	for i := 0; i < latestMediaCandidateBatchSize-1; i++ {
		media = append(media, model.Media{
			PermanentBase: model.PermanentBase{ID: fmt.Sprintf("newest-%03d", i), CreatedAt: now.Add(time.Duration(i+1) * time.Second)},
			LibraryID:     lib.ID, MetadataID: newestEpisode, Title: "newest", Path: fmt.Sprintf("/media/latest-tv/newest-%03d.mkv", i),
			SeasonNum: 1, EpisodeNum: 1,
		})
	}
	media = append(media,
		model.Media{PermanentBase: model.PermanentBase{ID: "zzz-a", CreatedAt: now}, LibraryID: lib.ID, MetadataID: aEpisode, Title: "a", Path: "/media/latest-tv/a.mkv", SeasonNum: 1, EpisodeNum: 1},
		model.Media{PermanentBase: model.PermanentBase{ID: "aaa-z", CreatedAt: now}, LibraryID: lib.ID, MetadataID: zEpisode, Title: "z", Path: "/media/latest-tv/z.mkv", SeasonNum: 1, EpisodeNum: 1},
	)
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	items, err := svc.LatestItems(t.Context(), "", lib.ID, 2, false)
	if err != nil || len(items) != 2 || items[0]["Id"] != "series-newest" || items[1]["Id"] != "series-z" {
		t.Fatalf("latest series = %#v, err=%v", items, err)
	}
	shared := model.Media{PermanentBase: model.PermanentBase{ID: "other-a", CreatedAt: now.Add(time.Hour)}, LibraryID: "other-library", MetadataID: aEpisode, Path: "/other/a.mkv", SeasonNum: 1, EpisodeNum: 1}
	if err := svc.repo.DB.Create(&shared).Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"series-a", "series-newest"} {
		items, err := svc.LatestItems(t.Context(), "", lib.ID, 1, false)
		if err != nil || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("cross-library series Latest want=%s items=%v err=%v", want, items, err)
		}
		if err := svc.repo.DB.Delete(&shared).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 大量更近的作品属于其它库时，仍须返回当前库完整一页。
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'outside-'||n,'series','Outside','local' FROM generate_series(1,?) n`,
		`INSERT INTO media(id,path,library_id,metadata_id,created_at) SELECT 'outside-file-'||n,'/other/'||n,'other-library','outside-'||n,TIMESTAMP '2099-01-01' FROM generate_series(1,?) n`,
	} {
		if err := svc.repo.DB.Exec(sql, 513).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, partial := range []bool{false, true} {
		if partial {
			if err := svc.repo.DB.Exec("UPDATE media SET created_at=TIMESTAMP '2100-01-01' WHERE metadata_id=?", newestEpisode).Error; err != nil {
				t.Fatal(err)
			}
		}
		items, err = svc.LatestItems(t.Context(), "", lib.ID, 2, false)
		if err != nil || len(items) != 2 || items[0]["Id"] != "series-newest" || items[1]["Id"] != "series-z" {
			t.Fatalf("library candidates partial=%v series=%v err=%v", partial, items, err)
		}
	}
	if err := svc.repo.DB.Exec("UPDATE metadata_items SET library_ids=NULL WHERE kind='series'").Error; err != nil {
		t.Fatal(err)
	}
	unknown, err := svc.LatestItems(t.Context(), "", lib.ID, 2, false)
	if err != nil || !reflect.DeepEqual(unknown, items) {
		t.Fatalf("unknown membership changed latest: got=%v want=%v err=%v", unknown, items, err)
	}
}

func assertLatestFieldsAndQueries(t *testing.T, svc *EmbyService, libraryID string) {
	t.Helper()
	queries := 0
	countQuery := func(db *gorm.DB) {
		queries++
		sql := db.Statement.SQL.String()
		if strings.Contains(sql, "scoped_metadata") || strings.Contains(sql, "scoped_series") {
			t.Errorf("Latest executed an unused total count: %s", sql)
		}
	}
	if err := svc.repo.DB.Callback().Query().After("gorm:query").Register("test:latest-queries", countQuery); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DB.Callback().Row().After("gorm:row").Register("test:latest-rows", countQuery); err != nil {
		t.Fatal(err)
	}
	full, err := svc.LatestItems(t.Context(), "user-1", libraryID, 1, false)
	if err != nil || len(full) != 1 {
		t.Fatalf("default latest = %#v, err=%v", full, err)
	}
	fullQueries := queries
	queries = 0
	minimal, err := svc.LatestItems(t.Context(), "user-1", libraryID, 1, false, "PrimaryImageAspectRatio", "Path")
	if err != nil || len(minimal) != 1 || minimal[0]["Id"] != full[0]["Id"] {
		t.Fatalf("minimal latest = %#v, err=%v", minimal, err)
	}
	if queries >= fullQueries {
		t.Fatalf("minimal queries=%d, full=%d; expected fewer queries", queries, fullQueries)
	}
	t.Logf("%s relation queries: default=%d, minimal=%d", full[0]["Type"], fullQueries, queries)
	for _, key := range []string{"People", "ProviderIds", "MediaSources"} {
		if _, ok := minimal[0][key]; ok {
			t.Errorf("minimal latest contains %s", key)
		}
	}
	for _, key := range []string{"People", "ProviderIds"} {
		if _, ok := full[0][key]; !ok {
			t.Errorf("default latest missing %s", key)
		}
	}
	selected, err := svc.LatestItems(t.Context(), "user-1", libraryID, 1, false, "People")
	if err != nil || len(selected) != 1 {
		t.Fatalf("selected latest = %#v, err=%v", selected, err)
	}
	if _, ok := selected[0]["People"]; !ok {
		t.Error("explicitly requested People missing")
	}
	if _, ok := selected[0]["ProviderIds"]; ok {
		t.Error("unrequested ProviderIds present")
	}
}
