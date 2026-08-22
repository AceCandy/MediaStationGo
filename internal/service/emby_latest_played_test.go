package service

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

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
			Base: model.Base{ID: "metadata-" + id}, Kind: model.MetadataKindMovie,
			Title: id, Source: "local",
		})
		if err := svc.repo.DB.Create(&model.Media{
			Base:      model.Base{ID: "media-" + id, CreatedAt: now.Add(-time.Duration(i) * time.Hour)},
			LibraryID: lib.ID, MetadataID: metadata.ID, Title: id, Path: "/media/movies/" + id + ".mkv",
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
			Base: model.Base{ID: "series-" + state}, Kind: model.MetadataKindSeries,
			Title: state, Source: "local",
		})
		season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			Base: model.Base{ID: "season-" + state}, Kind: model.MetadataKindSeason,
			ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "local",
		})
		episode := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			Base: model.Base{ID: "episode-" + state}, Kind: model.MetadataKindEpisode,
			ParentID: &season.ID, SeasonNum: 1, EpisodeNum: 1, Title: "Episode 1", Source: "local",
		})
		if err := svc.repo.DB.Create(&model.Media{
			Base:      model.Base{ID: "media-" + state, CreatedAt: now.Add(-time.Duration(i) * time.Hour)},
			LibraryID: lib.ID, MetadataID: episode.ID, Title: series.Title,
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
}
