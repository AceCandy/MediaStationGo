package service

import (
	"errors"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestEmbyProgressClampsProbeDuration(t *testing.T) {
	for _, source := range []string{"legacy", "nfo", "hongguo"} {
		t.Run(source, func(t *testing.T) {
			db := newServiceTestDB(t)
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE UNIQUE INDEX test_history_identity ON playback_histories(user_id,metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
				t.Fatal(err)
			}
			create := func(value any) {
				t.Helper()
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			create(&model.Library{Base: model.Base{ID: "library"}, Name: "Test", Path: "/test", Type: "movie"})
			file := model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: "library", Path: "/test/movie.mkv"}
			itemID := "movie"
			switch source {
			case "legacy":
				create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: "movie"}, Kind: "movie", Title: "Movie", Source: "test"})
				file.MetadataID = "movie"
			case "nfo":
				create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: "movie"}, LibraryID: "library", LocalKey: "movie", Kind: "movie", NFOFields: model.NFOFields{Title: "Movie"}})
				file.CatalogSource, itemID = source, "nfo-movie"
			case "hongguo":
				create(&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "movie"}, SourceID: "123", Kind: "movie", Title: "Movie"})
				file.CatalogSource, file.LookupCatalogID, itemID = source, "123", "hg-work-movie"
			}
			create(&file)
			if source == "nfo" {
				create(&model.NFOMediaBinding{MediaID: file.ID, ItemID: "movie", NFOFields: model.NFOFields{Title: "Movie"}})
			}
			if source == "hongguo" {
				create(&model.HongGuoMediaBinding{MediaID: file.ID, WorkID: "movie"})
			}
			const duration = int64(120_000)
			create(&model.MediaProbeMetadata{MediaID: file.ID, DurationMS: duration})
			e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
			e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
			stateTable := map[string]string{"legacy": "playback_histories", "nfo": "nfo_user_states", "hongguo": "hongguo_user_states"}[source]
			eventTable := map[string]string{"legacy": "playback_events", "nfo": "nfo_playback_events", "hongguo": "hongguo_playback_events"}[source]
			for _, sourceID := range []string{file.ID, ""} {
				if err := e.RecordProgress(t.Context(), "viewer", itemID, sourceID, "stopped", (duration+14)*10_000, 0); err != nil {
					t.Fatalf("source ID %q: %v", sourceID, err)
				}
			}
			var state struct {
				PositionMs, DurationMs int64
				Completed              bool
				WatchedAt              time.Time
			}
			if err := db.Table(stateTable).Where("user_id = ?", "viewer").Take(&state).Error; err != nil {
				t.Fatal(err)
			}
			if state.PositionMs != duration || state.DurationMs != duration || !state.Completed {
				t.Fatalf("state = %+v", state)
			}
			for _, tc := range []struct{ position, runtime int64 }{{-1, 0}, {duration + 14, duration}} {
				if err := e.RecordProgress(t.Context(), "viewer", itemID, file.ID, "invalid", tc.position*10_000, tc.runtime*10_000); !errors.Is(err, ErrInvalidPlaybackProgress) {
					t.Fatalf("invalid bounds %+v: %v", tc, err)
				}
				after := state
				if err := db.Table(stateTable).Where("user_id = ?", "viewer").Take(&after).Error; err != nil || after != state {
					t.Fatalf("invalid report changed state: before=%+v after=%+v err=%v", state, after, err)
				}
			}
			var events int64
			if err := db.Table(eventTable).Count(&events).Error; err != nil || events != 1 {
				t.Fatalf("events = %d, err = %v", events, err)
			}
		})
	}
}
