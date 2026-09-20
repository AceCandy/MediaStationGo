// Package service — filesystem watcher.
//
// WatcherService observes every enabled library root with fsnotify and
// debounces incoming events into incremental, per-file ingests. New / renamed
// files become Media rows; deletes remove them.
//
// 设计目标：只在「有新增/变更媒体」时增量入库，绝不因为单个文件变化就对整个
// 媒体库做全量重扫——全量重扫会反复读盘、损伤硬盘，也是用户明确要避免的。
// 因此 watcher 递归监听库内所有子目录，事件去抖后只处理具体变化的路径。
//
// The watcher runs in the background and is started after migrations
// complete. It survives library add / delete via Refresh().
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// pendingEvent records the most recent change to a path and the library it
// belongs to, for debounced incremental processing.
type pendingEvent struct {
	libraryID string
	ts        time.Time
}

// WatcherService is a thin orchestrator on top of fsnotify.
type WatcherService struct {
	log     *zap.Logger
	repo    *repository.Container
	scanner *ScannerService
	tasks   *TaskTrackerService

	mu      sync.Mutex
	watcher *fsnotify.Watcher
	watched map[string]string       // dir -> libraryID
	pending map[string]pendingEvent // path -> most recent change
	stop    chan struct{}
}

// NewWatcherService is the constructor.
func NewWatcherService(log *zap.Logger, repo *repository.Container, scanner *ScannerService, tasks *TaskTrackerService) *WatcherService {
	return &WatcherService{
		log:     log,
		repo:    repo,
		scanner: scanner,
		tasks:   tasks,
		watched: make(map[string]string),
		pending: make(map[string]pendingEvent),
		stop:    make(chan struct{}),
	}
}

// Start initialises the underlying fsnotify watcher and registers every
// library root currently in the database.
func (w *WatcherService) Start(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = fw
	if err := w.Refresh(ctx); err != nil {
		w.log.Warn("watcher refresh failed", zap.Error(err))
	}
	go w.loop(ctx)
	go w.debouncer(ctx)
	return nil
}

// Stop tears down the watcher (called on graceful shutdown).
func (w *WatcherService) Stop() {
	close(w.stop)
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

// Refresh reads the library list and adjusts the set of watched
// directories. Idempotent — safe to call after every CRUD.
func (w *WatcherService) Refresh(ctx context.Context) error {
	libs, err := w.repo.Library.List(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	// Map every directory (root + all subdirectories) to its library so new
	// files anywhere in the tree raise events — fsnotify itself is
	// non-recursive, so we register each directory explicitly.
	current := make(map[string]string)
	for _, l := range libs {
		if !l.Enabled {
			continue
		}
		roots := l.Roots
		if len(roots) == 0 && l.Path != "" {
			roots = []model.LibraryRoot{{LibraryID: l.ID, Path: l.Path, Enabled: true}}
		}
		for _, root := range roots {
			if !root.Enabled {
				continue
			}
			if isRetiredCloudPath(root.Path) {
				continue
			}
			watchRoot, info, err := resolveAccessibleMappedPath(root.Path)
			if err != nil || !info.IsDir() {
				w.log.Warn("watch path inaccessible",
					zap.String("path", root.Path),
					zap.String("library_id", l.ID),
					zap.String("root_id", root.ID),
					zap.Error(err))
				continue
			}
			for _, dir := range listDirsForWatch(watchRoot) {
				current[dir] = l.ID
			}
		}
	}
	// Remove disappeared paths.
	for path := range w.watched {
		if _, ok := current[path]; !ok {
			_ = w.watcher.Remove(path)
			delete(w.watched, path)
		}
	}
	// Add new ones.
	for path, id := range current {
		if _, ok := w.watched[path]; ok {
			continue
		}
		if err := w.watcher.Add(path); err != nil {
			w.log.Warn("watch add failed", zap.String("path", path), zap.Error(err))
			continue
		}
		w.watched[path] = id
	}
	return nil
}

// listDirsForWatch returns root plus every (non-hidden) subdirectory so the
// watcher can register the whole tree recursively.
func listDirsForWatch(root string) []string {
	dirs := []string{root}
	_ = walk(root, func(path string, info walkInfo) error {
		if info.isDir && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	return dirs
}

// watchDirRecursive registers a newly-created directory subtree so files
// copied into it afterwards still raise events.
func (w *WatcherService) watchDirRecursive(dir, libraryID string) {
	for _, d := range listDirsForWatch(dir) {
		if _, ok := w.watched[d]; ok {
			continue
		}
		if err := w.watcher.Add(d); err != nil {
			w.log.Debug("watch add (recursive) failed", zap.String("path", d), zap.Error(err))
			continue
		}
		w.watched[d] = libraryID
	}
}

// loop drains fsnotify events and pushes the affected library into the
// pending map. The actual rescan happens in the debouncer goroutine.
func (w *WatcherService) loop(ctx context.Context) {
	if w.watcher == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Write) == 0 {
				continue
			}
			lib := w.findLibrary(ev.Name)
			if lib == "" {
				continue
			}
			// 新建目录：立即递归纳入监听，确保随后拷入的文件也能触发事件。
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					w.mu.Lock()
					w.watchDirRecursive(ev.Name, lib)
					w.mu.Unlock()
				}
			}
			w.mu.Lock()
			w.pending[ev.Name] = pendingEvent{libraryID: lib, ts: time.Now()}
			w.mu.Unlock()
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn("watcher error", zap.Error(err))
		}
	}
}

// findLibrary maps a path back to the watching library ID, taking the
// shortest matching prefix.
func (w *WatcherService) findLibrary(path string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	dir := filepath.Dir(path)
	for {
		if id, ok := w.watched[dir]; ok {
			return id
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// duePath couples a settled path with its library for incremental processing.
type duePath struct {
	path      string
	libraryID string
}

// debouncer drains the pending set every 5 s and processes each settled path
// incrementally: existing files are ingested (single-file upsert), vanished
// files are removed. Coalescing by path avoids storming the disk during bulk
// operations (mass-rename, large copies), and crucially we never re-walk the
// entire library — only the paths that actually changed.
func (w *WatcherService) debouncer(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-t.C:
		}
		w.mu.Lock()
		due := make([]duePath, 0, len(w.pending))
		now := time.Now()
		for path, ev := range w.pending {
			if now.Sub(ev.ts) >= 5*time.Second {
				due = append(due, duePath{path: path, libraryID: ev.libraryID})
				delete(w.pending, path)
			}
		}
		w.mu.Unlock()
		w.processBatch(ctx, due)
	}
}

func (w *WatcherService) processBatch(ctx context.Context, due []duePath) {
	candidates := make([]duePath, 0, len(due))
	sidecars := make([]duePath, 0)
	for _, d := range due {
		if fi, err := os.Stat(d.path); err == nil && fi.IsDir() {
			continue
		}
		if _, ok := videoExtensions[strings.ToLower(filepath.Ext(d.path))]; ok {
			candidates = append(candidates, d)
		} else {
			switch strings.ToLower(filepath.Ext(d.path)) {
			case ".nfo", ".jpg", ".jpeg", ".png", ".webp":
				sidecars = append(sidecars, d)
			}
		}
	}
	if len(candidates) == 0 && len(sidecars) == 0 {
		return
	}
	libs, err := w.repo.Library.List(ctx)
	if err != nil {
		w.requeue(candidates)
		w.requeue(sidecars)
		w.log.Error("load watcher libraries failed", zap.Error(err))
		return
	}
	nfoLibraries := make(map[string]bool)
	for _, lib := range libs {
		nfoLibraries[lib.ID] = libraryUsesNFOOnly(&lib)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		seen[candidate.path] = true
	}
	for _, sidecar := range sidecars {
		if !nfoLibraries[sidecar.libraryID] {
			continue
		}
		// 侧车只重读所在目录下已入库的文件；不重新遍历媒体库目录树。
		prefix := filepath.Dir(sidecar.path) + string(filepath.Separator)
		var paths []string
		err := w.repo.DB.WithContext(ctx).Model(&model.Media{}).
			Where("library_id = ? AND catalog_source = ? AND path LIKE ? ESCAPE '\\'", sidecar.libraryID, model.CatalogSourceNFO, repository.EscapeLike(prefix)+"%").Pluck("path", &paths).Error
		if err != nil {
			w.requeue([]duePath{sidecar})
			continue
		}
		for _, path := range paths {
			if !seen[path] {
				candidates = append(candidates, duePath{path: path, libraryID: sidecar.libraryID})
				seen[path] = true
			}
		}
	}
	if len(candidates) == 0 {
		return
	}
	metrics := map[string]int64{"total": int64(len(candidates))}
	if w.tasks == nil {
		w.requeue(candidates)
		w.log.Error("watcher task tracker unavailable")
		return
	}
	task := w.tasks.StartTriggered(TaskKindWatch, TaskTriggerEvent, "媒体库变更监听", TaskUpdate{
		Stage: "watch", Message: "媒体库变更处理已启动", Metrics: metrics,
	})
	if task == nil {
		w.requeue(candidates)
		w.log.Error("create watcher task execution failed")
		return
	}
	for _, d := range candidates {
		details, key := w.processPath(ctx, d)
		metrics[key]++
		task.Update(TaskUpdate{Stage: "watch", Metrics: metrics, Details: details})
	}
	if metrics["failed"] > 0 {
		task.Finish(errors.New("部分文件变更处理失败"), TaskUpdate{Stage: "watch", Message: "媒体库变更处理失败", Metrics: metrics})
		if metrics["added"]+metrics["updated"] > 0 {
			w.scanner.WakeProbeBackfill()
		}
		return
	}
	task.Finish(nil, TaskUpdate{Stage: "completed", Message: "媒体库变更处理完成", Metrics: metrics})
	if metrics["added"]+metrics["updated"] > 0 {
		w.scanner.WakeProbeBackfill()
	}
}

func (w *WatcherService) requeue(paths []duePath) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range paths {
		if _, exists := w.pending[d.path]; !exists {
			w.pending[d.path] = pendingEvent{libraryID: d.libraryID, ts: time.Now()}
		}
	}
}

// processPath ingests or removes one changed media path and returns one update's details.
func (w *WatcherService) processPath(ctx context.Context, d duePath) ([]string, string) {
	fi, err := os.Stat(d.path)
	if err != nil {
		if removed, derr := w.scanner.RemovePath(ctx, d.path); derr != nil {
			w.log.Warn("watcher remove failed", zap.String("path", d.path), zap.Error(derr))
			safeErr := sanitizeTaskLogError(derr)
			return []string{fmt.Sprintf("❌ 删除 %s 失败: %v", d.path, safeErr)}, "failed"
		} else if removed > 0 {
			w.log.Info("watcher removed media", zap.String("path", d.path))
			return []string{"🗑️ 删除 " + d.path}, "removed"
		}
		return []string{"⏭️ 删除路径无对应媒体记录 " + d.path}, "skipped"
	}
	if fi.IsDir() {
		return nil, "skipped"
	}
	res, ierr := w.scanner.IngestPathResult(ctx, d.libraryID, d.path)
	if ierr != nil {
		w.log.Warn("watcher ingest failed", zap.String("path", d.path), zap.Error(ierr))
		safeErr := sanitizeTaskLogError(ierr)
		return []string{fmt.Sprintf("❌ 入库 %s 失败: %v", d.path, safeErr)}, "failed"
	}
	if res != nil && res.ErrorCount > 0 {
		details := make([]string, 0, len(res.Errors))
		for _, item := range res.Errors {
			details = append(details, "❌ "+sanitizeTaskLogError(errors.New(item)).Error())
		}
		return details, "failed"
	}
	if res != nil && res.Added+res.Updated > 0 {
		w.log.Info("watcher ingested media", zap.String("path", d.path))
		if w.scanner.scraper != nil {
			lib, err := w.scanner.repo.Library.FindByID(ctx, d.libraryID)
			if err == nil && !libraryUsesNFOOnly(lib) {
				w.scanner.scraper.WakeScrapeWorker()
			}
		}
		details := res.ChangeDetails()
		if res.Added > 0 {
			return details, "added"
		}
		return details, "updated"
	}
	return []string{"⏭️ 文件未产生入库变化 " + d.path}, "skipped"
}
