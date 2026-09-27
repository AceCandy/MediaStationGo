package repository

import (
	"context"
	"slices"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ListRecentByLibraries 为统计面板选取每部近期作品的一个可见文件，不改变常规列表排序。
func (r *MediaViewRepository) ListRecentByLibraries(ctx context.Context, libraryIDs []string, limit int, filter MediaQueryFilter) ([]model.MediaView, error) {
	allowed := make([]string, 0, len(libraryIDs))
	for _, id := range libraryIDs {
		if len(filter.AllowedLibraryIDs) == 0 || slices.Contains(filter.AllowedLibraryIDs, id) {
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		return []model.MediaView{}, nil
	}
	filter.AllowedLibraryIDs = allowed
	rows, err := r.ListRecentLogicalWorks(ctx, limit, filter)
	if err != nil {
		return nil, err
	}
	identity := func(row model.MediaView) string {
		if row.SeriesID != "" {
			return row.SeriesID
		}
		if row.CatalogItemID != "" {
			return row.CatalogItemID
		}
		return row.MetadataID
	}
	seen := make(map[string]bool, len(rows))
	result := make([]model.MediaView, 0, limit)
	for _, row := range rows {
		id := identity(row)
		if !seen[id] {
			seen[id] = true
			result = append(result, row)
		}
	}
	return result, nil
}
