package service

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestListFavouritesUsesMetadataIdentity(t *testing.T) {
	db := newServiceTestDB(t, &model.User{}, &model.Library{}, &model.Media{}, &model.Favorite{})
	repos := repository.New(db)
	user := model.User{Username: "favorite-user"}
	lib := model.Library{Name: "Movies", Path: "/movies", Type: "movie", Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Metadata Movie", Source: "test"})
	media := model.Media{LibraryID: lib.ID, MetadataID: metadata.ID, Path: "/movies/metadata-movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Favorite{UserID: user.ID, MetadataID: metadata.ID, MediaID: "removed-media"}).Error; err != nil {
		t.Fatal(err)
	}

	items, err := NewPlaybackService(zap.NewNop(), repos).ListFavourites(t.Context(), user.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != media.ID {
		t.Fatalf("favorites = %#v, want visible version %q", items, media.ID)
	}
}

func TestListFavouritesChoosesVisibleSeriesRepresentative(t *testing.T) {
	db := newServiceTestDB(t, &model.User{}, &model.Library{}, &model.Media{}, &model.Favorite{})
	repos := repository.New(db)
	user := model.User{Username: "favorite-visible-user"}
	lib := model.Library{Name: "Shows", Path: "/shows", Type: "tv", Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "test"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: series.Title, Source: "test"})
	visibleEpisode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Visible", Source: "test"})
	hiddenEpisode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "Hidden", Source: "test", NSFW: true})
	visibleMedia := model.Media{PermanentBase: model.PermanentBase{CreatedAt: time.Now().Add(-time.Minute)}, LibraryID: lib.ID, MetadataID: visibleEpisode.ID, Path: "/shows/series/s01e01.mkv"}
	hiddenMedia := model.Media{PermanentBase: model.PermanentBase{CreatedAt: time.Now()}, LibraryID: lib.ID, MetadataID: hiddenEpisode.ID, Path: "/shows/series/s01e02.mkv"}
	if err := db.Create(&[]model.Media{visibleMedia, hiddenMedia}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Favorite{UserID: user.ID, MetadataID: series.ID}).Error; err != nil {
		t.Fatal(err)
	}

	items, err := NewPlaybackService(zap.NewNop(), repos).ListFavourites(t.Context(), user.ID, MediaVisibility{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MetadataID != visibleEpisode.ID {
		t.Fatalf("favorites = %#v, want visible representative %q", items, visibleEpisode.ID)
	}
}

func TestListFavouritesSupportsSeriesAndLegacyEpisodeMetadata(t *testing.T) {
	db := newServiceTestDB(t, &model.User{}, &model.Library{}, &model.Media{}, &model.Favorite{})
	repos := repository.New(db)
	user := model.User{Username: "favorite-series-user"}
	lib := model.Library{Name: "Shows", Path: "/shows", Type: "tv", Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "test"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: series.Title, Source: "test"})
	episode1 := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Source: "test"})
	episode2 := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "Episode 2", Source: "test"})
	for _, media := range []model.Media{
		{LibraryID: lib.ID, MetadataID: episode1.ID, Path: "/shows/series/s01e01.mkv", SeasonNum: 1, EpisodeNum: 1},
		{LibraryID: lib.ID, MetadataID: episode2.ID, Path: "/shows/series/s01e02.mkv", SeasonNum: 1, EpisodeNum: 2},
	} {
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&[]model.Favorite{
		{UserID: user.ID, MetadataID: series.ID},
		{UserID: user.ID, MetadataID: episode2.ID},
	}).Error; err != nil {
		t.Fatal(err)
	}

	items, err := NewPlaybackService(zap.NewNop(), repos).ListFavourites(t.Context(), user.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].SeriesID != series.ID || items[1].MetadataID != episode2.ID {
		t.Fatalf("favorites = %#v, want series and legacy episode identities", items)
	}
}
