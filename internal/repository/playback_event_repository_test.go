package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestPlaybackEventStatsDetailsAndSeasonRanking(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Library{}, &model.MetadataItem{}, &model.Media{},
		&model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.PlaybackEvent{},
	); err != nil {
		t.Fatal(err)
	}

	user := model.User{Username: "viewer", PasswordHash: "hash", Role: "user", Nickname: "观众"}
	movieLibrary := model.Library{Name: "电影库", Path: "/movies", Type: "movie", Enabled: true}
	tvLibrary := model.Library{Name: "剧集库", Path: "/tv", Type: "tv", Enabled: true}
	for _, row := range []any{&user, &movieLibrary, &tvLibrary} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	movie := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "电影", Source: "test"}
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "剧名", Source: "test"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 2, Title: "第二季", Source: "test"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode1 := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "第一集", Source: "test"}
	episode2 := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "第二集", Source: "test"}
	if err := db.Create(&[]*model.MetadataItem{&episode1, &episode2}).Error; err != nil {
		t.Fatal(err)
	}

	media := []model.Media{
		{LibraryID: movieLibrary.ID, MetadataID: movie.ID, Title: "电影 A", Path: "/movies/a.mkv"},
		{LibraryID: movieLibrary.ID, MetadataID: movie.ID, Title: "电影 B", Path: "/movies/b.mkv"},
		{LibraryID: tvLibrary.ID, MetadataID: episode1.ID, Title: "第一集", Path: "/tv/s02e01.mkv"},
		{LibraryID: tvLibrary.ID, MetadataID: episode2.ID, Title: "第二集", Path: "/tv/s02e02.mkv"},
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)
	events := []model.PlaybackEvent{
		{UserID: user.ID, SessionID: "movie-a", MetadataID: movie.ID, MediaID: media[0].ID, LibraryID: movieLibrary.ID, PlayedAt: base},
		{UserID: user.ID, SessionID: "movie-b", MetadataID: movie.ID, MediaID: media[1].ID, LibraryID: movieLibrary.ID, PlayedAt: base.Add(time.Minute)},
		{UserID: user.ID, SessionID: "episode-1-a", MetadataID: episode1.ID, MediaID: media[2].ID, LibraryID: tvLibrary.ID, PlayedAt: base.Add(2 * time.Minute)},
		{UserID: user.ID, SessionID: "episode-1-b", MetadataID: episode1.ID, MediaID: media[2].ID, LibraryID: tvLibrary.ID, PlayedAt: base.Add(3 * time.Minute)},
		{UserID: user.ID, SessionID: "episode-2", MetadataID: episode2.ID, MediaID: media[3].ID, LibraryID: tvLibrary.ID, PlayedAt: base.Add(4 * time.Minute)},
		{UserID: user.ID, SessionID: "outside-rank", MetadataID: movie.ID, MediaID: media[1].ID, LibraryID: movieLibrary.ID, PlayedAt: base.Add(30 * time.Minute)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&media[0]).Error; err != nil {
		t.Fatal(err)
	}

	filter := PlaybackStatsFilter{
		Grain: "day", From: base.Add(-time.Hour), To: base.Add(time.Hour), TimeZone: "UTC",
		Page: 1, PageSize: 2, RankGrain: "day", RankPeriod: "2026-08-21",
		RankFrom: base.Add(-time.Hour), RankTo: base.Add(10 * time.Minute),
	}
	result, err := New(db).PlaybackEvent.Stats(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 6 || result.Details.Total != 6 || len(result.Details.Items) != 2 {
		t.Fatalf("unexpected totals/details: %#v", result)
	}
	if result.Details.Items[0].Title != "电影" || result.Details.Items[1].Title != "第二集" {
		t.Fatalf("details are not newest first: %#v", result.Details.Items)
	}
	if len(result.Ranking.Items) != 2 || result.Ranking.Items[0].GroupID != season.ID || result.Ranking.Items[0].Count != 3 {
		t.Fatalf("season ranking = %#v", result.Ranking.Items)
	}
	if result.Ranking.Items[0].Title != series.Title || result.Ranking.Items[0].SeasonNum != 2 {
		t.Fatalf("season display = %#v", result.Ranking.Items[0])
	}

	filter.MediaType = "movie"
	filter.PageSize = 20
	movieResult, err := New(db).PlaybackEvent.Stats(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if movieResult.Total != 3 || len(movieResult.Ranking.Items) != 1 || movieResult.Ranking.Items[0].Count != 2 {
		t.Fatalf("movie versions should share one rank item: %#v", movieResult)
	}
	available := map[string]bool{}
	for _, item := range movieResult.Details.Items {
		available[item.MediaID] = item.MediaAvailable
	}
	if available[media[0].ID] || !available[media[1].ID] {
		t.Fatalf("deleted media availability = %#v", available)
	}

	filter.Page = 99
	beyondLastPage, err := New(db).PlaybackEvent.Stats(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(beyondLastPage.Details.Items) != 0 || beyondLastPage.Details.Total != 3 {
		t.Fatalf("page beyond last = %#v", beyondLastPage.Details)
	}
}
