package service

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ListMedia paginates media items inside a library.
func (s *MediaService) ListMedia(ctx context.Context, libraryID string, page, pageSize int) ([]model.MediaView, int64, error) {
	return s.ListMediaVisible(ctx, libraryID, page, pageSize, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) ListMediaVisible(ctx context.Context, libraryID string, page, pageSize int, visibility MediaVisibility) ([]model.MediaView, int64, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 2000 {
		pageSize = 2000
	}
	if page < 1 {
		page = 1
	}
	libraryIDs := []string{libraryID}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	cacheKey := s.mediaListCacheKey(libraryID, libraryIDs, page, pageSize, filter)
	var cached mediaListCacheValue
	if s.cache != nil && s.cache.GetJSON(ctx, cacheKey, &cached) {
		s.attachLibraryMetadataViews(ctx, cached.Items)
		return cached.Items, cached.Total, nil
	}
	items, total, err := s.repo.MediaView.ListByLibrariesFiltered(ctx, libraryIDs, (page-1)*pageSize, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadataViews(ctx, items)
	if s.cache != nil {
		s.cache.SetJSON(ctx, cacheKey, mediaListCacheValue{Items: items, Total: total}, time.Duration(s.mediaCacheTTLSeconds())*time.Second)
	}
	return items, total, nil
}

func (s *MediaService) ListMediaVisibleGrouped(ctx context.Context, libraryID string, page, pageSize int, visibility MediaVisibility) ([]MediaItem, int64, error) {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	items, err := s.listMediaVisibleForGrouping(ctx, libraryID, visibility)
	if err != nil {
		return nil, 0, err
	}
	grouped := groupMediaVersions(mediaViewsAsMedia(items))
	return paginateMediaItems(grouped, page, pageSize), int64(len(grouped)), nil
}

func (s *MediaService) listMediaVisibleForGrouping(ctx context.Context, libraryID string, visibility MediaVisibility) ([]model.MediaView, error) {
	libraryIDs := []string{libraryID}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	cacheKey := s.mediaListCacheKey(libraryID, libraryIDs, 0, maxMediaSearchLimit, filter) + ":group-source"
	var cached mediaListCacheValue
	if s.cache != nil && s.cache.GetJSON(ctx, cacheKey, &cached) {
		s.attachLibraryMetadataViews(ctx, cached.Items)
		return cached.Items, nil
	}
	items, total, err := s.repo.MediaView.ListByLibrariesFiltered(ctx, libraryIDs, 0, maxMediaSearchLimit, filter)
	if err != nil {
		return nil, err
	}
	if total > int64(len(items)) && s.log != nil {
		s.log.Warn("media version grouping truncated by safety limit",
			zap.String("library_id", libraryID),
			zap.Int64("total", total),
			zap.Int("limit", maxMediaSearchLimit))
	}
	s.attachLibraryMetadataViews(ctx, items)
	if s.cache != nil {
		s.cache.SetJSON(ctx, cacheKey, mediaListCacheValue{Items: items, Total: total}, time.Duration(s.mediaCacheTTLSeconds())*time.Second)
	}
	return items, nil
}

// GetMedia 返回包含共享元数据的统一媒体视图。
func (s *MediaService) GetMedia(ctx context.Context, id string) (*model.MediaView, error) {
	return s.getMedia(ctx, id, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) GetMediaVisible(ctx context.Context, id string, visibility MediaVisibility) (*model.MediaView, error) {
	return s.getMedia(ctx, id, visibility)
}

func (s *MediaService) getMedia(ctx context.Context, id string, visibility MediaVisibility) (*model.MediaView, error) {
	media, err := s.repo.MediaView.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if media != nil && !visibility.AllowsView(media) {
		return nil, nil
	}
	if media == nil {
		media, err = s.repo.MediaView.FindByLogicalMetadataID(ctx, id, repository.MediaQueryFilter{
			IncludeNSFW:       visibility.IncludeNSFW,
			AllowedLibraryIDs: visibility.AllowedLibraryIDs,
			HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
		})
		if err != nil || media == nil {
			return media, err
		}
	}
	items := []model.MediaView{*media}
	s.attachLibraryMetadataViews(ctx, items)
	*media = items[0]
	if s.probe != nil {
		if doc, ok := s.probe.Load(ctx, media.ID); ok {
			media.Tracks = projectProbeTracks(doc)
		}
	}
	return media, nil
}

// ListMediaVersions 返回当前用户可见的同作品媒体版本。
func (s *MediaService) ListMediaVersions(ctx context.Context, id, userID string, visibility MediaVisibility) ([]model.MediaView, error) {
	media, err := s.repo.MediaView.FindByID(ctx, id)
	if err != nil {
		return []model.MediaView{}, err
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	var items []model.MediaView
	if media == nil {
		items, err = s.repo.MediaView.FindByLogicalMetadataIDs(ctx, []string{id}, filter)
	} else if visibility.AllowsView(media) {
		items, err = s.repo.MediaView.FindByMetadataID(ctx, media.MetadataID, filter)
	}
	if err != nil {
		return nil, err
	}
	s.attachLibraryMetadataViews(ctx, items)
	sort.SliceStable(items, func(i, j int) bool {
		return preferMediaVersion(items[i].Media, items[j].Media)
	})
	if userID != "" && len(items) > 1 {
		mediaIDs := make([]string, 0, len(items))
		for i := range items {
			mediaIDs = append(mediaIDs, items[i].ID)
		}
		var history model.PlaybackHistory
		if err := s.repo.DB.WithContext(ctx).
			Where("user_id = ? AND media_id IN ?", userID, mediaIDs).
			Order("watched_at DESC").Limit(1).Find(&history).Error; err == nil && history.MediaID != "" {
			for i := range items {
				if items[i].ID == history.MediaID {
					items[0], items[i] = items[i], items[0]
					break
				}
			}
		}
	}
	return items, nil
}

// GetRawMedia 仅供扫描、文件打开和播放内部读取文件事实。
func (s *MediaService) GetRawMedia(ctx context.Context, id string) (*model.Media, error) {
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil || media == nil {
		return media, err
	}
	items := []model.Media{*media}
	s.attachLibraryMetadata(ctx, items)
	*media = items[0]
	return media, nil
}
