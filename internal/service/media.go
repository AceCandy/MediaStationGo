// Package service — library / media bookkeeping.
package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// MediaService offers high-level CRUD over libraries and media items.
type MediaService struct {
	cfg   *config.Config
	log   *zap.Logger
	repo  *repository.Container
	cache *RuntimeCacheService
}

type MediaVisibility struct {
	IncludeNSFW       bool
	AllowedLibraryIDs []string
	HiddenLibraryIDs  []string
}

type MediaItem struct {
	model.Media
	Versions []model.Media `json:"versions,omitempty"`
}

const maxMediaSearchLimit = 50000
const maxMediaSearchPageSize = 2000

func (v MediaVisibility) Allows(media *model.Media) bool {
	if media == nil {
		return false
	}
	if !v.IncludeNSFW && media.NSFW {
		return false
	}
	for _, id := range v.HiddenLibraryIDs {
		if id == media.LibraryID {
			return false
		}
	}
	if len(v.AllowedLibraryIDs) == 0 {
		return true
	}
	for _, id := range v.AllowedLibraryIDs {
		if id == media.LibraryID {
			return true
		}
	}
	return false
}

// NewMediaService is the constructor.
func NewMediaService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *MediaService {
	return &MediaService{cfg: cfg, log: log, repo: repo}
}

func (s *MediaService) SetRuntimeCache(cache *RuntimeCacheService) *MediaService {
	if s != nil {
		s.cache = cache
	}
	return s
}

// CreateLibrary persists a library after validating that its path exists.
func (s *MediaService) CreateLibrary(ctx context.Context, name, path, kind string) (*model.Library, error) {
	if name == "" || path == "" {
		return nil, errors.New("name and path required")
	}
	abs, err := resolveAccessibleLibraryPath(path)
	if err != nil {
		return nil, err
	}
	kind = inferLibraryKind(name, abs, kind)
	lib := &model.Library{Name: name, Path: abs, Type: kind, Enabled: true}
	if err := s.repo.Library.Create(ctx, lib); err != nil {
		return nil, err
	}
	s.invalidateMediaCache(ctx)
	return lib, nil
}

// ListLibraries returns every library configured on the server.
func (s *MediaService) ListLibraries(ctx context.Context) ([]model.Library, error) {
	return s.repo.Library.List(ctx)
}

// DeleteLibrary removes a library and its media rows. The on-disk files are
// left untouched.
func (s *MediaService) DeleteLibrary(ctx context.Context, id string) error {
	lib, err := s.repo.Library.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if lib != nil {
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			if err := s.repo.Media.PurgeByLibrary(ctx, id); err != nil {
				return err
			}
			err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Library{}).Error
			if err == nil {
				s.invalidateMediaCache(ctx)
			}
			return err
		}
	}
	if err := s.repo.Media.DeleteByLibrary(ctx, id); err != nil {
		return err
	}
	err = s.repo.Library.Delete(ctx, id)
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}

// ListMedia paginates media items inside a library.
func (s *MediaService) ListMedia(ctx context.Context, libraryID string, page, pageSize int) ([]model.Media, int64, error) {
	return s.ListMediaVisible(ctx, libraryID, page, pageSize, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) ListMediaVisible(ctx context.Context, libraryID string, page, pageSize int, visibility MediaVisibility) ([]model.Media, int64, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 2000 {
		pageSize = 2000
	}
	if page < 1 {
		page = 1
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	libraryIDs, err := MergedLibraryIDsForLibrary(ctx, s.repo, libraryID)
	if err != nil {
		return nil, 0, err
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	cacheKey := s.mediaListCacheKey(libraryID, libraryIDs, page, pageSize, filter)
	var cached mediaListCacheValue
	if s.cache != nil && s.cache.GetJSON(ctx, cacheKey, &cached) {
		s.attachLibraryMetadata(ctx, cached.Items)
		return cached.Items, cached.Total, nil
	}
	items, total, err := s.repo.Media.ListByLibrariesFiltered(ctx, libraryIDs, (page-1)*pageSize, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadata(ctx, items)
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
	grouped := groupMediaVersions(items)
	return paginateMediaItems(grouped, page, pageSize), int64(len(grouped)), nil
}

type mediaListCacheValue struct {
	Items []model.Media `json:"items"`
	Total int64         `json:"total"`
}

func (s *MediaService) mediaListCacheKey(libraryID string, libraryIDs []string, page, pageSize int, filter repository.MediaQueryFilter) string {
	allowed := append([]string(nil), filter.AllowedLibraryIDs...)
	hidden := append([]string(nil), filter.HiddenLibraryIDs...)
	libs := append([]string(nil), libraryIDs...)
	sort.Strings(allowed)
	sort.Strings(hidden)
	sort.Strings(libs)
	sum := sha1.Sum([]byte(strings.Join([]string{
		libraryID,
		strings.Join(libs, ","),
		fmt.Sprintf("%d:%d:%t", page, pageSize, filter.IncludeNSFW),
		strings.Join(allowed, ","),
		strings.Join(hidden, ","),
	}, "|")))
	return "media:list:" + hex.EncodeToString(sum[:])
}

func (s *MediaService) mediaCacheTTLSeconds() int {
	if s == nil || s.cfg == nil || s.cfg.Cache.MediaTTLSeconds < 1 {
		return 15
	}
	return s.cfg.Cache.MediaTTLSeconds
}

func (s *MediaService) invalidateMediaCache(ctx context.Context) {
	if s != nil && s.cache != nil {
		s.cache.DeletePrefix(ctx, "media:")
		s.cache.DeletePrefix(ctx, "stats:")
	}
}

func (s *MediaService) listMediaVisibleForGrouping(ctx context.Context, libraryID string, visibility MediaVisibility) ([]model.Media, error) {
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	libraryIDs, err := MergedLibraryIDsForLibrary(ctx, s.repo, libraryID)
	if err != nil {
		return nil, err
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	cacheKey := s.mediaListCacheKey(libraryID, libraryIDs, 0, maxMediaSearchLimit, filter) + ":group-source"
	var cached mediaListCacheValue
	if s.cache != nil && s.cache.GetJSON(ctx, cacheKey, &cached) {
		s.attachLibraryMetadata(ctx, cached.Items)
		return cached.Items, nil
	}
	items, total, err := s.repo.Media.ListByLibrariesFiltered(ctx, libraryIDs, 0, maxMediaSearchLimit, filter)
	if err != nil {
		return nil, err
	}
	if total > int64(len(items)) && s.log != nil {
		s.log.Warn("media version grouping truncated by safety limit",
			zap.String("library_id", libraryID),
			zap.Int64("total", total),
			zap.Int("limit", maxMediaSearchLimit))
	}
	s.attachLibraryMetadata(ctx, items)
	if s.cache != nil {
		s.cache.SetJSON(ctx, cacheKey, mediaListCacheValue{Items: items, Total: total}, time.Duration(s.mediaCacheTTLSeconds())*time.Second)
	}
	return items, nil
}

func normalizeGroupedMediaPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > maxMediaSearchPageSize {
		pageSize = maxMediaSearchPageSize
	}
	return page, pageSize
}

func paginateMediaItems(items []MediaItem, page, pageSize int) []MediaItem {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	if len(items) == 0 {
		return nil
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []MediaItem{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func firstMediaItems(items []MediaItem, limit int) []MediaItem {
	if len(items) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	if limit > len(items) {
		limit = len(items)
	}
	return items[:limit]
}

func groupMediaVersions(items []model.Media) []MediaItem {
	if len(items) == 0 {
		return nil
	}
	type group struct {
		key     string
		primary model.Media
		rows    []model.Media
	}
	groups := make([]group, 0, len(items))
	byKey := make(map[string]int, len(items))
	for _, item := range items {
		key := mediaVersionGroupKey(item)
		if key == "" {
			groups = append(groups, group{primary: item, rows: []model.Media{item}})
			continue
		}
		if idx, ok := byKey[key]; ok {
			groups[idx].rows = append(groups[idx].rows, item)
			if betterMediaVersion(item, groups[idx].primary) {
				groups[idx].primary = item
			}
			continue
		}
		byKey[key] = len(groups)
		groups = append(groups, group{key: key, primary: item, rows: []model.Media{item}})
	}
	out := make([]MediaItem, 0, len(groups))
	for _, g := range groups {
		sort.SliceStable(g.rows, func(i, j int) bool {
			return betterMediaVersion(g.rows[i], g.rows[j])
		})
		item := MediaItem{Media: g.primary}
		if len(g.rows) > 1 {
			item.Versions = g.rows
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func mediaVersionGroupKey(m model.Media) string {
	if m.SeasonNum > 0 || m.EpisodeNum > 0 {
		switch {
		case m.TMDbID > 0:
			return fmt.Sprintf("episode:tmdb:%d:%d:%d", m.TMDbID, m.SeasonNum, m.EpisodeNum)
		case m.BangumiID > 0:
			return fmt.Sprintf("episode:bangumi:%d:%d:%d", m.BangumiID, m.SeasonNum, m.EpisodeNum)
		case strings.TrimSpace(m.DoubanID) != "":
			return fmt.Sprintf("episode:douban:%s:%d:%d", strings.ToLower(strings.TrimSpace(m.DoubanID)), m.SeasonNum, m.EpisodeNum)
		case strings.TrimSpace(m.TheTVDBID) != "":
			return fmt.Sprintf("episode:thetvdb:%s:%d:%d", strings.ToLower(strings.TrimSpace(m.TheTVDBID)), m.SeasonNum, m.EpisodeNum)
		}
		title := firstNonEmpty(m.OriginalName, m.Title)
		if title == "" {
			title, _ = CleanQuery(m.Path)
		}
		title, _ = mediaVersionTitleKey(title)
		if title == "" {
			return ""
		}
		return strings.Join([]string{
			"episode",
			strings.ToLower(strings.TrimSpace(m.LibraryID)),
			title,
			fmt.Sprintf("%d:%d", m.SeasonNum, m.EpisodeNum),
		}, "|")
	}
	switch {
	case m.TMDbID > 0:
		return fmt.Sprintf("tmdb:%d", m.TMDbID)
	case m.BangumiID > 0:
		return fmt.Sprintf("bangumi:%d", m.BangumiID)
	case strings.TrimSpace(m.DoubanID) != "":
		return "douban:" + strings.ToLower(strings.TrimSpace(m.DoubanID))
	case strings.TrimSpace(m.TheTVDBID) != "":
		return "thetvdb:" + strings.ToLower(strings.TrimSpace(m.TheTVDBID))
	}
	title := firstNonEmpty(m.OriginalName, m.Title)
	titleYear := 0
	if title == "" {
		title, _ = CleanQuery(m.Path)
	} else {
		title, titleYear = mediaVersionTitleKey(title)
	}
	if title == "" {
		return ""
	}
	year := m.Year
	if year <= 0 {
		year = titleYear
	}
	if year <= 0 {
		_, year = CleanQuery(m.Path)
	}
	return fmt.Sprintf("movie:%s:%d", title, year)
}

func mediaVersionTitleKey(value string) (string, int) {
	cleaned, year := CleanQuery(value)
	if strings.TrimSpace(cleaned) == "" {
		cleaned = value
	}
	return normalizeMediaVersionText(cleaned), year
}

func normalizeMediaVersionText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '.', '_', '-', ' ', '\t', '/', '\\', '[', ']', '(', ')', '（', '）', '【', '】':
			return true
		default:
			return false
		}
	})
	out := fields[:0]
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, noise := noiseTokenSet[field]; noise {
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

func betterMediaVersion(candidate, current model.Media) bool {
	candidateCloud := isCloudMediaVersion(candidate)
	currentCloud := isCloudMediaVersion(current)
	if candidateCloud != currentCloud {
		return !candidateCloud
	}
	candidatePixels := candidate.Width * candidate.Height
	currentPixels := current.Width * current.Height
	if candidatePixels != currentPixels {
		return candidatePixels > currentPixels
	}
	if candidate.SizeBytes != current.SizeBytes {
		return candidate.SizeBytes > current.SizeBytes
	}
	return candidate.CreatedAt.After(current.CreatedAt)
}

func isCloudMediaVersion(media model.Media) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(media.Path)), "cloud://") ||
		strings.Contains(strings.ToLower(strings.TrimSpace(media.STRMURL)), "/api/cloud/play/")
}

// SearchMedia performs a simple LIKE search across titles.
func (s *MediaService) SearchMedia(ctx context.Context, query string, limit int) ([]model.Media, error) {
	return s.SearchMediaVisible(ctx, query, limit, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) SearchMediaVisible(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]model.Media, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	items, err := s.repo.Media.SearchFiltered(ctx, query, limit, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil {
		return nil, err
	}
	s.attachLibraryMetadata(ctx, items)
	return items, nil
}

func (s *MediaService) SearchMediaVisibleGrouped(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]MediaItem, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	items, err := s.SearchMediaVisible(ctx, query, maxMediaSearchLimit, visibility)
	if err != nil {
		return nil, err
	}
	return firstMediaItems(groupMediaVersions(items), limit), nil
}

func (s *MediaService) SearchMediaVisiblePage(ctx context.Context, query string, page, pageSize int, visibility MediaVisibility) ([]model.Media, int64, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > maxMediaSearchPageSize {
		pageSize = maxMediaSearchPageSize
	}
	if page < 1 {
		page = 1
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	items, total, err := s.repo.Media.SearchFilteredPage(ctx, query, (page-1)*pageSize, pageSize, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadata(ctx, items)
	return items, total, nil
}

func (s *MediaService) SearchMediaVisiblePageGrouped(ctx context.Context, query string, page, pageSize int, visibility MediaVisibility) ([]MediaItem, int64, error) {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	items, err := s.SearchMediaVisible(ctx, query, maxMediaSearchLimit, visibility)
	if err != nil {
		return nil, 0, err
	}
	grouped := groupMediaVersions(items)
	return paginateMediaItems(grouped, page, pageSize), int64(len(grouped)), nil
}

// GetMedia returns a single media row.
func (s *MediaService) GetMedia(ctx context.Context, id string) (*model.Media, error) {
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil || media == nil {
		return media, err
	}
	items := []model.Media{*media}
	s.attachLibraryMetadata(ctx, items)
	*media = items[0]
	return media, nil
}

const maxRecycleBinRecords = 200

// SoftDelete moves a media row to the recycle bin (gorm soft delete).
// The on-disk file is kept; admins can purge it later.
func (s *MediaService) SoftDelete(ctx context.Context, id string) error {
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if media != nil && isCloudMediaPath(media.Path) {
		err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
		if err == nil {
			s.invalidateMediaCache(ctx)
		}
		return err
	}
	err = s.repo.DB.WithContext(ctx).Where("id = ?", id).Delete(&model.Media{}).Error
	if err == nil {
		if pruneErr := pruneRecycleBinRows(ctx, s.repo.DB, maxRecycleBinRecords); pruneErr != nil {
			return pruneErr
		}
		s.invalidateMediaCache(ctx)
	}
	return err
}

// RestoreDeleted unsets DeletedAt for a single media row.
func (s *MediaService) RestoreDeleted(ctx context.Context, id string) error {
	err := s.repo.DB.WithContext(ctx).Unscoped().Model(&model.Media{}).
		Where("id = ?", id).Update("deleted_at", nil).Error
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}

// ListRecycleBin returns every soft-deleted row, newest first.
func (s *MediaService) ListRecycleBin(ctx context.Context, limit int) ([]model.Media, error) {
	if err := pruneRecycleBinRows(ctx, s.repo.DB, maxRecycleBinRecords); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxRecycleBinRecords {
		limit = maxRecycleBinRecords
	}
	var rows []model.Media
	err := s.repo.DB.Unscoped().
		Where("deleted_at IS NOT NULL").
		Order("deleted_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func pruneRecycleBinRows(ctx context.Context, db *gorm.DB, keep int) error {
	if db == nil {
		return nil
	}
	if keep <= 0 {
		keep = maxRecycleBinRecords
	}
	var rows []struct {
		ID string
	}
	if err := db.WithContext(ctx).Unscoped().
		Model(&model.Media{}).
		Select("id").
		Where("deleted_at IS NOT NULL").
		Order("deleted_at desc").
		Limit(100000).
		Offset(keep).
		Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&model.Media{}).Error
}

// PurgeDeleted permanently removes a soft-deleted row from the database.
func (s *MediaService) PurgeDeleted(ctx context.Context, id string) error {
	err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}
