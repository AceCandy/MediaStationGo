package service

import (
	"context"
	"sync"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ItemCounts 按可见媒体库统计作品和文件；ItemCount 是电影与剧集作品总数。
// EpisodeCount 包含电影文件，不合并版本或分段。
func (e *EmbyService) ItemCounts(ctx context.Context, userID string) (map[string]any, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	visibility := e.mediaVisibility(ctx, userID)
	filter := repository.MediaQueryFilter{
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	if visibility.LibraryRestricted && len(filter.AllowedLibraryIDs) == 0 {
		filter.AllowedLibraryIDs = []string{"__locked__"}
	}
	db := e.repo.DB.WithContext(ctx)
	files := func() *gorm.DB {
		q := db.Table("media m")
		if len(filter.AllowedLibraryIDs) > 0 {
			q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
		}
		if len(filter.HiddenLibraryIDs) > 0 {
			q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
		}
		return q
	}
	works := func(table string, fallback *gorm.DB) *gorm.DB {
		q := db.Table(table + " w").Where("w.kind IN ('movie','series')")
		q = repository.FilterVisibleWorkLibraries(db, q, "w.library_ids", nil, filter)
		// 已维护的归属直接计数；历史 NULL 仅检查可见文件是否存在。
		return q.Where("(w.library_ids IS NOT NULL AND w.library_ids <> '[]'::jsonb) OR (w.library_ids IS NULL AND EXISTS (?))", fallback.Select("1"))
	}
	ordinaryFiles := files().Where(`m.metadata_id IN (
SELECT w.id UNION ALL SELECT id FROM metadata_items WHERE parent_id=w.id UNION ALL
SELECT ep.id FROM metadata_items season JOIN metadata_items ep ON ep.parent_id=season.id WHERE season.parent_id=w.id)`)
	nfoFiles := files().Joins("JOIN nfo_media_bindings b ON b.media_id=m.id").Where(`b.item_id IN (
SELECT w.id UNION ALL SELECT id FROM nfo_items WHERE parent_id=w.id UNION ALL
SELECT ep.id FROM nfo_items season JOIN nfo_items ep ON ep.parent_id=season.id WHERE season.parent_id=w.id)`)
	nfo := db.Table("nfo_items w").Where("w.kind IN ('movie','series')").
		Where("w.latest_media_added_at IS NOT NULL OR EXISTS (?)", nfoFiles.Select("1"))
	if len(filter.AllowedLibraryIDs) > 0 {
		nfo = nfo.Where("w.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		nfo = nfo.Where("w.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	queries := []*gorm.DB{
		works("metadata_items", ordinaryFiles),
		nfo,
		works("hongguo_works", files().Joins("JOIN hongguo_media_bindings b ON b.media_id=m.id").Where("b.work_id=w.id")),
		works("huangguoai_works", files().Joins("JOIN huangguoai_media_bindings b ON b.media_id=m.id").Where("b.work_id=w.id")),
	}
	type workCounts struct {
		Movies int64
		Series int64
	}
	results := make([]workCounts, len(queries))
	var pending sync.WaitGroup
	for i, query := range queries {
		pending.Go(func() {
			if err := query.Select("COUNT(*) FILTER (WHERE w.kind='movie') AS movies, COUNT(*) FILTER (WHERE w.kind='series') AS series").Scan(&results[i]).Error; err != nil {
				cancel(err)
			}
		})
	}
	var fileCount int64
	pending.Go(func() {
		if err := files().Count(&fileCount).Error; err != nil {
			cancel(err)
		}
	})
	pending.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	var movieCount, seriesCount int64
	for _, counts := range results {
		movieCount += counts.Movies
		seriesCount += counts.Series
	}
	return map[string]any{
		"MovieCount":   movieCount,
		"SeriesCount":  int(seriesCount),
		"EpisodeCount": fileCount,
		"ItemCount":    movieCount + seriesCount,
	}, nil
}
