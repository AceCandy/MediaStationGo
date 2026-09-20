package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestWatcherRefreshMapsHostLibraryPathToContainerPath(t *testing.T) {
	root := t.TempDir()
	hostMedia := filepath.Join(root, "nas-host", "media")
	containerMedia := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerMedia, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatalf("mkdir container library: %v", err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", hostMedia)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerMedia)

	db := newServiceTestDB(t, &model.Library{})
	repos := repository.New(db)
	lib := model.Library{
		Base:    model.Base{ID: "lib-tv"},
		Name:    "国产剧",
		Path:    filepath.Join(hostMedia, "电视剧", "国产剧"),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer fw.Close()
	watcher := NewWatcherService(zap.NewNop(), repos, nil, nil)
	watcher.watcher = fw

	if err := watcher.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, ok := watcher.watched[filepath.Clean(containerLibrary)]; !ok {
		t.Fatalf("expected mapped container path watched, got %#v", watcher.watched)
	}
	if _, ok := watcher.watched[filepath.Clean(lib.Path)]; ok {
		t.Fatalf("host path should not be watched inside container: %#v", watcher.watched)
	}
}

func TestWatcherBatchCreatesOneIsolatedExecution(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.TaskExecution{})
	repos := repository.New(db)
	log := zap.NewNop()
	tracker := NewTaskTrackerService(log, nil)
	tracker.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	scanner := NewScannerService(&config.Config{}, log, repos, nil, nil, nil)
	watcher := NewWatcherService(log, repos, scanner, tracker)
	paths := []string{filepath.Join(t.TempDir(), "missing-a.mkv"), filepath.Join(t.TempDir(), "missing-b.mp4")}

	watcher.processBatch(t.Context(), []duePath{{path: paths[0], libraryID: "lib"}, {path: paths[1], libraryID: "lib"}})

	history, err := tracker.DefinitionHistory(TaskDefinitionLibraryWatch, 1, 10)
	if err != nil || history.Total != 1 || len(history.Items) != 1 {
		t.Fatalf("watch history = %#v, err = %v", history, err)
	}
	task := history.Items[0]
	if task.Trigger != TaskTriggerEvent || task.Status != TaskStatusCompleted || task.Metrics["total"] != 2 || task.Metrics["skipped"] != 2 {
		t.Fatalf("watch task = %#v", task)
	}
	scanHistory, err := tracker.DefinitionHistory(TaskDefinitionLibraryScan, 1, 10)
	if err != nil || scanHistory.Total != 0 {
		t.Fatalf("scan history = %#v, err = %v", scanHistory, err)
	}
	watchLog, err := tracker.ReadDefinitionLog(TaskDefinitionLibraryWatch, "", 0)
	if err != nil || !strings.Contains(watchLog.Content, paths[0]) || !strings.Contains(watchLog.Content, paths[1]) {
		t.Fatalf("watch log = %#v, err = %v", watchLog, err)
	}
}

func TestWatcherBatchContinuesAfterPathFailures(t *testing.T) {
	trackerDB := newServiceTestDB(t, &model.TaskExecution{})
	trackerRepos := repository.New(trackerDB)
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	tracker.ConfigurePersistence(trackerRepos.TaskExecution, t.TempDir())
	scannerRepos := repository.New(newServiceTestDB(t, &model.Library{}))
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), scannerRepos, nil, nil, nil)
	watcher := NewWatcherService(zap.NewNop(), scannerRepos, scanner, tracker)

	watcher.processBatch(t.Context(), []duePath{
		{path: filepath.Join(t.TempDir(), "failed-a.mkv"), libraryID: "lib"},
		{path: filepath.Join(t.TempDir(), "failed-b.mkv"), libraryID: "lib"},
	})

	history, err := tracker.DefinitionHistory(TaskDefinitionLibraryWatch, 1, 10)
	if err != nil || len(history.Items) != 1 {
		t.Fatalf("watch history = %#v, err = %v", history, err)
	}
	if task := history.Items[0]; task.Status != TaskStatusFailed || task.Metrics["failed"] != 2 {
		t.Fatalf("failed watch task = %#v", task)
	}
}

func TestWatcherBatchRequeuesWhenTaskCreationFails(t *testing.T) {
	trackerRepos := repository.New(newServiceTestDB(t, &model.Library{}))
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	tracker.ConfigurePersistence(trackerRepos.TaskExecution, t.TempDir())
	watcher := NewWatcherService(zap.NewNop(), trackerRepos, nil, tracker)
	path := filepath.Join(t.TempDir(), "retry.mkv")

	watcher.processBatch(t.Context(), []duePath{{path: path, libraryID: "lib"}})

	if pending, ok := watcher.pending[path]; !ok || pending.libraryID != "lib" {
		t.Fatalf("pending = %#v, want requeued path", watcher.pending)
	}
}
