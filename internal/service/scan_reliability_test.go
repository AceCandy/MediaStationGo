package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestScanWalkReportsMissingAndUnreadablePaths(t *testing.T) {
	root := t.TempDir()
	if err := walk(filepath.Join(root, "missing"), func(string, walkInfo) error { return nil }); !os.IsNotExist(err) {
		t.Fatalf("missing root: %v", err)
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	err := walk(root, func(path string, info walkInfo) error {
		if path == child {
			return os.Remove(child)
		}
		return nil
	})
	if !os.IsNotExist(err) {
		t.Fatalf("removed child must fail traversal: %v", err)
	}
}

func TestRemovePathRejectsAccessErrorsBeforeDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.mkv")
	if err := os.Symlink(path, path); err != nil {
		t.Fatal(err)
	}
	scanner := &ScannerService{}
	if removed, err := scanner.RemovePath(t.Context(), path); err == nil || os.IsNotExist(err) || removed != 0 {
		t.Fatalf("access error treated as removal: %d %v", removed, err)
	}
}

func TestRemovePathPreservesOfflineRoot(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := filepath.Join(t.TempDir(), "offline")
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Path: filepath.Join(root, "movie.mkv")}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if removed, err := scanner.RemovePath(t.Context(), media.Path); err == nil || removed != 0 {
		t.Fatalf("offline root removed records: %d %v", removed, err)
	}
	if countMedia(t, repos) != 1 {
		t.Fatal("offline file was deleted")
	}
}

func TestScanUnreadableSubdirectoryPreservesRootRecords(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	stale := model.Media{LibraryID: lib.ID, Path: filepath.Join(root, "vanished.mkv")}
	if err := repos.DB.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "a.mkv"), "video")
	blocked := filepath.Join(root, "z-blocked")
	if err := os.Mkdir(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0700) })
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if !errors.Is(err, os.ErrPermission) || res.Added != 1 || res.Removed != 0 || res.ErrorCount == 0 {
		t.Fatalf("partial traversal: %+v %v", res, err)
	}
	if countMedia(t, repos) != 2 {
		t.Fatal("partial traversal pruned prior records or lost flushed writes")
	}
}

func TestWatcherRetriesPreserveNewEventsAndStop(t *testing.T) {
	w := NewWatcherService(zap.NewNop(), nil, nil, nil)
	d := duePath{path: "/media/movie.mkv", libraryID: "lib", metadata: true}
	w.requeue([]duePath{d})
	first := w.pending[d.path]
	if first.attempts != 1 || time.Until(first.readyAt) < 25*time.Second || !first.metadata {
		t.Fatalf("first retry: %+v", first)
	}
	fresh := pendingEvent{libraryID: "lib", readyAt: time.Now().Add(time.Second)}
	w.pending[d.path] = fresh
	w.requeue([]duePath{d})
	if got := w.pending[d.path]; got.attempts != 0 || !got.readyAt.Equal(fresh.readyAt) || !got.metadata {
		t.Fatalf("retry replaced a new event: %+v", got)
	}
	delete(w.pending, d.path)
	d.attempts = 5
	w.requeue([]duePath{d})
	if len(w.pending) != 0 {
		t.Fatal("retry exceeded its bound")
	}
}

func TestWatcherRecoversMovedDirectoryAndDeletedFiles(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	w := NewWatcherService(zap.NewNop(), repos, scanner, NewTaskTrackerService(zap.NewNop(), nil))
	w.watcher = fw
	dir := filepath.Join(root, "moved")
	file := filepath.Join(dir, "movie.mkv")
	writeTestFile(t, file, "video")
	w.processBatch(t.Context(), []duePath{{path: dir, libraryID: lib.ID, directory: true}})
	if _, ok := w.pending[file]; !ok || w.watched[dir] != lib.ID {
		t.Fatalf("moved files not queued or directory not watched: %+v", w.pending)
	}
	w.processBatch(t.Context(), []duePath{{path: file, libraryID: lib.ID}})
	if countMedia(t, repos) != 1 {
		t.Fatal("moved file not ingested")
	}
	delete(w.pending, file)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	w.processBatch(t.Context(), []duePath{{path: dir, libraryID: lib.ID, directory: true}})
	if _, ok := w.pending[file]; !ok {
		t.Fatal("directory removal did not queue old media")
	}
	w.processBatch(t.Context(), []duePath{{path: file, libraryID: lib.ID}})
	if countMedia(t, repos) != 0 {
		t.Fatal("removed subtree retained vanished media")
	}
}

func TestWatcherRecoveryCoalescesRoots(t *testing.T) {
	w := NewWatcherService(zap.NewNop(), nil, nil, nil)
	w.watched["/media"] = "lib"
	w.watched["/media/a"] = "lib"
	w.watched["/media/a/b"] = "lib"
	w.queueWatchRecovery()
	first := w.pending["/media"]
	w.queueWatchRecovery()
	if len(w.pending) != 1 || !w.pending["/media"].readyAt.Equal(first.readyAt) {
		t.Fatalf("overflow duplicated or postponed recovery: %+v", w.pending)
	}
}

func TestWatcherOfflineRecoveryDoesNotReviveDisabledLibrary(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "disabled", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	offline := model.Library{Name: "offline", Path: filepath.Join(t.TempDir(), "offline"), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &offline); err != nil {
		t.Fatal(err)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	w := NewWatcherService(zap.NewNop(), repos, scanner, NewTaskTrackerService(zap.NewNop(), nil))
	w.watcher = fw
	if err := fw.Add(root); err != nil {
		t.Fatal(err)
	}
	w.watched[root] = lib.ID
	if err := repos.DB.Model(&lib).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(t.Context()); err == nil {
		t.Fatal("offline root must be reported")
	}
	if _, ok := w.watched[root]; ok {
		t.Fatal("unrelated offline root retained disabled watch")
	}
	path := filepath.Join(root, "movie.mkv")
	writeTestFile(t, path, "video")
	w.processBatch(t.Context(), []duePath{{path: root, libraryID: lib.ID, directory: true}, {path: path, libraryID: lib.ID}})
	if _, ok := w.watched[root]; ok || countMedia(t, repos) != 0 {
		t.Fatal("stale events revived disabled library")
	}
	delete(w.pending, offline.Path)
	w.processBatch(t.Context(), []duePath{{path: offline.Path, libraryID: offline.ID, directory: true}})
	if pending := w.pending[offline.Path]; !pending.directory || pending.attempts != 1 {
		t.Fatalf("offline root lost watch recovery: %+v", pending)
	}
	writeTestFile(t, filepath.Join(offline.Path, "recovered.mkv"), "video")
	delete(w.pending, offline.Path)
	w.processBatch(t.Context(), []duePath{{path: offline.Path, libraryID: offline.ID, directory: true, attempts: 1}})
	if w.watched[offline.Path] != offline.ID {
		t.Fatal("recovered root did not regain watch")
	}
}

func TestWatcherFilesystemDirectoryEvents(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcherService(zap.NewNop(), repos, scanner, NewTaskTrackerService(zap.NewNop(), nil))
	w.watcher = fw
	if err := w.Refresh(t.Context()); err != nil {
		_ = fw.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); w.loop(ctx) }()
	t.Cleanup(func() { cancel(); <-done; _ = fw.Close() })
	take := func(path string) duePath {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			w.mu.Lock()
			event, ok := w.pending[path]
			delete(w.pending, path)
			w.mu.Unlock()
			if ok {
				return duePath{path: path, libraryID: event.libraryID, directory: event.directory, metadata: event.metadata, attempts: event.attempts}
			}
			select {
			case <-tick.C:
			case <-timer.C:
				t.Fatalf("filesystem event not received: %s", path)
			}
		}
	}
	source := filepath.Join(t.TempDir(), "packed")
	writeTestFile(t, filepath.Join(source, "movie.mkv"), "video")
	dir := filepath.Join(root, "moved")
	if err := os.Rename(source, dir); err != nil {
		t.Fatal(err)
	}
	w.processBatch(t.Context(), []duePath{take(dir)})
	path := filepath.Join(dir, "movie.mkv")
	w.processBatch(t.Context(), []duePath{take(path)})
	if countMedia(t, repos) != 1 {
		t.Fatal("directory arrival event failed to ingest its existing file")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	removed := take(dir)
	if !removed.directory {
		t.Fatal("directory removal lost its reconciliation intent")
	}
	w.processBatch(t.Context(), []duePath{removed})
	w.processBatch(t.Context(), []duePath{take(path)})
	if countMedia(t, repos) != 0 {
		t.Fatal("directory removal event retained vanished media")
	}
}
