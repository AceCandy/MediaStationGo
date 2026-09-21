package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestPlaybackStateReplayAndDeletedVersion(t *testing.T) {
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
			file := model.Media{PermanentBase: model.PermanentBase{ID: "current-file"}, LibraryID: "library", Path: "/test/current.mkv"}
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
			create(&model.MediaProbeMetadata{MediaID: file.ID, DurationMS: 1_440_000})
			repos := repository.New(db)
			e := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
			p := NewPlaybackService(zap.NewNop(), repos)
			visibility := MediaVisibility{IncludeNSFW: true}
			e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: visibility, expiresAt: time.Now().Add(time.Hour)}}
			assertState := func(played bool, position int64, resumes int) {
				t.Helper()
				item, err := e.Item(t.Context(), itemID, "viewer")
				if err != nil {
					t.Fatal(err)
				}
				data := item["UserData"].(map[string]any)
				if data["Played"] != played || data["PlaybackPositionTicks"] != position*10_000 {
					t.Fatalf("state=%v want played=%v position=%d", data, played, position)
				}
				web, err := p.ContinueHistory(t.Context(), "viewer", 20, visibility)
				if err != nil || len(web) != resumes {
					t.Fatalf("web resumes=%v err=%v", web, err)
				}
				resume, err := e.ResumeItems(t.Context(), "viewer", 20)
				if err != nil || reflect.ValueOf(resume["Items"]).Len() != resumes {
					t.Fatalf("emby resumes=%v err=%v", resume, err)
				}
			}
			if err := e.MarkPlayed(t.Context(), "viewer", itemID, true); err != nil {
				t.Fatal(err)
			}
			assertState(true, 0, 0)
			if err := p.RecordProgress(t.Context(), "viewer", file.ID, "replay", 120_000, 1_440_000, visibility); err != nil {
				t.Fatal(err)
			}
			assertState(true, 120_000, 1)
			if err := p.RecordProgress(t.Context(), "viewer", file.ID, "replay", 1_440_000, 3_900_000, visibility); err != nil {
				t.Fatal(err)
			}
			assertState(true, 0, 0)
			if err := e.MarkPlayed(t.Context(), "viewer", itemID, false); err != nil {
				t.Fatal(err)
			}
			assertState(false, 0, 0)
			// 旧版本已被删除，只剩 24 分钟版本；读取不能改写历史快照。
			now := time.Now()
			switch source {
			case "legacy":
				create(&model.PlaybackHistory{UserID: "viewer", MetadataID: "movie", MediaID: "removed", PositionMs: 1_440_000, DurationMs: 3_900_000, WatchedAt: now})
			case "nfo":
				if err := db.Model(&model.NFOUserState{}).Where("user_id = ?", "viewer").Updates(map[string]any{"media_id": "removed", "position_ms": 1_440_000, "duration_ms": 3_900_000, "completed": false, "watched_at": now}).Error; err != nil {
					t.Fatal(err)
				}
			case "hongguo":
				create(&model.HongGuoUserState{UserID: "viewer", SourceID: "123", EpisodeNumber: 1, MediaID: "removed", PositionMs: 1_440_000, DurationMs: 3_900_000, WatchedAt: &now})
			}
			assertState(true, 0, 0)
			stateTable := map[string]string{"legacy": "playback_histories", "nfo": "nfo_user_states", "hongguo": "hongguo_user_states"}[source]
			var snapshot struct {
				MediaID                string
				PositionMs, DurationMs int64
				Completed              bool
			}
			if err := db.Table(stateTable).Where("user_id = ?", "viewer").Where("media_id = ?", "removed").Take(&snapshot).Error; err != nil {
				t.Fatal(err)
			}
			if snapshot.Completed || snapshot.PositionMs != 1_440_000 || snapshot.DurationMs != 3_900_000 {
				t.Fatalf("read changed snapshot: %+v", snapshot)
			}
			checkProjection := func(user string, filter repository.MediaQueryFilter, wantPlayed bool, wantRows int) {
				t.Helper()
				var states []struct{ Completed bool }
				if err := repository.PlaybackStates(t.Context(), db, source, user, filter).Scan(&states).Error; err != nil {
					t.Fatal(err)
				}
				if len(states) != wantRows || (len(states) > 0 && states[0].Completed != wantPlayed) {
					t.Fatalf("projection user=%s states=%+v", user, states)
				}
			}
			checkProjection("another-user", repository.MediaQueryFilter{IncludeNSFW: true}, false, 0)
			checkProjection("viewer", repository.MediaQueryFilter{IncludeNSFW: true, HiddenLibraryIDs: []string{"library"}}, false, 1)
			if err := db.Model(&model.MediaProbeMetadata{}).Where("media_id = ?", file.ID).Update("duration_ms", 0).Error; err != nil {
				t.Fatal(err)
			}
			checkProjection("viewer", repository.MediaQueryFilter{IncludeNSFW: true}, false, 1)
			eventTable := map[string]string{"legacy": "playback_events", "nfo": "nfo_playback_events", "hongguo": "hongguo_playback_events"}[source]
			var events int64
			if err := db.Table(eventTable).Where("user_id = ?", "viewer").Count(&events).Error; err != nil {
				t.Fatal(err)
			}
			if events != 1 {
				t.Fatalf("manual marks or reads created events: %d", events)
			}
		})
	}
}
