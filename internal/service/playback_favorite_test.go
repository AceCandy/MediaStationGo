package service

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestListFavouritesUsesMetadataIdentity(t *testing.T) {
	db := newServiceTestDB(t, &model.User{}, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.Favorite{})
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
	db := newServiceTestDB(t, &model.User{}, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.Favorite{})
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
	if len(items) != 1 || items[0].MetadataID != series.ID || items[0].Path != visibleMedia.Path {
		t.Fatalf("favorites = %#v, want series identity with visible representative", items)
	}
}

func TestListFavouritesShowsSeriesMetadata(t *testing.T) {
	db := newServiceTestDB(t, &model.User{}, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.Favorite{})
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
	poster := createServiceTestArtwork(t, db, series.ID, model.ArtworkTypePoster, "favorite-series-poster")
	createServiceTestArtwork(t, db, episode2.ID, model.ArtworkTypeStill, "favorite-episode-still")
	for _, media := range []model.Media{
		{LibraryID: lib.ID, MetadataID: episode1.ID, Path: "/shows/series/s01e01.mkv", SeasonNum: 1, EpisodeNum: 1},
		{LibraryID: lib.ID, MetadataID: episode2.ID, Path: "/shows/series/s01e02.mkv", SeasonNum: 1, EpisodeNum: 2},
	} {
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repos.Favorite.SetByIdentity(t.Context(), user.ID, series.ID, "", true); err != nil {
		t.Fatal(err)
	}

	items, err := NewPlaybackService(zap.NewNop(), repos).ListFavourites(t.Context(), user.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MetadataID != series.ID || items[0].SeriesID != series.ID || items[0].MetadataKind != model.MetadataKindSeries || items[0].Title != series.Title || items[0].PosterURL != poster {
		t.Fatalf("favorites = %#v, want series identity, title and poster", items)
	}
	if items[0].EpisodeNum != 0 || items[0].SeasonNum != 0 || items[0].SeasonID != "" || items[0].LibraryID != lib.ID || items[0].ID == series.ID {
		t.Fatalf("series card must keep a file/library for navigation without episode coordinates: %#v", items[0])
	}
	if err := db.Model(series).Update("nsfw", true).Error; err != nil {
		t.Fatal(err)
	}
	items, err = NewPlaybackService(zap.NewNop(), repos).ListFavourites(t.Context(), user.ID, MediaVisibility{})
	if err != nil || len(items) != 0 {
		t.Fatalf("hidden series favorites = %#v, err = %v", items, err)
	}
}

func TestFavoritesOnlyMoviesAndSeries(t *testing.T) {
	emby := newTestEmbyService(t)
	repos := emby.repo
	db := repos.DB
	user := model.User{Username: "favorite-kinds-user", Role: "admin", IsActive: true}
	lib := model.Library{Name: "Shows", Path: "/shows", Type: "tv", Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	movie := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "test"})
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "test"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season", Source: "test"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "test"})
	movieMedia := model.Media{LibraryID: lib.ID, MetadataID: movie.ID, Path: "/shows/movie.mkv"}
	episodeMedia := model.Media{LibraryID: lib.ID, MetadataID: episode.ID, Path: "/shows/s01e01.mkv", SeasonNum: 1, EpisodeNum: 1}
	for _, media := range []*model.Media{&movieMedia, &episodeMedia} {
		if err := db.Create(media).Error; err != nil {
			t.Fatal(err)
		}
	}
	playback := NewPlaybackService(zap.NewNop(), repos)
	for _, metadata := range []*model.MetadataItem{movie, series} {
		for _, favorite := range []bool{true, true, false, true, false} {
			if err := emby.SetFavorite(t.Context(), user.ID, metadata.ID, favorite); err != nil {
				t.Fatalf("set %s favorite=%v: %v", metadata.Kind, favorite, err)
			}
			item, err := emby.Item(t.Context(), metadata.ID, user.ID)
			if err != nil || item == nil || item["UserData"].(map[string]any)["IsFavorite"] != favorite {
				t.Fatalf("%s favorite readback = %#v, err = %v", metadata.Kind, item, err)
			}
		}
	}
	if favorite, err := playback.ToggleFavourite(t.Context(), user.ID, movieMedia.ID); err != nil || !favorite {
		t.Fatalf("movie toggle = %v, %v", favorite, err)
	}
	for _, metadata := range []*model.MetadataItem{season, episode} {
		for _, favorite := range []bool{true, false} {
			if _, err := repos.Favorite.SetByIdentity(t.Context(), user.ID, metadata.ID, episodeMedia.ID, favorite); !errors.Is(err, repository.ErrFavoriteUnsupportedType) {
				t.Fatalf("repository %s favorite error = %v", metadata.Kind, err)
			}
			if err := emby.SetFavorite(t.Context(), user.ID, metadata.ID, favorite); !errors.Is(err, repository.ErrFavoriteUnsupportedType) {
				t.Fatalf("Emby %s favorite error = %v", metadata.Kind, err)
			}
		}
	}
	if _, err := playback.ToggleFavourite(t.Context(), user.ID, episodeMedia.ID); !errors.Is(err, repository.ErrFavoriteUnsupportedType) {
		t.Fatalf("episode toggle error = %v", err)
	}
	if _, err := playback.SetFavourite(t.Context(), user.ID, episodeMedia.ID, true); !errors.Is(err, repository.ErrFavoriteUnsupportedType) {
		t.Fatalf("episode set error = %v", err)
	}
	rows, err := repos.Favorite.ListByUser(t.Context(), user.ID)
	if err != nil || len(rows) != 1 || rows[0].MetadataID != movie.ID {
		t.Fatalf("unsupported requests changed favorites: %#v, %v", rows, err)
	}
}
