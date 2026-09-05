package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SearchMedia performs a simple LIKE search across titles.
func (s *MediaService) SearchMedia(ctx context.Context, query string, limit int) ([]model.MediaView, error) {
	return s.SearchMediaVisible(ctx, query, limit, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) SearchMediaVisible(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]model.MediaView, error) {
	if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
		return []model.MediaView{}, nil
	}
	if limit <= 0 {
		limit = 50
	} else if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	items, err := s.repo.MediaView.SearchFiltered(ctx, query, limit, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil {
		return nil, err
	}
	s.attachLibraryMetadataViews(ctx, items)
	return items, nil
}

func (s *MediaService) SearchMediaVisibleGrouped(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]MediaItem, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	items, _, err := s.SearchMediaVisiblePageGrouped(ctx, query, 1, limit, visibility)
	return items, err
}

func (s *MediaService) SearchMediaVisiblePage(ctx context.Context, query string, page, pageSize int, visibility MediaVisibility) ([]model.MediaView, int64, error) {
	if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
		return []model.MediaView{}, 0, nil
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > maxMediaSearchPageSize {
		pageSize = maxMediaSearchPageSize
	}
	if page < 1 {
		page = 1
	}
	items, total, err := s.repo.MediaView.SearchFilteredPage(ctx, query, (page-1)*pageSize, pageSize, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadataViews(ctx, items)
	return items, total, nil
}

func (s *MediaService) SearchMediaVisiblePageGrouped(ctx context.Context, query string, page, pageSize int, visibility MediaVisibility) ([]MediaItem, int64, error) {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	items, total, err := s.SearchMediaVisiblePage(ctx, query, page, pageSize, visibility)
	if err != nil {
		return nil, 0, err
	}
	// 搜索已按 metadata 去重和排序，此处只转换响应，避免版本分组重排结果。
	rows := mediaViewsAsMedia(items)
	result := make([]MediaItem, len(rows))
	for i, row := range rows {
		result[i] = MediaItem{Media: row}
	}
	return result, total, nil
}
