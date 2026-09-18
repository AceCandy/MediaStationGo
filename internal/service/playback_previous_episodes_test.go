package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestPlaybackAutoMarkPreviousEpisodes(t *testing.T) {
	for _, client := range []string{"web", "emby"} {
		t.Run(client, func(t *testing.T) {
			e := newTestEmbyService(t)
			db, ctx := e.repo.DB, t.Context()
			if err := db.AutoMigrate(&model.PlaybackEvent{}); err != nil {
				t.Fatal(err)
			}
			for _, sql := range []string{
				`CREATE UNIQUE INDEX test_history_identity ON playback_histories (user_id, metadata_id) WHERE deleted_at IS NULL`,
				`CREATE UNIQUE INDEX test_event_identity ON playback_events (user_id, session_id, metadata_id) WHERE deleted_at IS NULL`,
			} {
				if err := db.Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
			}
			visibility := MediaVisibility{AllowedLibraryIDs: []string{"visible", "hidden"}, HiddenLibraryIDs: []string{"hidden"}}
			e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: visibility, expiresAt: time.Now().Add(time.Hour)}}
			series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "local"})
			season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 0, Source: "local"})
			media := make([]model.Media, 10)
			for n := 1; n <= 9; n++ {
				ep := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: n, NSFW: n == 3, Source: "local"})
				media[n] = model.Media{MetadataID: ep.ID, LibraryID: "visible", Path: fmt.Sprintf("/test/S00E%02d.mkv", n)}
				if n == 4 {
					media[n].LibraryID = "hidden"
				}
				if n == 5 {
					media[n].LibraryID = "forbidden"
				}
				if n != 6 {
					if err := db.Create(&media[n]).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			otherSeason := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Source: "local"})
			other := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &otherSeason.ID, EpisodeNum: 1, Source: "local"})
			if err := db.Create(&model.Media{MetadataID: other.ID, LibraryID: "visible", Path: "/test/S01E01.mkv"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.Media{MetadataID: media[1].MetadataID, LibraryID: "visible", Path: "/test/S00E01-v2.mkv"}).Error; err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-time.Hour).Truncate(time.Second)
			for _, row := range []*model.PlaybackHistory{
				{UserID: "viewer", MetadataID: media[1].MetadataID, MediaID: media[1].ID, PositionMs: 30_000, DurationMs: 120_000, WatchedAt: old},
				{UserID: "viewer", MetadataID: media[2].MetadataID, MediaID: media[2].ID, PositionMs: 100_000, DurationMs: 120_000, Completed: true, WatchedAt: old},
				{UserID: "other", MetadataID: media[1].MetadataID, MediaID: media[1].ID, PositionMs: 25_000, DurationMs: 120_000, WatchedAt: old},
			} {
				if err := e.repo.History.Upsert(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
			playback := NewPlaybackService(e.log, e.repo)
			record := func(position int64, session string) error {
				if client == "emby" {
					return e.RecordProgress(ctx, "viewer", media[8].MetadataID, media[8].ID, session, position*10_000, 120_000*10_000)
				}
				return playback.RecordProgress(ctx, "viewer", media[8].ID, session, position, 120_000, visibility)
			}
			checkFirst := func(completed bool) {
				t.Helper()
				var row model.PlaybackHistory
				if err := db.Where("user_id = ? AND metadata_id = ?", "viewer", media[1].MetadataID).First(&row).Error; err != nil {
					t.Fatal(err)
				}
				if row.Completed != completed || (completed && row.PositionMs != 120_000) {
					t.Fatalf("unexpected first episode: %#v", row)
				}
			}
			if err := record(100_000, ""); err != nil {
				t.Fatal(err)
			}
			checkFirst(false) // 缺失开关时默认关闭。
			for _, value := range []string{"false", "invalid", "true"} {
				if err := e.repo.Setting.Set(ctx, autoMarkPreviousEpisodesSetting, value); err != nil {
					t.Fatal(err)
				}
				position := int64(100_000)
				if value == "true" {
					position = 40_000
				}
				if err := record(position, ""); err != nil {
					t.Fatal(err)
				}
				checkFirst(false)
			}
			if err := e.MarkPlayed(ctx, "viewer", media[8].MetadataID, true); err != nil {
				t.Fatal(err)
			}
			checkFirst(false)
			// 事件失败必须撤销当前集完成与所有补标。
			if err := db.Callback().Create().Before("gorm:create").Register("test:fail-auto-event", func(tx *gorm.DB) {
				if tx.Statement.Table == "playback_events" {
					tx.AddError(errors.New("fixture event failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := record(100_000, "failed"); err == nil {
				t.Fatal("expected rollback")
			}
			if err := db.Callback().Create().Remove("test:fail-auto-event"); err != nil {
				t.Fatal(err)
			}
			checkFirst(false)
			for range 2 {
				if err := record(100_000, "session"); err != nil {
					t.Fatal(err)
				}
			}
			checkFirst(true)
			var rows []model.PlaybackHistory
			if err := db.Order("user_id, metadata_id").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 5 {
				t.Fatalf("marked invisible, missing, later or other-season episode: %#v", rows)
			}
			for _, row := range rows {
				if row.UserID == "other" && (row.Completed || row.PositionMs != 25_000) {
					t.Fatal("changed other user")
				}
				if row.UserID == "viewer" && row.MetadataID == media[2].MetadataID && (!row.WatchedAt.Equal(old) || row.PositionMs != 100_000) {
					t.Fatal("overwrote completed history")
				}
			}
			var events int64
			if err := db.Model(&model.PlaybackEvent{}).Count(&events).Error; err != nil || events != 1 {
				t.Fatalf("events=%d err=%v", events, err)
			}
			// 无会话 ID 的兼容回报也必须补标，但不增加真实播放事件。
			if err := db.Where("user_id = ? AND metadata_id = ?", "viewer", media[7].MetadataID).Delete(&model.PlaybackHistory{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := record(100_000, ""); err != nil {
				t.Fatal(err)
			}
			var restored model.PlaybackHistory
			if err := db.Where("user_id = ? AND metadata_id = ?", "viewer", media[7].MetadataID).First(&restored).Error; err != nil || !restored.Completed {
				t.Fatalf("missing compatibility completion: %#v %v", restored, err)
			}
		})
	}
}

func TestHongGuoAutoMarkPreviousEpisodes(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	ctx := t.Context()
	for _, id := range []string{"visible", "hidden"} {
		if err := db.Create(&model.Library{Base: model.Base{ID: id}, Name: id, Path: "/test/" + id, Type: model.LibraryTypeHongGuo}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Setting.Set(ctx, autoMarkPreviousEpisodesSetting, "true"); err != nil {
		t.Fatal(err)
	}
	work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "900000000000000031", Title: "Show", EpisodeCount: 5, TotalEpisodes: 5, Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	media := make([]model.Media, 5)
	for n := 1; n <= 4; n++ {
		media[n] = model.Media{LibraryID: "visible", Path: fmt.Sprintf("/test/hg/E%03d.strm", n), CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: work.SourceID, SeasonNum: 1, EpisodeNum: n}
		if n == 3 {
			media[n].LibraryID = "hidden"
		}
		if err := repos.Media.Upsert(ctx, &media[n]); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := db.Create(&model.HongGuoUserState{UserID: "viewer", SourceID: work.SourceID, EpisodeNumber: 2, MediaID: media[2].ID, Completed: true, WatchedAt: &old}).Error; err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	visibility := MediaVisibility{AllowedLibraryIDs: []string{"visible", "hidden"}, HiddenLibraryIDs: []string{"hidden"}}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: visibility, expiresAt: time.Now().Add(time.Hour)}}
	view := serviceTestMediaView(t, repos, media[4].ID)
	for range 2 {
		if err := e.RecordProgress(ctx, "viewer", view.CatalogItemID, media[4].ID, "session", 100_000*10_000, 120_000*10_000); err != nil {
			t.Fatal(err)
		}
	}
	var rows []model.HongGuoUserState
	if err := db.Order("episode_number").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].EpisodeNumber != 1 || !rows[0].Completed || rows[1].WatchedAt == nil || !rows[1].WatchedAt.Equal(old) || rows[2].EpisodeNumber != 4 {
		t.Fatalf("unexpected source states: %#v", rows)
	}
	var count int64
	if err := db.Model(&model.PlaybackHistory{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("canonical histories=%d err=%v", count, err)
	}
	if err := db.Model(&model.HongGuoPlaybackEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("source events=%d err=%v", count, err)
	}
}
