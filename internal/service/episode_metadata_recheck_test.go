package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbEpisodeRecheckUpdatesReleaseDateAndDetectsCandidates(t *testing.T) {
	item := &model.MetadataItem{Title: "Episode 1", Overview: "Old", ReleaseDate: "2025-01-01", Rating: 1, Year: 2025}
	episode := &TMDbEpisodeDetails{Name: "Pilot", Overview: "New", AirDate: "2026-08-26", AirYear: 2026, Rating: 8}
	updates, _ := tmdbEpisodeMetadataUpdates(nil, episode, 0)
	changed := changedTMDbEpisodeFields(item, updates)
	applyTMDbEpisodeMetadataUpdates(item, updates)
	if item.Title != "Pilot" || item.Overview != "New" || item.ReleaseDate != "2026-08-26" || item.Year != 2026 || item.Rating != 8 {
		t.Fatalf("updated episode = %#v", item)
	}
	if len(changed) != 5 {
		t.Fatalf("changed fields = %#v", changed)
	}
	if !tmdbEpisodeCandidateNeedsRecheck(repository.TMDbEpisodeMetadataRecheckCandidate{Title: "第 1 集", Overview: "Overview", ReleaseDate: "2026-08-26"}) {
		t.Fatal("generated title was not selected for recheck")
	}
	if tmdbEpisodeCandidateNeedsRecheck(repository.TMDbEpisodeMetadataRecheckCandidate{Title: "Pilot", Overview: "Overview", ReleaseDate: "2026-08-26"}) {
		t.Fatal("complete episode was selected for recheck")
	}
}
