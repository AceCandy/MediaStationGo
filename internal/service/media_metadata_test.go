package service

import (
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestUpdateMediaMetadataMarksManualMatch(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	lib := model.Library{Name: "自采集", Path: "/media/custom", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{Base: model.Base{ID: "custom-media"}, LibraryID: lib.ID, Title: "raw", Path: "/media/custom/raw.mp4", ScrapeStatus: "no_match"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	title := "手动标题"
	overview := "手动简介"
	releaseDate := "2026-06-23"
	season := 0
	episode := 1
	tmdbID := 12345
	nsfw := true
	updated, err := svc.UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{
		Title:       &title,
		Overview:    &overview,
		ReleaseDate: &releaseDate,
		SeasonNum:   &season,
		EpisodeNum:  &episode,
		TMDbID:      &tmdbID,
		NSFW:        &nsfw,
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Title != title || updated.Overview != overview || updated.ScrapeStatus != "matched" {
		t.Fatalf("metadata not saved: %#v", updated)
	}
	if updated.SeasonNum != 0 || updated.EpisodeNum != 1 || updated.TMDbID != tmdbID || !updated.NSFW {
		t.Fatalf("ids/episode metadata not saved: %#v", updated)
	}
	if updated.ReleaseDate != releaseDate {
		t.Fatalf("release date = %q, want %q", updated.ReleaseDate, releaseDate)
	}
}

func TestUpdateEpisodeMetadataDoesNotModifyParentIdentityOrArtwork(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	lib := model.Library{Name: "Series", Path: "/media/series", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		Base: model.Base{ID: "episode-media"}, LibraryID: lib.ID, Title: "Show", EpisodeTitle: "Episode 1",
		Path: "/media/series/show-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1,
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	episode, err := repos.Metadata.FindByID(t.Context(), media.MetadataID)
	if err != nil || episode == nil || episode.ParentID == nil {
		t.Fatalf("episode metadata = %#v, err = %v", episode, err)
	}
	seasonID := *episode.ParentID
	createServiceTestArtwork(t, db, episode.ID, model.ArtworkTypePoster, "episode-poster")
	createServiceTestArtwork(t, db, seasonID, model.ArtworkTypePoster, "season-poster")

	title := "Own episode title"
	tmdbID := 9876
	emptyArtwork := ""
	updated, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{
		Title: &title, TMDbID: &tmdbID, PosterURL: &emptyArtwork,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || updated.TMDbID != tmdbID || updated.PosterURL != "" {
		t.Fatalf("episode metadata not updated on the episode: %#v", updated)
	}
	if item, findErr := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindEpisode, "9876"); findErr != nil || item == nil || item.ID != episode.ID {
		t.Fatalf("episode identifier = %#v, err = %v", item, findErr)
	}
	if item, findErr := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeason, "9876"); findErr != nil || item != nil {
		t.Fatalf("parent received episode identifier: %#v, err = %v", item, findErr)
	}
	if asset, findErr := repos.Artwork.FindSelection(t.Context(), episode.ID, model.ArtworkTypePoster); findErr != nil || asset != nil {
		t.Fatalf("episode poster = %#v, err = %v", asset, findErr)
	}
	if asset, findErr := repos.Artwork.FindSelection(t.Context(), seasonID, model.ArtworkTypePoster); findErr != nil || asset == nil || asset.ID != "season-poster" {
		t.Fatalf("parent poster changed: %#v, err = %v", asset, findErr)
	}
}

func TestManualEpisodeParentDoesNotCreateParentFromEpisodeMetadata(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{})
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repository.New(db))
	episode := &model.MetadataItem{
		Kind: model.MetadataKindEpisode, Title: "Episode title", Overview: "Episode overview",
		Year: 2026, Genres: "Episode genre", Source: "manual",
	}

	if parent, err := svc.manualEpisodeParent(t.Context(), episode); err == nil || parent != nil {
		t.Fatalf("parent = %#v, err = %v", parent, err)
	}
	var seriesCount int64
	if err := db.Model(&model.MetadataItem{}).Where("kind = ?", model.MetadataKindSeries).Count(&seriesCount).Error; err != nil {
		t.Fatal(err)
	}
	if seriesCount != 0 {
		t.Fatalf("created %d series rows from episode metadata", seriesCount)
	}
}
