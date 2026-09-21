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
	readyAt   time.Time
	attempts  int
	metadata  bool
	directory bool
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
	failedRoots := make(map[string]string)
	var failures []error
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
				if err == nil {
					err = errors.New("路径不是目录")
				}
				failures = append(failures, fmt.Errorf("监听目录不可访问: %s: %w", root.Path, err))
				for _, path := range mappedPathCandidates(root.Path) {
					failedRoots[path] = l.ID
				}
				w.pending[root.Path] = pendingEvent{libraryID: l.ID, readyAt: time.Now().Add(30 * time.Second), directory: true}
				continue
			}
			dirs, err := listDirsForWatch(watchRoot)
			if err != nil {
				failures = append(failures, err)
				failedRoots[watchRoot] = l.ID
				w.pending[watchRoot] = pendingEvent{libraryID: l.ID, readyAt: time.Now().Add(30 * time.Second), directory: true}
			}
			for _, dir := range dirs {
				current[dir] = l.ID
			}
		}
	}
	// Remove disappeared paths.
	for path, libraryID := range w.watched {
		preserve := false
		for root, id := range failedRoots {
			if id == libraryID && pathBelongsToRoot(path, root) {
				preserve = true
				break
			}
		}
		if _, ok := current[path]; !ok && !preserve {
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
			failures = append(failures, err)
			w.pending[path] = pendingEvent{libraryID: id, readyAt: time.Now().Add(30 * time.Second), directory: true}
			continue
		}
		w.watched[path] = id
	}
	return errors.Join(failures...)
}

// listDirsForWatch returns root plus every (non-hidden) subdirectory so the
// watcher can register the whole tree recursively.
func listDirsForWatch(root string) ([]string, error) {
	dirs := []string{root}
	err := walk(root, func(path string, info walkInfo) error {
		if info.isDir && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	return dirs, err
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
			w.mu.Lock()
			_, directory := w.watched[ev.Name]
			if directory && ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				for path := range w.watched {
					if pathBelongsToRoot(path, ev.Name) {
						_ = w.watcher.Remove(path)
						delete(w.watched, path)
					}
				}
			}
			old := w.pending[ev.Name]
			w.pending[ev.Name] = pendingEvent{libraryID: lib, readyAt: time.Now().Add(5 * time.Second), metadata: old.metadata, directory: directory || old.directory}
			w.mu.Unlock()
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn("watcher error", zap.Error(err))
			// 丢失事件后仅合并一次目录补扫；正常单文件事件仍不触发全库遍历。
			w.queueWatchRecovery()
		}
	}
}

// findLibrary maps a path back to the watching library ID, taking the
// shortest matching prefix.
func (w *WatcherService) findLibrary(path string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if id, ok := w.watched[path]; ok {
		return id
	}
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
	attempts  int
	metadata  bool
	directory bool
}

// debouncer drains the pending set every 5 s and processes each settled path
// incrementally: existing files are ingested (single-file upsert), vanished
// files are removed. Coalescing by path avoids storming the disk during bulk
// operations (mass-rename, large copies). Normal file events touch only changed
// paths; directory events and watch failures explicitly queue subtree recovery.
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
			if !now.Before(ev.readyAt) {
				due = append(due, duePath{path: path, libraryID: ev.libraryID, attempts: ev.attempts, metadata: ev.metadata, directory: ev.directory})
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
	directories := make([]duePath, 0)
	for _, d := range due {
		if fi, err := os.Stat(d.path); err == nil && fi.IsDir() {
			d.directory = true
		}
		if d.directory {
			directories = append(directories, d)
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
	if len(candidates) == 0 && len(sidecars) == 0 && len(directories) == 0 {
		return
	}
	libs, err := w.repo.Library.List(ctx)
	if err != nil {
		w.requeue(candidates)
		w.requeue(sidecars)
		w.requeue(directories)
		w.log.Error("load watcher libraries failed", zap.Error(err))
		return
	}
	libraries := make(map[string]model.Library)
	for _, lib := range libs {
		libraries[lib.ID] = lib
	}
	for _, d := range directories {
		if lib, ok := libraries[d.libraryID]; !ok || !lib.Enabled {
			continue
		}
		if err := w.queueDirectory(ctx, d); err != nil {
			if _, statErr := os.Stat(d.path); os.IsNotExist(statErr) {
				if missingErr := w.queueMissingDirectory(ctx, d); missingErr != nil {
					w.log.Warn("watch missing directory reconciliation failed", zap.Error(missingErr))
				}
			}
			w.log.Warn("watch directory reconciliation failed", zap.String("path", d.path), zap.Error(err))
			w.requeue([]duePath{d})
		}
	}
	active := candidates[:0]
	for _, d := range candidates {
		if lib, ok := libraries[d.libraryID]; ok && lib.Enabled {
			active = append(active, d)
		}
	}
	candidates = active
	seen := map[string]int{}
	for i, candidate := range candidates {
		seen[candidate.path] = i
	}
	for _, sidecar := range sidecars {
		lib, ok := libraries[sidecar.libraryID]
		if !ok || !lib.Enabled || lib.Type == model.LibraryTypeHongGuo {
			continue
		}
		nfo := libraryUsesNFOOnly(&lib)
		if !nfo && !strings.EqualFold(filepath.Ext(sidecar.path), ".nfo") {
			continue
		}
		// 侧车只重读所在目录下已入库的文件；不重新遍历媒体库目录树。
		prefix := filepath.Dir(sidecar.path) + string(filepath.Separator)
		var paths []string
		source := ""
		if nfo {
			source = model.CatalogSourceNFO
		}
		err := w.repo.DB.WithContext(ctx).Model(&model.Media{}).
			Where("library_id = ? AND COALESCE(catalog_source, '') = ? AND path LIKE ? ESCAPE '\\'", sidecar.libraryID, source, repository.EscapeLike(prefix)+"%").Pluck("path", &paths).Error
		if err != nil {
			w.requeue([]duePath{sidecar})
			continue
		}
		for _, path := range paths {
			if index, ok := seen[path]; ok {
				candidates[index].metadata = candidates[index].metadata || !nfo
			} else {
				seen[path] = len(candidates)
				candidates = append(candidates, duePath{path: path, libraryID: sidecar.libraryID, attempts: sidecar.attempts, metadata: !nfo})
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
	defer func() {
		if metrics["added"]+metrics["updated"] > 0 {
			w.scanner.WakeProbeBackfill()
		}
	}()
	for _, d := range candidates {
		if ctx.Err() != nil {
			w.requeue([]duePath{d})
			continue
		}
		details, key := w.processPath(ctx, d)
		if key == "failed" {
			w.requeue([]duePath{d})
		}
		metrics[key]++
		task.Update(TaskUpdate{Stage: "watch", Metrics: metrics, Details: details})
	}
	if ctx.Err() != nil {
		task.Finish(ctx.Err(), TaskUpdate{Stage: "watch", Message: "媒体库变更处理已取消", Metrics: metrics})
		return
	}
	if metrics["failed"] > 0 {
		task.Finish(errors.New("部分文件变更处理失败"), TaskUpdate{Stage: "watch", Message: "媒体库变更处理失败", Metrics: metrics})
		return
	}
	task.Finish(nil, TaskUpdate{Stage: "completed", Message: "媒体库变更处理完成", Metrics: metrics})
}

func (w *WatcherService) requeue(paths []duePath) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range paths {
		if next, exists := w.pending[d.path]; exists {
			next.metadata = next.metadata || d.metadata
			next.directory = next.directory || d.directory
			w.pending[d.path] = next
		} else if d.attempts < 5 {
			delay := min(30*time.Second*time.Duration(1<<d.attempts), 5*time.Minute)
			w.pending[d.path] = pendingEvent{libraryID: d.libraryID, readyAt: time.Now().Add(delay), attempts: d.attempts + 1, metadata: d.metadata, directory: d.directory}
		} else {
			w.log.Warn("watch retry limit reached; next event or library scan required", zap.String("path", d.path))
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
	metadataChanged := false
	if d.metadata {
		if changed, err := w.scanner.refreshLocalMetadataHints(ctx, d.libraryID, d.path); err != nil {
			return []string{fmt.Sprintf("❌ 本地资料 %s: %v", d.path, sanitizeTaskLogError(err))}, "failed"
		} else if changed {
			metadataChanged = true
			w.scanner.startAutoScrape(ctx, d.libraryID)
		}
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
	if metadataChanged {
		return []string{"🔄 更新本地资料提示 " + d.path}, "metadata"
	}
	return []string{"⏭️ 文件未产生入库变化 " + d.path}, "skipped"
}
