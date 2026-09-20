package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestNFOTaskIsolation(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	for _, kind := range []string{TaskKindScan, TaskKindWatch, TaskKindScrape, TaskKindNFOScan, TaskKindNFOWatch} {
		startFinishedTask(t, tracker, kind, kind, TaskUpdate{})
	}
	definitions, err := tracker.DefinitionsForSystem(nil, model.TaskSystemNFO)
	if err != nil || len(definitions) != 2 {
		t.Fatalf("NFO definitions: %v %v", definitions, err)
	}
	for _, definition := range definitions {
		if definition.Latest == nil || definition.Latest.System != model.TaskSystemNFO {
			t.Fatalf("missing NFO history: %+v", definition)
		}
	}
	for system, want := range map[string]int64{model.TaskSystemCommon: 2, model.TaskSystemCatalog: 1, model.TaskSystemNFO: 2} {
		page, err := tracker.ListSystem(system, 1, 30)
		if err != nil || page.Total != want {
			t.Fatalf("%s total=%d want=%d err=%v", system, page.Total, want, err)
		}
	}
	for _, kind := range []string{model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		if LibraryScanTaskKind(&model.Library{Type: kind}) != TaskKindNFOScan {
			t.Fatalf("NFO library routed to common: %s", kind)
		}
	}
}

func TestNFOWatcherSeparatesMixedBatch(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.TaskExecution{})
	repos := repository.New(db)
	log := zap.NewNop()
	tracker := NewTaskTrackerService(log, nil)
	scanner := NewScannerService(&config.Config{}, log, repos, nil, nil, nil)
	watcher := NewWatcherService(log, repos, scanner, tracker)
	var due []duePath
	for _, libraryType := range []string{"movie", model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		lib := model.Library{Name: libraryType, Type: libraryType, Path: t.TempDir()}
		if err := repos.Library.Create(t.Context(), &lib); err != nil {
			t.Fatal(err)
		}
		due = append(due, duePath{path: filepath.Join(lib.Path, "deleted.mkv"), libraryID: lib.ID})
	}
	watcher.processBatch(t.Context(), due)
	for key, want := range map[string]int64{TaskDefinitionLibraryWatch: 1, TaskKindNFOWatch: 2} {
		history, err := tracker.DefinitionHistory(key, 1, 10)
		if err != nil || len(history.Items) != 1 || history.Items[0].Metrics["total"] != want {
			t.Fatalf("%s history=%+v err=%v", key, history, err)
		}
	}
}

func TestNFOSchedulerSeparatesLibraries(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	log := zap.NewNop()
	tracker := NewTaskTrackerService(log, nil)
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scheduler := NewSchedulerService(log, repos, scanner, nil, NewHub(log))
	scheduler.SetTaskTracker(tracker)
	for _, libraryType := range []string{"movie", model.LibraryTypeNFOMovie} {
		lib := model.Library{Name: libraryType, Type: libraryType, Path: t.TempDir(), Enabled: true}
		if err := repos.Library.Create(t.Context(), &lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{TaskDefinitionLibraryScan, TaskKindNFOScan} {
		history, err := tracker.DefinitionHistory(key, 1, 10)
		if err != nil || len(history.Items) != 1 || history.Items[0].Metrics["libraries"] != 1 {
			t.Fatalf("%s history=%+v err=%v", key, history, err)
		}
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
