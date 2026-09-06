package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestSeriesLocalCorrectionProtectsConcurrentEdits(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "测试剧", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	for i, source := range []string{"manual", "tmdb", "unchanged"} {
		season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: i + 1, Title: "ซีซั่น 1", Source: "tmdb", Rating: 8}
		if err := repos.DB.Create(&season).Error; err != nil {
			t.Fatal(err)
		}
		snapshot := model.MetadataProviderSnapshot{MetadataID: season.ID, Metadata: season, Provider: "tmdb", Payload: `{"name":"第 1 季"}`}
		if source != "unchanged" {
			if err := repos.DB.Model(&model.MetadataItem{}).Where("id = ?", season.ID).Updates(map[string]any{"source": source, "title": "用户刚保存的标题", "updated_at": season.UpdatedAt.Add(time.Second)}).Error; err != nil {
				t.Fatal(err)
			}
		}
		updated, err := s.localizeTMDbCatalogSnapshot(t.Context(), &snapshot)
		if err != nil || updated != (source == "unchanged") {
			t.Fatalf("source=%s updated=%v err=%v", source, updated, err)
		}
		after, err := repos.Metadata.FindByID(t.Context(), season.ID)
		if err != nil || after == nil {
			t.Fatalf("read result: %v", err)
		}
		if source != "unchanged" && (after.Title != "用户刚保存的标题" || after.Source != source) {
			t.Fatalf("concurrent edit overwritten: %#v", after)
		}
		if after.Rating != 8 {
			t.Fatal("unrelated rating changed")
		}
	}
}

func TestSeriesLocalCorrectionTaskVersionAndRetry(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	if err := repos.DB.AutoMigrate(&model.Setting{}, &model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	s.tasks = NewTaskTrackerService(nil, nil)
	s.tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "测试剧", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "ซีซั่น 1", Source: "tmdb"}
	if err := repos.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), season.ID, "tmdb", json.RawMessage(`{"name":123}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSeriesLocalCorrection(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	s.WaitCatalogHydrationWorker()
	version, _ := repos.Setting.Get(t.Context(), seriesLocalCorrectionVersionKey)
	recent := s.tasks.memorySnapshot().Recent
	if version == seriesLocalCorrectionVersion || len(recent) != 1 || recent[0].Status != TaskStatusFailed || recent[0].Metrics["failed"] != 1 {
		t.Fatalf("failed run marked complete: version=%s recent=%#v", version, recent)
	}
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), season.ID, "tmdb", json.RawMessage(`{"name":"第 1 季"}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSeriesLocalCorrection(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	s.WaitCatalogHydrationWorker()
	version, _ = repos.Setting.Get(t.Context(), seriesLocalCorrectionVersionKey)
	recent = s.tasks.memorySnapshot().Recent
	if version != seriesLocalCorrectionVersion || len(recent) != 2 || recent[0].Status != TaskStatusCompleted || recent[0].Metrics["updated"] != 1 || recent[0].Trigger != TaskTriggerManual {
		t.Fatalf("retry failed: version=%s recent=%#v", version, recent)
	}
	log, err := s.tasks.ReadDefinitionLog(TaskDefinitionSeriesLocalCorrection, "", 0)
	if err != nil || !strings.Contains(log.Content, "纠正 1") {
		t.Fatalf("correction log missing: %#v err=%v", log, err)
	}
	if err := s.StartSeriesLocalCorrection(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	s.WaitCatalogHydrationWorker()
	if len(s.tasks.memorySnapshot().Recent) != 2 {
		t.Fatal("completed version repeated at startup")
	}
	active := s.tasks.StartTriggeredIfKindIdle(TaskKindSeriesLocalCorrection, TaskTriggerManual, "剧集本地资料纠正", TaskUpdate{})
	if err := s.StartSeriesLocalCorrection(t.Context(), false); !errors.Is(err, ErrSeriesLocalCorrectionRunning) {
		t.Fatalf("duplicate run: %v", err)
	}
	active.Finish(nil, TaskUpdate{})
}
