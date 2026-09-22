package service

import (
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestPlaybackProgressRecordsOneEventPerSession(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.PlaybackHistory{}, &model.PlaybackEvent{})
	for _, sql := range []string{
		`CREATE UNIQUE INDEX test_history_identity ON playback_histories(user_id,metadata_id) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX test_event_identity ON playback_events(user_id,session_id,metadata_id) WHERE deleted_at IS NULL`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	media := model.Media{LibraryID: "library-1", MetadataID: metadata.ID, Title: "Movie", Path: "/media/movie.mkv", DurationSec: 120}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	playback := NewPlaybackService(zap.NewNop(), repos)
	visibility := MediaVisibility{IncludeNSFW: true}

	if err := playback.RecordProgress(t.Context(), "user-1", media.ID, "session-1", 20_000, 120_000, visibility); err != nil {
		t.Fatal(err)
	}
	if err := playback.RecordProgress(t.Context(), "user-1", media.ID, "session-1", 30_000, 120_000, visibility); err != nil {
		t.Fatal(err)
	}
	if err := playback.RecordProgress(t.Context(), "user-1", media.ID, "session-2", 30_000, 120_000, visibility); err != nil {
		t.Fatal(err)
	}
	if err := playback.RecordProgress(t.Context(), "user-1", media.ID, "session-3", 19_999, 120_000, visibility); err != nil {
		t.Fatal(err)
	}

	var eventCount int64
	if err := db.Model(&model.PlaybackEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("event count = %d, want 2", eventCount)
	}
	var history model.PlaybackHistory
	if err := db.Where("user_id = ? AND metadata_id = ?", "user-1", metadata.ID).First(&history).Error; err != nil {
		t.Fatal(err)
	}
	if history.PositionMs != 30_000 {
		t.Fatalf("history position = %d, want 30000", history.PositionMs)
	}
}
