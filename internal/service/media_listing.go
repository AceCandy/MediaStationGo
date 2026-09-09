package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

var ErrInvalidScrapeIssueStatus = errors.New("invalid scrape issue status")

const (
	providerStatusMissing  = "missing"
	providerStatusPartial  = "partial"
	providerStatusDegraded = "degraded"
	providerStatusComplete = "complete"
)

type MediaScrapeIssue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Path        string `json:"path"`
	Year        int    `json:"year"`
	SeasonNum   int    `json:"season_num"`
	EpisodeNum  int    `json:"episode_num"`
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	LibraryType string `json:"library_type"`
	Status      string `json:"scrape_status"`
	Reason      string `json:"reason"`
}

type MediaScrapeIssuePage struct {
	Items    []MediaScrapeIssue `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

// ListMedia paginates media items inside a library.
func (s *MediaService) ListMedia(ctx context.Context, libraryID string, page, pageSize int) ([]model.MediaView, int64, error) {
	return s.ListMediaVisible(ctx, libraryID, page, pageSize, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) ListScrapeIssues(ctx context.Context, libraryID, keyword string, statuses []string, page, pageSize int) (MediaScrapeIssuePage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if len(statuses) == 0 || len(statuses) == 1 && strings.TrimSpace(statuses[0]) == "" {
		statuses = []string{"error", "no_match"}
	}
	for i := range statuses {
		statuses[i] = strings.ToLower(strings.TrimSpace(statuses[i]))
		if statuses[i] != "error" && statuses[i] != "no_match" {
			return MediaScrapeIssuePage{}, ErrInvalidScrapeIssueStatus
		}
	}
	keyword = strings.TrimSpace(keyword)
	query := func() *gorm.DB {
		q := s.repo.DB.WithContext(ctx).Table("media AS m").
			Joins("JOIN libraries AS l ON l.id = m.library_id AND l.deleted_at IS NULL").
			Where("m.scrape_status IN ?", statuses)
		if strings.TrimSpace(libraryID) != "" {
			q = q.Where("m.library_id = ?", strings.TrimSpace(libraryID))
		}
		if keyword != "" {
			pattern := "%" + strings.ToLower(repository.EscapeLike(keyword)) + "%"
			q = q.Where(`LOWER(COALESCE(m.scan_title,'')) LIKE ? ESCAPE '\' OR LOWER(m.path) LIKE ? ESCAPE '\' OR LOWER(l.name) LIKE ? ESCAPE '\' OR LOWER(COALESCE(m.scrape_error,'')) LIKE ? ESCAPE '\'`, pattern, pattern, pattern, pattern)
		}
		return q
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return MediaScrapeIssuePage{}, err
	}
	items := make([]MediaScrapeIssue, 0)
	if err := query().Select(`m.id, m.scan_title AS title, m.path, m.scan_year AS year,
		m.season_num, m.episode_num, m.library_id, l.name AS library_name,
		l.type AS library_type, m.scrape_status AS status, m.scrape_error AS reason`).
		Order("m.updated_at DESC, m.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		return MediaScrapeIssuePage{}, err
	}
	for i := range items {
		items[i].Reason = scrapeIssueReason(items[i])
	}
	return MediaScrapeIssuePage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func scrapeIssueReason(issue MediaScrapeIssue) string {
	if reason := strings.TrimSpace(issue.Reason); reason != "" {
		return sanitizeTaskLogError(errors.New(reason)).Error()
	}
	if issue.Status == "no_match" {
		if issue.LibraryType == model.LibraryTypeNFOMovie || issue.LibraryType == model.LibraryTypeNFOTV {
			return "未找到本地 NFO"
		}
		return "未找到匹配元数据"
	}
	return "刮削失败，请重试"
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
		IncludeNSFW:         visibility.IncludeNSFW,
		AllowedLibraryIDs:   visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:    visibility.HiddenLibraryIDs,
		MissingPoster:       visibility.MissingPoster,
		MissingChineseTitle: visibility.MissingChineseTitle,
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
	if !visibility.allows(libraryID, false) {
		return []MediaItem{}, 0, nil
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:         visibility.IncludeNSFW,
		AllowedLibraryIDs:   visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:    visibility.HiddenLibraryIDs,
		MissingPoster:       visibility.MissingPoster,
		MissingChineseTitle: visibility.MissingChineseTitle,
	}
	items, summaries, total, err := s.repo.MediaView.ListLibraryMetadataPage(ctx, libraryID, model.MetadataKindMovie, "", (page-1)*pageSize, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadataViews(ctx, items)
	byID := make(map[string]model.Media, len(items))
	for _, item := range mediaViewsAsMedia(items) {
		byID[item.ID] = item
	}
	out := make([]MediaItem, 0, len(summaries))
	for _, summary := range summaries {
		if item, ok := byID[summary.MediaID]; ok {
			out = append(out, MediaItem{Media: item, VersionCount: summary.VersionCount})
		}
	}
	return out, total, nil
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
	if err := s.attachMediaProviderDetails(ctx, media); err != nil {
		return nil, err
	}
	if s.probe != nil {
		if doc, ok := s.probe.Load(ctx, media.ID); ok {
			media.Tracks = projectProbeTracks(doc)
		}
	}
	return media, nil
}

func (s *MediaService) attachMediaProviderDetails(ctx context.Context, media *model.MediaView) error {
	if media == nil || strings.TrimSpace(media.MetadataID) == "" {
		return nil
	}
	if media.MetadataKind == model.MetadataKindSeason || media.MetadataKind == model.MetadataKindEpisode {
		identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, media.SeriesID)
		if err != nil {
			return err
		}
		if externalID, ok := uniqueIdentifier(identifiers, "tmdb", model.MetadataKindSeries); ok {
			media.SeriesTMDbID, _ = strconv.Atoi(externalID)
		}
	}
	if media.TMDbID > 0 || (media.MetadataKind == model.MetadataKindEpisode && media.SeriesTMDbID > 0) {
		media.TMDbStatus = providerStatusMissing
		snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, media.MetadataID, "tmdb")
		if err != nil {
			return err
		}
		media.TMDbSnapshot = snapshot != nil
		if snapshot != nil {
			media.TMDbStatus = providerStatusPartial
			hasArtwork, err := s.repo.Artwork.HasProviderArtwork(ctx, media.MetadataID, providerArtworkType(media.MetadataKind), "tmdb")
			if err != nil {
				return err
			}
			if hasArtwork {
				media.TMDbStatus = providerStatusComplete
			}
		}
	}
	if media.DoubanID != "" {
		media.DoubanStatus = providerStatusMissing
		snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, media.MetadataID, "douban")
		if err != nil {
			return err
		}
		media.DoubanSnapshot = snapshot != nil
		if snapshot != nil {
			if snapshot.Degraded {
				media.DoubanStatus = providerStatusDegraded
			} else {
				media.DoubanStatus = providerStatusPartial
				hasArtwork, err := s.repo.Artwork.HasProviderArtwork(ctx, media.MetadataID, providerArtworkType(media.MetadataKind), "douban")
				if err != nil {
					return err
				}
				if hasArtwork && doubanSnapshotIsCurrent([]byte(snapshot.Payload)) {
					media.DoubanStatus = providerStatusComplete
				}
			}
		}
	}
	return nil
}

func providerArtworkType(metadataKind string) string {
	if metadataKind == model.MetadataKindEpisode {
		return model.ArtworkTypeStill
	}
	return model.ArtworkTypePoster
}

func doubanSnapshotIsCurrent(payload []byte) bool {
	var raw map[string]any
	if json.Unmarshal(payload, &raw) != nil || raw == nil {
		return false
	}
	if _, ok := raw["subject"].(map[string]any); ok {
		return false
	}
	if _, ok := raw["data"].(map[string]any); ok {
		return false
	}
	return true
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
	return s.repo.Media.FindByID(ctx, id)
}
