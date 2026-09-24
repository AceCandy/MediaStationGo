package service

import (
	"context"
	"strings"
	"sync"

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
	items, _, err := s.searchMediaPage(ctx, query, 0, limit, visibility)
	return items, err
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
	return s.searchMediaPage(ctx, query, (page-1)*pageSize, pageSize, visibility)
}

// searchMediaPage 按来源召回候选，统一排序分页后只加载当页可见代表文件。
func (s *MediaService) searchMediaPage(ctx context.Context, query string, offset, limit int, visibility MediaVisibility) ([]model.MediaView, int64, error) {
	filter := repository.MetadataSearchFilter{
		MediaQueryFilter: repository.MediaQueryFilter{
			IncludeNSFW:       visibility.IncludeNSFW,
			AllowedLibraryIDs: visibility.AllowedLibraryIDs,
			HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
		},
		Fields: repository.MetadataSearchFieldsWeb,
		Kinds:  []string{model.MetadataKindMovie, model.MetadataKindSeries},
	}
	var items []model.MediaView
	var total int64
	var err error
	if strings.TrimSpace(query) == "" {
		items, total, err = s.repo.MediaView.SearchFilteredPage(ctx, query, offset, limit, filter.MediaQueryFilter)
	} else {
		searchCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		var source []repository.MetadataSearchCandidate
		var ids []string
		var pending sync.WaitGroup
		pending.Go(func() {
			var err error
			source, err = s.repo.HongGuo.SearchCandidates(searchCtx, query, filter)
			if err != nil {
				cancel(err)
			}
		})
		pending.Go(func() {
			var err error
			// 无红果命中时沿用普通/NFO 的总数及分页，有红果时仍仅合并原有候选上限。
			ids, total, err = s.repo.MediaView.SearchMetadataIDs(searchCtx, query, 0, max(repository.MetadataSearchCandidateLimit, offset+limit), filter)
			if err != nil {
				cancel(err)
			}
		})
		pending.Wait()
		if err := context.Cause(searchCtx); err != nil {
			return nil, 0, err
		}
		if len(source) == 0 {
			start := min(max(offset, 0), len(ids))
			ids = ids[start:min(start+limit, len(ids))]
		} else {
			ids = ids[:min(len(ids), repository.MetadataSearchCandidateLimit)]
			candidates, searchErr := s.repo.MediaView.SearchCandidateDetails(ctx, ids)
			if searchErr != nil {
				return nil, 0, searchErr
			}
			ranked, count := repository.RankWebMetadataSearchCandidatePage(query, append(candidates, source...), offset, limit)
			total = count
			ids = make([]string, 0, len(ranked))
			for _, candidate := range ranked {
				ids = append(ids, candidate.ID)
			}
		}
		items, err = s.repo.MediaView.FindMetadataSearchRepresentatives(ctx, ids, filter.MediaQueryFilter)
	}
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
