package service

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTaskDefinitionHistorySeparatesSharedKinds(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	startFinishedTask(t, tracker, TaskKindTMDbPeopleSupplement, "TMDb 演职员与人物资料补全", TaskUpdate{Stage: "people"})
	startFinishedTask(t, tracker, TaskKindPeople, "人物翻译", TaskUpdate{Stage: "translation"})
	startFinishedTask(t, tracker, TaskKindScrape, "媒体入库刮削：本地电影", TaskUpdate{Stage: "scrape", SourcePath: "/media/movie.mkv"})
	startFinishedTask(t, tracker, TaskKindScrape, "发现目录刮削：电影 1221950", TaskUpdate{Stage: "scrape"})
	startFinishedTask(t, tracker, TaskKindTMDbImages, "TMDb 图片本地化修复", TaskUpdate{Stage: "repair"})

	tests := []struct {
		key  string
		name string
	}{
		{TaskKindTMDbPeopleSupplement, "TMDb 演职员与人物资料补全"},
		{TaskDefinitionPeopleTranslation, "人物翻译"},
		{TaskDefinitionMediaScrape, "媒体入库刮削：本地电影"},
		{TaskKindTMDbImages, "TMDb 图片本地化修复"},
	}
	for _, tt := range tests {
		page, err := tracker.DefinitionHistory(tt.key, 1, 30)
		if err != nil {
			t.Fatalf("history %s: %v", tt.key, err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Name != tt.name {
			t.Fatalf("history %s = %#v", tt.key, page)
		}
	}
	definitions, err := tracker.Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if definition.Key == TaskKindTMDbImages && (definition.Name != "TMDb 图片下载与修复" || definition.Latest == nil) {
			t.Fatalf("renamed image definition lost history: %+v", definition)
		}
	}
}

func TestTaskDefinitionLogsSeparateSharedKinds(t *testing.T) {
	db := newServiceTestDB(t, &model.TaskExecution{})
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	tracker.ConfigurePersistence(repository.New(db).TaskExecution, t.TempDir())
	tracker.now = func() time.Time { return time.Date(2026, 8, 13, 19, 0, 0, 0, time.Local) }

	startFinishedTask(t, tracker, TaskKindTMDbPeopleSupplement, "TMDb 演职员与人物资料补全", TaskUpdate{Details: []string{"backfill"}})
	startFinishedTask(t, tracker, TaskKindPeople, "人物翻译", TaskUpdate{Details: []string{"translation"}})
	startFinishedTask(t, tracker, TaskKindScrape, "媒体入库刮削：本地电影", TaskUpdate{Details: []string{"media scrape"}})
	startFinishedTask(t, tracker, TaskKindScrape, "发现目录刮削：电影 1221950", TaskUpdate{Details: []string{"catalog scrape"}})

	tests := []struct {
		key     string
		want    string
		notWant string
	}{
		{TaskKindTMDbPeopleSupplement, "backfill", "translation"},
		{TaskDefinitionPeopleTranslation, "translation", "backfill"},
		{TaskDefinitionMediaScrape, "media scrape", "catalog scrape"},
	}
	for _, tt := range tests {
		log, err := tracker.ReadDefinitionLog(tt.key, "", 0)
		if err != nil {
			t.Fatalf("read %s: %v", tt.key, err)
		}
		if log.Date != "2026-08-13" || !strings.Contains(log.Content, tt.want) || strings.Contains(log.Content, tt.notWant) {
			t.Fatalf("log %s = %#v", tt.key, log)
		}
	}
}

func TestTaskDefinitionsIncludeIdleTasksAndLatestExecution(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	startFinishedTask(t, tracker, TaskKindPeople, "人物翻译", TaskUpdate{Stage: "translation"})

	definitions, err := tracker.Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != len(taskDefinitionSpecs)-2 {
		t.Fatalf("definitions = %d, want %d (download definitions are hidden)", len(definitions), len(taskDefinitionSpecs)-2)
	}
	for _, definition := range definitions {
		if definition.Key == TaskDefinitionPeopleTranslation {
			if definition.CurrentState != "idle" || definition.Latest == nil || definition.Latest.Name != "人物翻译" {
				t.Fatalf("translation definition = %#v", definition)
			}
			return
		}
	}
	t.Fatal("people translation definition not found")
}

func TestProbeBackfillDefinitionSupportsManualExecution(t *testing.T) {
	definitions, err := NewTaskTrackerService(nil, nil).Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if definition.Key == TaskDefinitionProbeBackfill {
			if definition.Action != "probe_backfill" {
				t.Fatalf("action = %q", definition.Action)
			}
			return
		}
	}
	t.Fatal("probe backfill definition not found")
}

func TestScheduledTaskDefinitionsSupportManualExecution(t *testing.T) {
	if TaskDefinitionExists("metadata_artwork_backfill") {
		t.Fatal("retired metadata artwork backfill definition still exists")
	}
	definitions, err := NewTaskTrackerService(nil, nil).Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		TaskKindHuangGuoAISupplement:    true,
		TaskKindHongGuoSupplement:       true,
		TaskKindHongGuoSync:             true,
		TaskKindHongGuoRank:             true,
		TaskDefinitionLibraryScan:       true,
		TaskKindTMDbPeopleSupplement:    true,
		TaskDefinitionPeopleTranslation: true,
		TaskKindTMDbImages:              true,
		TaskKindTMDbInventory:           true,
		TaskKindDoubanImages:            true,
		TaskKindTMDbEpisodeSupplement:   true,
		TaskKindDoubanMovieSupplement:   true,
		TaskDefinitionAccountCleanup:    true,
	}
	for _, definition := range definitions {
		if !want[definition.Key] {
			continue
		}
		expectedTrigger := "定时 / 手动"
		if definition.Key == TaskDefinitionLibraryScan {
			expectedTrigger = "定时 / 手动 / 新增后自动"
		}
		if definition.Key == TaskKindTMDbImages || definition.Key == TaskKindDoubanImages {
			expectedTrigger = "资料入库后自动 / 定时 / 手动"
		}
		if definition.Action != "scheduler" || definition.Trigger != expectedTrigger {
			t.Fatalf("definition %s = %#v", definition.Key, definition)
		}
		if definition.Key == TaskDefinitionTMDbEpisodeMetadataRecheck && definition.Name != "TMDb 季/集信息补全/复查" {
			t.Fatalf("season/episode recheck display name = %q", definition.Name)
		}
		if definition.Key == TaskDefinitionDoubanEnrichment && definition.Name != "豆瓣信息补齐" {
			t.Fatalf("douban enrichment display name = %q", definition.Name)
		}
		if _, ok := TaskDefinitionSchedulerJob(definition.Key); !ok {
			t.Fatalf("scheduler job missing for %s", definition.Key)
		}
		delete(want, definition.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing scheduled definitions: %v", want)
	}
}

func TestTaskDefinitionsSeparateCurrentStateFromLatestResult(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	startFinishedTask(t, tracker, TaskKindTMDbPeopleSupplement, "TMDb 演职员与人物资料补全", TaskUpdate{Stage: "people"})
	running := tracker.StartTriggered(TaskKindTMDbPeopleSupplement, TaskTriggerEvent, "TMDb 演职员与人物资料补全", TaskUpdate{Stage: "people"})
	if running == nil {
		t.Fatal("expected running task")
	}

	definitions, err := tracker.Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if definition.Key != TaskKindTMDbPeopleSupplement {
			continue
		}
		if definition.CurrentState != TaskStatusRunning {
			t.Fatalf("current state = %q", definition.CurrentState)
		}
		if definition.Latest == nil || definition.Latest.Status != TaskStatusCompleted {
			t.Fatalf("latest = %#v", definition.Latest)
		}
		if definition.Current == nil || definition.Current.Status != TaskStatusRunning {
			t.Fatalf("current = %#v", definition.Current)
		}
		return
	}
	t.Fatal("people backfill definition not found")
}

func TestTaskDefinitionHistoryRejectsUnknownKey(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	if _, err := tracker.DefinitionHistory("unknown", 1, 30); err != ErrTaskDefinitionNotFound {
		t.Fatalf("error = %v, want %v", err, ErrTaskDefinitionNotFound)
	}
}

func startFinishedTask(t *testing.T, tracker *TaskTrackerService, kind, name string, update TaskUpdate) {
	t.Helper()
	task := tracker.StartTriggered(kind, TaskTriggerEvent, name, update)
	if task == nil {
		t.Fatal("expected task handle")
	}
	update.Stage = "completed"
	task.Finish(nil, update)
}

func TestDoubanRefreshBelongsToSourceTasks(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	startFinishedTask(t, tracker, TaskKindDoubanMovieRating, "豆瓣资料刷新", TaskUpdate{})
	for _, system := range []string{model.TaskSystemCommon, model.TaskSystemMedia, model.TaskSystemDouban, model.TaskSystemTMDb} {
		definitions, err := tracker.DefinitionsForSystem(nil, system)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, definition := range definitions {
			if definition.Key == TaskKindDoubanMovieRating {
				found = true
				if definition.Latest == nil || definition.Latest.System != model.TaskSystemDouban {
					t.Fatalf("refresh history: %#v", definition)
				}
			}
			if (definition.Key == TaskDefinitionDoubanEnrichment || definition.Key == TaskKindTMDbImages) && system != model.TaskSystemMedia {
				t.Fatalf("media task moved to common: %#v", definition)
			}
		}
		if found != (system == model.TaskSystemDouban) {
			t.Fatalf("refresh definition in %s: %v", system, found)
		}
	}
}

func TestSourcePresentationTaskSurfaces(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	startFinishedTask(t, tracker, TaskKindPeople, "人物翻译", TaskUpdate{})
	definitions, err := tracker.Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	peopleFound := false
	for _, definition := range definitions {
		if definition.Key == TaskDefinitionOrganize || definition.Key == "catalog_scrape" || definition.Key == "people_backfill" {
			t.Fatal("retired organizer is still visible")
		}
		if definition.Key == TaskKindTMDbPeopleSupplement {
			peopleFound = definition.System == model.TaskSystemTMDb && definition.Action == "scheduler"
		}
		if definition.Key == TaskDefinitionPeopleTranslation {
			found = true
			if definition.System != model.TaskSystemTMDb || definition.Latest == nil || definition.Latest.System != model.TaskSystemTMDb {
				t.Fatalf("translation has wrong system: %+v", definition)
			}
		}
	}
	if !found || !peopleFound {
		t.Fatal("translation definition missing")
	}
	if _, ok := TaskDefinitionSchedulerJob(TaskDefinitionOrganize); ok {
		t.Fatal("retired organizer can still run")
	}
}
