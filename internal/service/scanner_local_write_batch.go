package service

import (
	"context"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type localMediaWriteBatch struct {
	scanner            *ScannerService
	ctx                context.Context
	res                *ScanResult
	limit              int
	items              []localMediaWriteItem
	mediaRepo          *repository.MediaRepository
	flushSearch        func(context.Context)
	recognitionUses    int
	recognitionEnabled bool
	recognitionRules   []recognitionWordRule
	nfoCache           *nfoScanCache
}

type localMediaWriteItem struct {
	path         string
	media        *model.Media
	updateReason string
}

func newLocalMediaWriteBatch(scanner *ScannerService, ctx context.Context, res *ScanResult, limit int) *localMediaWriteBatch {
	if limit <= 0 {
		limit = 100
	}
	writer, flush := scanner.repo.Media.WithBatchedHongGuoSearch(limit)
	return &localMediaWriteBatch{scanner: scanner, ctx: ctx, res: res, limit: limit, mediaRepo: writer, flushSearch: flush}
}

// cleanQuery 仅为需入库的文件读取规则，每批重新读取以接收扫描期间的设置变更。
func (b *localMediaWriteBatch) cleanQuery(raw string) (string, int) {
	if b.recognitionUses == 0 {
		cfg := recognitionWordsConfig(b.ctx, b.scanner.repo)
		b.recognitionEnabled = cfg.Enabled
		b.recognitionRules = nil
		if cfg.Enabled {
			b.recognitionRules = parseRecognitionWordRules(recognitionWordsCombinedText(cfg))
		}
	}
	b.recognitionUses = (b.recognitionUses + 1) % b.limit
	if b.recognitionEnabled {
		raw = applyRecognitionWordRules(raw, b.recognitionRules)
	}
	return CleanQuery(raw)
}

func (b *localMediaWriteBatch) Add(path string, media *model.Media) {
	b.AddWithReason(path, media, "")
}

func (b *localMediaWriteBatch) AddWithReason(path string, media *model.Media, updateReason string) {
	if b == nil || b.scanner == nil || media == nil {
		return
	}
	if media.ScrapeStatus == "" {
		media.ScrapeStatus = "pending"
	}
	if media.ScrapeTrigger == "" {
		media.ScrapeTrigger = TaskTriggerEvent
	}
	b.items = append(b.items, localMediaWriteItem{path: path, media: media, updateReason: updateReason})
	if len(b.items) >= b.limit {
		b.Flush()
	}
}

func (b *localMediaWriteBatch) Flush() {
	if b == nil {
		return
	}
	// 更新文件不进入新增缓冲，收尾时仍须刷新其已提交的索引变更。
	defer b.flushSearch(b.ctx)
	b.recognitionUses = 0
	b.nfoCache = nil
	if len(b.items) == 0 || b.scanner == nil || b.scanner.repo == nil || b.scanner.repo.DB == nil {
		return
	}
	items := b.items
	b.items = nil
	existingPaths := b.existingPaths(items)
	createItems := make([]localMediaWriteItem, 0, len(items))
	for _, item := range items {
		if item.media == nil {
			continue
		}
		if existingPaths[filepath.Clean(item.media.Path)] {
			b.upsertExistingItem(item)
			continue
		}
		createItems = append(createItems, item)
	}
	if len(createItems) == 0 {
		b.publish()
		return
	}
	for _, item := range createItems {
		if item.media == nil {
			continue
		}
		wasExisting := b.mediaPathExists(item.media.Path)
		if err := b.scanner.invalidateChangedMediaProbe(b.ctx, item.path, item.updateReason); err != nil {
			addScanError(b.res, item.path, err)
			b.scanner.log.Warn("invalidate changed media probe failed", zap.String("path", item.path), zap.Error(err))
			continue
		}
		if err := b.scanner.upsertLocalScanMedia(b.ctx, item.media, b.mediaRepo); err != nil {
			addScanError(b.res, item.path, err)
			b.scanner.log.Warn("upsert media failed", zap.String("path", item.path), zap.Error(err))
			continue
		}
		b.res.addProbeMedia(item.media)
		if wasExisting {
			b.res.Updated++
			b.res.addChange(ScanChangeUpdated, item.path, item.updateReason)
		} else {
			b.res.Added++
			b.res.addChange(ScanChangeAdded, item.path, "")
		}
	}
	b.publish()
}

func (b *localMediaWriteBatch) existingPaths(items []localMediaWriteItem) map[string]bool {
	out := map[string]bool{}
	if b == nil || b.scanner == nil || b.scanner.repo == nil || b.scanner.repo.DB == nil || len(items) == 0 {
		return out
	}
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if item.media == nil || item.media.Path == "" {
			continue
		}
		paths = append(paths, item.media.Path)
	}
	if len(paths) == 0 {
		return out
	}
	var rows []string
	if err := b.scanner.repo.DB.WithContext(b.ctx).
		Model(&model.Media{}).
		Where("path IN ?", paths).
		Pluck("path", &rows).Error; err != nil {
		b.scanner.log.Debug("load existing media paths for scan batch failed", zap.Error(err))
		return out
	}
	for _, path := range rows {
		out[filepath.Clean(path)] = true
	}
	return out
}

func (b *localMediaWriteBatch) upsertExistingItem(item localMediaWriteItem) {
	if item.media == nil {
		return
	}
	if err := b.scanner.invalidateChangedMediaProbe(b.ctx, item.path, item.updateReason); err != nil {
		addScanError(b.res, item.path, err)
		b.scanner.log.Warn("invalidate changed media probe failed", zap.String("path", item.path), zap.Error(err))
		return
	}
	if err := b.scanner.upsertLocalScanMedia(b.ctx, item.media, b.mediaRepo); err != nil {
		addScanError(b.res, item.path, err)
		b.scanner.log.Warn("upsert media failed", zap.String("path", item.path), zap.Error(err))
		return
	}
	b.res.addProbeMedia(item.media)
	b.res.Updated++
	b.res.addChange(ScanChangeUpdated, item.path, item.updateReason)
}

func (b *localMediaWriteBatch) mediaPathExists(path string) bool {
	if b == nil || b.scanner == nil || b.scanner.repo == nil || b.scanner.repo.DB == nil || path == "" {
		return false
	}
	var count int64
	err := b.scanner.repo.DB.WithContext(b.ctx).
		Model(&model.Media{}).
		Where("path = ?", path).
		Count(&count).Error
	return err == nil && count > 0
}

func (b *localMediaWriteBatch) publish() {
	if b == nil || b.scanner == nil || b.scanner.hub == nil || b.res == nil {
		return
	}
	b.scanner.hub.Publish("scan", map[string]any{
		"library_id": b.res.LibraryID,
		"visited":    b.res.Visited,
		"added":      b.res.Added,
		"updated":    b.res.Updated,
		"probed":     b.res.Probed,
		"local_meta": b.res.LocalMetadata,
		"batched":    true,
	})
}
