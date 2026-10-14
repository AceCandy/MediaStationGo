package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbEpisodeRecheckUpdatesReleaseDateAndDetectsCandidates(t *testing.T) {
	item := &model.MetadataItem{Title: "Episode 1", Overview: "Old", ReleaseDate: "2025-01-01", Rating: 1, Year: 2025}
	episode := &TMDbEpisodeDetails{Name: "Pilot", Overview: "New", AirDate: "2026-08-26", AirYear: 2026, Rating: 8}
	updates, _ := tmdbEpisodeMetadataUpdates(nil, episode, 0)
	applyTMDbMetadataUpdates(item, updates)
	if item.Title != "Pilot" || item.Overview != "New" || item.ReleaseDate != "2026-08-26" || item.Year != 2026 || item.Rating != 8 {
		t.Fatalf("updated episode = %#v", item)
	}
	for _, kind := range []string{model.MetadataKindSeason, model.MetadataKindEpisode} {
		for _, title := range []string{"", "第 1 季", "特别篇", "Season 1", "第 1 集", "Episode 1", "Pilot"} {
			candidate := repository.TMDbMetadataRecheckCandidate{Kind: kind, Title: title, Overview: "Overview", ReleaseDate: "2026-08-26", TMDbID: "42"}
			if tmdbMetadataCandidateNeedsRecheck(candidate) {
				t.Fatalf("title alone triggered recheck: %s %q", kind, title)
			}
			for _, field := range []string{"overview", "date", "artwork", "identifier", "snapshot"} {
				gap := candidate
				switch field {
				case "overview":
					gap.Overview = ""
				case "date":
					gap.ReleaseDate = ""
				case "artwork":
					gap.ArtworkMissing = true
				case "identifier":
					gap.TMDbID = ""
				case "snapshot":
					gap.SnapshotMissing = true
				}
				want := kind == model.MetadataKindSeason || (field != "identifier" && field != "snapshot")
				if tmdbMetadataCandidateNeedsRecheck(gap) != want {
					t.Fatalf("%s %s gap selected incorrectly", kind, field)
				}
			}
		}
	}
}

func TestFetchTMDbMetadataRecheckUsesSeasonCoordinates(t *testing.T) {
	var mode atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/42/season/1" {
			t.Errorf("unexpected TMDb request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if mode.Load() == 3 {
			_, _ = io.WriteString(w, `{`)
			return
		}
		seasonNumber := 1
		if mode.Load() == 1 {
			seasonNumber++
		}
		id := 201
		if mode.Load() == 2 {
			id = 0
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "season_number": seasonNumber})
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
	s := &ScraperService{tmdb: NewTMDbProvider(cfg, zap.NewNop(), nil)}
	for _, oldID := range []string{"999", "not-a-number"} {
		candidate := repository.TMDbMetadataRecheckCandidate{Kind: model.MetadataKindSeason, SeasonNum: 1, TMDbID: oldID}
		details, err := s.fetchTMDbMetadataRecheck(t.Context(), candidate, 42)
		if err != nil || details == nil || details.id != 201 {
			t.Fatalf("old season TMDb ID %q blocked coordinate lookup: details=%#v err=%v", oldID, details, err)
		}
	}
	mode.Store(1)
	candidate := repository.TMDbMetadataRecheckCandidate{Kind: model.MetadataKindSeason, SeasonNum: 1, TMDbID: "201"}
	if _, err := s.fetchTMDbMetadataRecheck(t.Context(), candidate, 42); !errors.Is(err, ErrTMDbRefreshIdentity) {
		t.Fatalf("mismatched season number error = %v", err)
	}
	candidate.SeriesTMDbID = "42"
	for _, invalidMode := range []int32{2, 3} {
		mode.Store(invalidMode)
		details, err := s.fetchTMDbMetadataRecheck(t.Context(), candidate, 42)
		if err == nil && details != nil && details.id > 0 && json.Valid(details.rawJSON) {
			t.Fatalf("invalid upstream response mode %d reached persistence: details=%v err=%v", invalidMode, details, err)
		}
	}
}
