package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newMediaMetadataTMDbProvider(t *testing.T, handler http.HandlerFunc) *TMDbProvider {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	return NewTMDbProvider(cfg, zap.NewNop(), nil)
}

func TestUpdateMediaMetadataMarksManualMatch(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "自采集", Path: "/media/custom", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{PermanentBase: model.PermanentBase{ID: "custom-media"}, LibraryID: lib.ID, Title: "raw", Path: "/media/custom/raw.mp4", ScrapeStatus: "no_match"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	tmdb := newMediaMetadataTMDbProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/12345" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "title": "手动标题", "future_field": true})
	})
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos).SetTMDbProvider(tmdb)
	title := "手动标题"
	overview := "手动简介"
	releaseDate := "2026-06-23"
	tmdbID := 12345
	nsfw := true
	updated, err := svc.UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{
		Title:       &title,
		Overview:    &overview,
		ReleaseDate: &releaseDate,
		TMDbID:      &tmdbID,
		NSFW:        &nsfw,
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Title != title || updated.Overview != overview || updated.ScrapeStatus != "matched" {
		t.Fatalf("metadata not saved: %#v", updated)
	}
	if updated.TMDbID != tmdbID || !updated.NSFW {
		t.Fatalf("ids/episode metadata not saved: %#v", updated)
	}
	if updated.ReleaseDate != releaseDate {
		t.Fatalf("release date = %q, want %q", updated.ReleaseDate, releaseDate)
	}
	if snapshot, findErr := repos.Metadata.FindProviderSnapshot(t.Context(), updated.MetadataID, "tmdb"); findErr != nil || snapshot == nil || snapshot.Payload == "" {
		t.Fatalf("TMDB snapshot = %#v, err = %v", snapshot, findErr)
	}
}

func TestUpdateEpisodeMetadataDoesNotModifyParentIdentityOrArtwork(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "Series", Path: "/media/series", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		PermanentBase: model.PermanentBase{ID: "episode-media"}, LibraryID: lib.ID, Title: "Show", EpisodeTitle: "Episode 1",
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
	season, err := repos.Metadata.FindByID(t.Context(), seasonID)
	if err != nil || season == nil || season.ParentID == nil {
		t.Fatalf("season metadata = %#v, err = %v", season, err)
	}
	if err := repos.Metadata.ReplaceIdentifier(t.Context(), *season.ParentID, "tmdb", model.MetadataKindSeries, "500"); err != nil {
		t.Fatal(err)
	}
	createServiceTestArtwork(t, db, episode.ID, model.ArtworkTypePoster, "episode-poster")
	createServiceTestArtwork(t, db, seasonID, model.ArtworkTypePoster, "season-poster")

	title := "Own episode title"
	tmdbID := 9876
	tmdb := newMediaMetadataTMDbProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/500/season/1/episode/1" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 9876, "name": "Own episode title", "future_field": true})
	})
	updated, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).SetTMDbProvider(tmdb).UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{
		Title: &title, TMDbID: &tmdbID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || updated.TMDbID != tmdbID || updated.PosterURL == "" {
		t.Fatalf("episode metadata not updated on the episode: %#v", updated)
	}
	if item, findErr := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindEpisode, "9876"); findErr != nil || item == nil || item.ID != episode.ID {
		t.Fatalf("episode identifier = %#v, err = %v", item, findErr)
	}
	if item, findErr := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeason, "9876"); findErr != nil || item != nil {
		t.Fatalf("parent received episode identifier: %#v, err = %v", item, findErr)
	}
	if asset, findErr := repos.Artwork.FindSelection(t.Context(), episode.ID, model.ArtworkTypePoster); findErr != nil || asset == nil || asset.ID != "episode-poster" {
		t.Fatalf("episode poster = %#v, err = %v", asset, findErr)
	}
	if asset, findErr := repos.Artwork.FindSelection(t.Context(), seasonID, model.ArtworkTypePoster); findErr != nil || asset == nil || asset.ID != "season-poster" {
		t.Fatalf("parent poster changed: %#v, err = %v", asset, findErr)
	}
	if snapshot, findErr := repos.Metadata.FindProviderSnapshot(t.Context(), episode.ID, "tmdb"); findErr != nil || snapshot == nil {
		t.Fatalf("episode TMDB snapshot = %#v, err = %v", snapshot, findErr)
	}
}

func TestUpdateMediaMetadataRejectsTMDbIDWhenDetailsFail(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "Original", Path: "/media/movies/original.mkv", TMDbID: 111}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), media.MetadataID, "tmdb", []byte(`{"id":111,"kept":true}`), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	tmdb := newMediaMetadataTMDbProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	})
	newID := 222
	if _, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).SetTMDbProvider(tmdb).UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{TMDbID: &newID}); err == nil {
		t.Fatal("invalid TMDB details must reject metadata update")
	}
	if old, _ := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "111"); old == nil || old.ID != media.MetadataID {
		t.Fatalf("old TMDB identifier changed: %#v", old)
	}
	if next, _ := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "222"); next != nil {
		t.Fatalf("failed TMDB identifier persisted: %#v", next)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), media.MetadataID, "tmdb")
	var payload map[string]any
	if err != nil || snapshot == nil || json.Unmarshal([]byte(snapshot.Payload), &payload) != nil || payload["id"] != float64(111) || payload["kept"] != true {
		t.Fatalf("old TMDB snapshot changed: %#v, err = %v", snapshot, err)
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
