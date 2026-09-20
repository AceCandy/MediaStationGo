package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestNFOTasksUseCommonDefinitions(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	tracker.ConfigurePersistence(nil, t.TempDir())
	for _, kind := range []string{TaskKindScan, TaskKindWatch, TaskKindScrape, TaskKindNFOScan, TaskKindNFOWatch} {
		startFinishedTask(t, tracker, kind, kind, TaskUpdate{Details: []string{kind}})
	}
	definitions, err := tracker.DefinitionsForSystem(nil, model.TaskSystemNFO)
	if err != nil || len(definitions) != 0 {
		t.Fatalf("NFO definitions: %v %v", definitions, err)
	}
	definitions, err = tracker.Definitions(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if definition.Key == TaskKindNFOScan || definition.Key == TaskKindNFOWatch {
			t.Fatalf("legacy NFO task still visible: %+v", definition)
		}
		if definition.Key == TaskDefinitionLibraryScan || definition.Key == TaskDefinitionLibraryWatch {
			if definition.Latest == nil || definition.System != model.TaskSystemCommon {
				t.Fatalf("missing common task history: %+v", definition)
			}
		}
	}
	for system, want := range map[string]int64{model.TaskSystemCommon: 2, model.TaskSystemCatalog: 1, model.TaskSystemNFO: 2} {
		page, err := tracker.ListSystem(system, 1, 30)
		if err != nil || page.Total != want {
			t.Fatalf("%s total=%d want=%d err=%v", system, page.Total, want, err)
		}
	}
	for _, key := range []string{TaskKindNFOScan, TaskKindNFOWatch} {
		history, err := tracker.DefinitionHistory(key, 1, 10)
		if err != nil || history.Total != 1 {
			t.Fatalf("legacy NFO history unavailable: %s %+v %v", key, history, err)
		}
		log, err := tracker.ReadDefinitionLog(key, "", 0)
		if err != nil || !strings.Contains(log.Content, key) {
			t.Fatalf("legacy NFO log unavailable: %s %+v %v", key, log, err)
		}
	}
}

func TestNFOWatcherSharesMixedBatch(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.TaskExecution{})
	repos := repository.New(db)
	log := zap.NewNop()
	tracker := NewTaskTrackerService(log, nil)
	scanner := NewScannerService(&config.Config{}, log, repos, nil, nil, nil)
	watcher := NewWatcherService(log, repos, scanner, tracker)
	var due []duePath
	for _, libraryType := range []string{"movie", model.LibraryTypeHongGuo, model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		lib := model.Library{Name: libraryType, Type: libraryType, Path: t.TempDir()}
		if err := repos.Library.Create(t.Context(), &lib); err != nil {
			t.Fatal(err)
		}
		due = append(due, duePath{path: filepath.Join(lib.Path, "deleted.mkv"), libraryID: lib.ID})
	}
	watcher.processBatch(t.Context(), due)
	history, err := tracker.List(1, 10)
	if err != nil || len(history.Items) != 1 || history.Items[0].Kind != TaskKindWatch || history.Items[0].System != model.TaskSystemCommon || history.Items[0].Metrics["total"] != 4 {
		t.Fatalf("mixed watch history=%+v err=%v", history, err)
	}
}

func TestNFOSchedulerSharesLibraries(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	log := zap.NewNop()
	tracker := NewTaskTrackerService(log, nil)
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scheduler := NewSchedulerService(log, repos, scanner, nil, NewHub(log))
	scheduler.SetTaskTracker(tracker)
	for _, libraryType := range []string{"movie", model.LibraryTypeHongGuo, model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		lib := model.Library{Name: libraryType, Type: libraryType, Path: t.TempDir(), Enabled: true}
		if err := repos.Library.Create(t.Context(), &lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatal(err)
	}
	history, err := tracker.List(1, 10)
	if err != nil || len(history.Items) != 1 || history.Items[0].Kind != TaskKindScan || history.Items[0].System != model.TaskSystemCommon || history.Items[0].Metrics["libraries"] != 4 {
		t.Fatalf("mixed scan history=%+v err=%v", history, err)
	}
}

func TestNFOTaskLegacySystemFilter(t *testing.T) {
	db := newServiceTestDB(t, &model.TaskExecution{})
	repo := repository.New(db).TaskExecution
	for _, system := range []string{"", model.TaskSystemNFO} {
		row := model.TaskExecution{System: system, Kind: TaskKindNFOScan, Trigger: TaskTriggerManual, Name: "NFO", Status: TaskStatusCompleted, StartedAt: time.Now()}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for system, want := range map[string]int64{model.TaskSystemNFO: 2, model.TaskSystemCatalog: 0, model.TaskSystemCommon: 0, model.TaskSystemHongGuo: 0} {
		_, total, err := repo.ListFiltered(t.Context(), repository.TaskExecutionFilter{System: system}, 0, 30)
		if err != nil || total != want {
			t.Fatalf("%s total=%d want=%d err=%v", system, total, want, err)
		}
	}
}
