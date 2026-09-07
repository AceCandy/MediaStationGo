package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbySeriesAndSeasonPlayedState(t *testing.T) {
	svc := newTestEmbyService(t)
	svc.SetRuntimeCache(NewRuntimeCacheService(nil, svc.log))
	db := svc.repo.DB
	if err := db.Exec(`CREATE UNIQUE INDEX test_history_identity ON playback_histories (user_id, metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Shows", Path: "/fixture/shows", Type: "tv"}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "local"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "local"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Source: "local"})
	// 没有探测时长也必须保留手动标记的完成状态。
	media := model.Media{MetadataID: episode.ID, LibraryID: lib.ID, Path: "/fixture/shows/S01E01.mkv", SeasonNum: 1, EpisodeNum: 1}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kind, id, parent string }{
		{"Series", series.ID, lib.ID},
		{"Season", season.ID, series.ID},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			check := func(userID string, played bool) {
				t.Helper()
				item, err := svc.Item(t.Context(), tc.id, userID)
				if err != nil || item == nil {
					t.Fatalf("detail: %v, %#v", err, item)
				}
				page, err := svc.Items(t.Context(), ItemsParams{UserID: userID, ParentID: tc.parent, IncludeItemTypes: []string{tc.kind}, Limit: 10})
				if err != nil {
					t.Fatal(err)
				}
				items := page["Items"].([]map[string]any)
				if len(items) != 1 || items[0]["Id"] != tc.id {
					t.Fatalf("list: %#v", page)
				}
				for _, payload := range []map[string]any{item, items[0]} {
					data := payload["UserData"].(map[string]any)
					if data["Played"] != played {
						t.Fatalf("user=%s played=%v: %#v", userID, played, data)
					}
					if played && (data["PlayCount"] != 1 || data["PlayedPercentage"] != float64(100)) {
						t.Fatalf("inconsistent completed UserData: %#v", data)
					}
				}
			}
			check("viewer", false)
			for _, played := range []bool{true, false, true, false} {
				cacheKey := svc.embyItemsCacheKey("items", ItemsParams{UserID: "viewer"})
				svc.cache.SetJSON(t.Context(), cacheKey, embyItemsCacheValue{}, time.Minute)
				if err := svc.MarkPlayed(t.Context(), "viewer", tc.id, played); err != nil {
					t.Fatal(err)
				}
				var cached embyItemsCacheValue
				if svc.cache.GetJSON(t.Context(), cacheKey, &cached) {
					t.Fatal("manual played state retained stale item cache")
				}
				check("viewer", played)
				check("other-viewer", false)
				child, err := svc.Item(t.Context(), episode.ID, "viewer")
				if err != nil || child["UserData"].(map[string]any)["Played"] != played {
					t.Fatalf("episode did not follow parent: %#v, %v", child, err)
				}
			}
		})
	}
}

func TestEmbyPlayedHierarchyScopeAndRollback(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	if err := db.AutoMigrate(&model.PlaybackEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX test_history_identity ON playback_histories (user_id, metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Shows", Path: "/fixture/shows", Type: "tv"}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	svc.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{AllowedLibraryIDs: []string{lib.ID}}, expiresAt: time.Now().Add(time.Hour)}}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "local"})
	var seasons, episodes []string
	for s := 0; s < 2; s++ {
		season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: s, Title: fmt.Sprint("Season ", s), Source: "local"})
		seasons = append(seasons, season.ID)
		for n := 1; n <= 2; n++ {
			episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: n, Title: "Episode", Source: "local"})
			episodes = append(episodes, episode.ID)
			// 两个文件版本只对应一条作品历史。
			for v := 0; v < 2; v++ {
				if err := db.Create(&model.Media{MetadataID: episode.ID, LibraryID: lib.ID, Path: fmt.Sprintf("/fixture/%d/%d-%d.mkv", s, n, v), SeasonNum: s, EpisodeNum: n}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	missing := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &seasons[1], EpisodeNum: 3, Title: "Missing", Source: "local"})
	event := model.PlaybackEvent{UserID: "viewer", MetadataID: episodes[0], SessionID: "fixture-session", PlayedAt: time.Now()}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	hidden := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &seasons[1], EpisodeNum: 4, Title: "Hidden", Source: "local"})
	if err := db.Create(&model.Media{MetadataID: hidden.ID, LibraryID: "hidden-library", Path: "/fixture/hidden.mkv", SeasonNum: 1, EpisodeNum: 4}).Error; err != nil {
		t.Fatal(err)
	}
	for _, history := range []model.PlaybackHistory{
		{UserID: "viewer", MetadataID: series.ID, Completed: true}, // 旧的父级记录不能覆盖子集。
		{UserID: "viewer", MetadataID: hidden.ID, Completed: true},
		{UserID: "other", MetadataID: episodes[0], Completed: true},
	} {
		if err := db.Create(&history).Error; err != nil {
			t.Fatal(err)
		}
	}
	check := func(id string, played bool) {
		t.Helper()
		item, err := svc.Item(t.Context(), id, "viewer")
		if err != nil || item == nil || item["UserData"].(map[string]any)["Played"] != played {
			t.Fatalf("item %s played=%v: %#v, %v", id, played, item, err)
		}
		parent, kind := lib.ID, "Series"
		for _, seasonID := range seasons {
			if id == seasonID {
				parent, kind = series.ID, "Season"
			}
		}
		for i, episodeID := range episodes {
			if id == episodeID {
				parent, kind = seasons[i/2], "Episode"
			}
		}
		if id == missing.ID {
			parent, kind = seasons[1], "Episode"
		}
		page, err := svc.Items(t.Context(), ItemsParams{UserID: "viewer", ParentID: parent, IncludeItemTypes: []string{kind}, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		items := page["Items"].([]map[string]any)
		for _, item := range items {
			if item["Id"] == id {
				if item["UserData"].(map[string]any)["Played"] != played {
					t.Fatalf("list %s played=%v: %#v", id, played, page)
				}
				return
			}
		}
		t.Fatalf("list omitted %s", id)
	}
	mark := func(id string, played bool) {
		t.Helper()
		if err := svc.MarkPlayed(t.Context(), "viewer", id, played); err != nil {
			t.Fatal(err)
		}
	}
	check(series.ID, false)
	mark(seasons[0], true)
	check(seasons[0], true)
	check(seasons[1], false)
	check(series.ID, false)
	for i, id := range episodes {
		check(id, i < 2)
	}
	mark(series.ID, true)
	for _, id := range append(append([]string{series.ID}, seasons...), episodes...) {
		check(id, true)
	}
	var count int64
	if err := db.Model(&model.PlaybackHistory{}).Where("user_id = ? AND metadata_id IN ?", "viewer", episodes).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("multi-version history count=%d: %v", count, err)
	}
	if err := db.Model(&model.PlaybackHistory{}).Where("user_id = ? AND metadata_id = ?", "viewer", missing.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("missing episode was marked: %d, %v", count, err)
	}
	// 标记后新入库的集仍未观看，父级随之恢复为未全部观看。
	if err := db.Create(&model.Media{MetadataID: missing.ID, LibraryID: lib.ID, Path: "/fixture/new.mkv", SeasonNum: 1, EpisodeNum: 3}).Error; err != nil {
		t.Fatal(err)
	}
	check(missing.ID, false)
	check(seasons[1], false)
	check(series.ID, false)
	mark(missing.ID, true)
	check(series.ID, true)
	mark(episodes[2], false)
	check(seasons[0], true)
	check(seasons[1], false)
	check(series.ID, false)
	mark(series.ID, false)
	for _, id := range episodes {
		check(id, false)
	}
	if err := db.Model(&model.PlaybackHistory{}).Where("user_id = ? AND metadata_id = ?", "viewer", missing.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("missing episode was marked: %d, %v", count, err)
	}
	if err := db.Model(&model.PlaybackHistory{}).Where("(user_id = ? AND metadata_id = ?) OR (user_id = ? AND metadata_id = ?)", "viewer", hidden.ID, "other", episodes[0]).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("hidden/other-user history changed: %d, %v", count, err)
	}
	if err := db.Model(&model.PlaybackEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("manual marks changed playback events: %d, %v", count, err)
	}
	// 跨越批量写入边界，在第二批失败时第一批也必须回滚。
	if err := db.Exec(`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num, episode_num)
		SELECT 'bulk-episode-' || n, 'episode', ?, 'Episode', 'local', 0, n
		FROM generate_series(10,210) n`, seasons[1]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, metadata_id, library_id, path, season_num, episode_num)
		SELECT 'bulk-file-' || n, 'bulk-episode-' || n, ?, '/fixture/bulk/' || n || '.mkv', 1, n
		FROM generate_series(10,210) n`, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	inserts := 0
	if err := db.Callback().Create().After("gorm:create").Register("test:fail-history", func(tx *gorm.DB) {
		if tx.Statement.Table == "playback_histories" {
			inserts++
			if inserts == 2 {
				tx.AddError(errors.New("history write failed"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("test:fail-history") })
	if err := svc.MarkPlayed(t.Context(), "viewer", series.ID, true); err == nil {
		t.Fatal("expected history write error")
	}
	if inserts != 2 {
		t.Fatalf("expected two history batches, got %d", inserts)
	}
	if err := db.Model(&model.PlaybackHistory{}).Where("user_id = ? AND metadata_id NOT IN ?", "viewer", []string{series.ID, hidden.ID}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial history survived rollback: %d, %v", count, err)
	}
	if err := db.Callback().Create().Remove("test:fail-history"); err != nil {
		t.Fatal(err)
	}
	mark(series.ID, true)
	if err := db.Model(&model.PlaybackHistory{}).Where("user_id = ? AND metadata_id NOT IN ?", "viewer", []string{series.ID, hidden.ID}).Count(&count).Error; err != nil || count != 206 {
		t.Fatalf("batch mark count=%d, want 206: %v", count, err)
	}
}
