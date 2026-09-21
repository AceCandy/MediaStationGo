package service

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// queueWatchRecovery 只在监听异常时合并根目录补扫，避免每个丢失事件重复遍历。
func (w *WatcherService) queueWatchRecovery() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for path, libraryID := range w.watched {
		root := true
		for parent := filepath.Dir(path); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			if w.watched[parent] == libraryID {
				root = false
				break
			}
		}
		if root {
			if _, exists := w.pending[path]; !exists {
				w.pending[path] = pendingEvent{libraryID: libraryID, readyAt: time.Now().Add(5 * time.Second), directory: true}
			}
		}
	}
}

// queueDirectory 先注册目录再读取其内容，补齐整体移入目录中没有单文件事件的媒体。
func (w *WatcherService) queueDirectory(ctx context.Context, d duePath) error {
	err := walk(d.path, func(path string, info walkInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.isDir {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.watcher != nil {
				if err := w.watcher.Add(path); err != nil {
					return err
				}
			}
			w.watched[path] = d.libraryID
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, video := videoExtensions[ext]; video || ext == ".nfo" {
			w.queueRecoveredPath(path, d)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return w.queueMissingDirectory(ctx, d)
}

// queueMissingDirectory 分页重验旧文件；是否删除仍由共享删除边界逐个核实。
func (w *WatcherService) queueMissingDirectory(ctx context.Context, d duePath) error {
	after := ""
	for {
		var rows []model.Media
		err := w.repo.DB.WithContext(ctx).Select("id", "path").
			Where("library_id = ? AND path LIKE ? ESCAPE '\\' AND id > ?", d.libraryID, repository.EscapeLike(filepath.Clean(d.path)+string(filepath.Separator))+"%", after).
			Order("id").Limit(200).Find(&rows).Error
		if err != nil {
			return err
		}
		for _, row := range rows {
			w.queueRecoveredPath(row.Path, d)
			after = row.ID
		}
		if len(rows) < 200 {
			return nil
		}
	}
}

func (w *WatcherService) queueRecoveredPath(path string, d duePath) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.pending[path]; !exists {
		w.pending[path] = pendingEvent{libraryID: d.libraryID, readyAt: time.Now().Add(5 * time.Second)}
	}
}
