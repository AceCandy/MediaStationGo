package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func waitForTMDbSnapshotTask(t *testing.T, tracker *TaskTrackerService, recent int) BackgroundTask {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		snapshot := tracker.Snapshot()
		if len(snapshot.Recent) >= recent {
			return snapshot.Recent[0]
		}
		select {
		case <-deadline.C:
			t.Fatal("TMDB snapshot backfill did not finish")
		case <-ticker.C:
		}
	}
}

func TestTMDbSnapshotBackfillCoversAllKindsAndPersistsAutomaticCompletion(t *testing.T) {
	db := newServiceTestDB(t, &model.MetadataProviderSnapshot{}, &model.Setting{})
	repos := repository.New(db)
	movie := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000100"}, Kind: model.MetadataKindMovie, Title: "Movie kept", Source: "manual"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "10"})
	series := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000200"}, Kind: model.MetadataKindSeries, Title: "Series kept", Source: "manual"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "20"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000300"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season kept", Source: "manual"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: "30"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000400"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode kept", Source: "manual"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindEpisode, ExternalID: "40"})
	failed := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000500"}, Kind: model.MetadataKindMovie, Title: "Failed kept", Source: "manual"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "50"})

	var requests atomic.Int32
	var recovered atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/10":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 10, "title": "Remote movie", "future_field": true})
		case "/tv/20":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 20, "name": "Remote series", "future_field": true})
		case "/tv/20/season/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 30, "season_number": 1, "name": "Remote season", "future_field": true, "episodes": []any{map[string]any{"id": 40, "season_number": 1, "episode_number": 1, "name": "单集标题", "overview": "单集简介", "future_field": true}}})
		case "/tv/20/season/1/episode/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 40, "name": "Remote episode", "future_field": true})
		case "/movie/50":
			if recovered.Load() {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 50, "title": "Recovered movie"})
			} else {
				http.Error(w, "provider failed", http.StatusBadGateway)
			}
		case "/movie/60":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 60, "title": "New movie"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)

	result, err := scraper.BackfillTMDbSnapshots(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 5 || result.Total != 5 || result.Succeeded != 4 || result.Failed != 1 || result.Remaining != 0 {
		t.Fatalf("backfill result = %#v", result)
	}
	for _, item := range []*model.MetadataItem{movie, series, season, episode} {
		assertServiceTestTMDbSnapshot(t, repos, item.ID)
	}
	if snapshot, findErr := repos.Metadata.FindProviderSnapshot(t.Context(), failed.ID, "tmdb"); findErr != nil || snapshot != nil {
		t.Fatalf("failed snapshot = %#v, err = %v", snapshot, findErr)
	}
	for _, item := range []*model.MetadataItem{movie, series, season, episode, failed} {
		stored, findErr := repos.Metadata.FindByID(t.Context(), item.ID)
		if findErr != nil || stored == nil || stored.Title != item.Title {
			t.Fatalf("metadata fields changed for %s: %#v, err = %v", item.ID, stored, findErr)
		}
	}
	if requests.Load() != 4 {
		t.Fatalf("TMDB requests = %d, want 4", requests.Load())
	}

	second, err := scraper.BackfillTMDbSnapshots(t.Context(), nil)
	if err != nil || second.Total != 1 || second.Processed != 1 || second.Failed != 1 {
		t.Fatalf("second backfill = %#v, err = %v", second, err)
	}
	if requests.Load() != 5 {
		t.Fatalf("successful snapshots were requested again: requests=%d", requests.Load())
	}

	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	scraper.SetTaskTracker(tracker)
	if err := scraper.StartTMDbSnapshotBackfill(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	automaticTask := waitForTMDbSnapshotTask(t, tracker, 1)
	if automaticTask.Status != TaskStatusFailed || automaticTask.Metrics["failed"] != 1 || automaticTask.Metrics["remaining"] != 0 {
		t.Fatalf("automatic task = %#v", automaticTask)
	}
	completed, err := repos.Setting.Get(t.Context(), tmdbSnapshotBackfillCompletedSettingKey)
	if err != nil || completed == "true" {
		t.Fatalf("completion setting = %q, err = %v", completed, err)
	}
	if err := scraper.StartTMDbSnapshotBackfill(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	retryTask := waitForTMDbSnapshotTask(t, tracker, 2)
	if retryTask.Status != TaskStatusFailed || retryTask.Metrics["failed"] != 1 {
		t.Fatalf("automatic retry task = %#v", retryTask)
	}
	if err := scraper.StartTMDbSnapshotBackfill(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	manualTask := waitForTMDbSnapshotTask(t, tracker, 3)
	if manualTask.Trigger != TaskTriggerManual || manualTask.Status != TaskStatusFailed || manualTask.Metrics["failed"] != 1 {
		t.Fatalf("manual retry task = %#v", manualTask)
	}
	recovered.Store(true)
	if err := scraper.StartTMDbSnapshotBackfill(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if task := waitForTMDbSnapshotTask(t, tracker, 4); task.Status != TaskStatusCompleted || task.Metrics["succeeded"] != 1 {
		t.Fatalf("recovered task = %#v", task)
	}
	completed, err = repos.Setting.Get(t.Context(), tmdbSnapshotBackfillCompletedSettingKey)
	if err != nil || completed != "true" {
		t.Fatalf("successful completion setting = %q, err = %v", completed, err)
	}
	before := requests.Load()
	if err := scraper.StartTMDbSnapshotBackfill(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if len(tracker.Snapshot().Recent) != 4 || requests.Load() != before {
		t.Fatal("completed backfill without gaps started again")
	}
	createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "New local movie", Source: "local"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "60"})
	if err := scraper.StartTMDbSnapshotBackfill(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if task := waitForTMDbSnapshotTask(t, tracker, 5); task.Status != TaskStatusCompleted || task.Metrics["succeeded"] != 1 {
		t.Fatalf("completion marker blocked new gap: %#v", task)
	}
}

func TestAutomaticTMDbSnapshotBackfillDoesNotMarkCancellationComplete(t *testing.T) {
	db := newServiceTestDB(t, &model.MetadataProviderSnapshot{}, &model.Setting{})
	repos := repository.New(db)
	item := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Interrupted", Source: "manual"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "10"})

	started := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
	scraper.SetTaskTracker(tracker)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := scraper.StartTMDbSnapshotBackfill(ctx, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("TMDB request did not start")
	}
	task := waitForTMDbSnapshotTask(t, tracker, 1)
	if task.Status != TaskStatusInterrupted {
		t.Fatalf("interrupted task = %#v", task)
	}
	completed, err := repos.Setting.Get(t.Context(), tmdbSnapshotBackfillCompletedSettingKey)
	if err != nil || completed != "" {
		t.Fatalf("completion setting = %q, err = %v", completed, err)
	}
	remaining, err := repos.Metadata.CountMissingTMDbSnapshots(t.Context())
	if err != nil || remaining != 1 {
		t.Fatalf("remaining candidates = %d, err = %v", remaining, err)
	}
	if snapshot, findErr := repos.Metadata.FindProviderSnapshot(t.Context(), item.ID, "tmdb"); findErr != nil || snapshot != nil {
		t.Fatalf("interrupted snapshot = %#v, err = %v", snapshot, findErr)
	}
}
