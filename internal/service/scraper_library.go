package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// lookup runs the provider chain after local NFO has been considered:
// TMDb -> Douban -> Bangumi -> TheTVDB. Douban and Bangumi do not require API
// keys; providers that are unavailable or return an error are skipped.
type providerLookupResult struct {
	Match *Match
	Err   error
	Tried bool
}

func (s *ScraperService) lookup(ctx context.Context, lib *model.Library, media *model.Media, query string, year int) *Match {
	return s.lookupWithOutcome(ctx, lib, media, query, year).Match
}

func (s *ScraperService) lookupWithOutcome(ctx context.Context, lib *model.Library, media *model.Media, query string, year int) providerLookupResult {
	kind := ""
	if lib != nil {
		kind = lib.Type
	}
	// An explicit movie lookup is used to repair filenames whose release year
	// was previously polluted into S01E20x. Do not let the dirty path override
	// that caller-supplied media type; TV/anime libraries still force TV below.
	explicitEpisode := media != nil && (media.SeasonNum > 0 || media.EpisodeNum > 0)
	if (normalizeOrganizeMediaType(kind) != "movie" || explicitEpisode) && mediaIsEpisodic(media, lib) {
		kind = "tv"
	}
	result := providerLookupResult{}
	var lookupErrors []error
	if s.tmdb != nil && s.tmdb.Enabled() {
		result.Tried = true
		if match, err := s.lookupAutomaticTMDbWithError(ctx, kind, query, year); match != nil {
			match.Source = "tmdb"
			result.Match = match
			return result
		} else if err != nil {
			lookupErrors = append(lookupErrors, fmt.Errorf("tmdb: %w", err))
		}
	}
	if s.douban != nil && s.douban.Enabled() {
		result.Tried = true
		if m, err := s.douban.SearchMatch(ctx, query); err == nil && m != nil && metadataMatchCompatibleWithType(kind, m) {
			m.Source = "douban"
			result.Match = m
			return result
		} else if err != nil {
			s.log.Debug("douban search failed", zap.String("query", query), zap.Error(err))
			lookupErrors = append(lookupErrors, fmt.Errorf("douban: %w", err))
		}
	}
	if s.bangumi != nil && s.bangumi.Enabled() {
		result.Tried = true
		if m, err := s.bangumi.Search(ctx, query); err == nil && m != nil && metadataMatchCompatibleWithType(kind, m) {
			m.Source = "bangumi"
			result.Match = m
			return result
		} else if err != nil {
			s.log.Debug("bangumi search failed", zap.String("query", query), zap.Error(err))
			lookupErrors = append(lookupErrors, fmt.Errorf("bangumi: %w", err))
		}
	}
	if (kind == "anime" || kind == "tv" || kind == "variety" || kind == "show" || kind == "shows") && s.thetvdb != nil && s.thetvdb.Enabled() {
		result.Tried = true
		if m, err := s.thetvdb.SearchSeries(ctx, query); err == nil && m != nil && metadataMatchCompatibleWithType(kind, m) {
			m.Source = "thetvdb"
			result.Match = m
			return result
		} else if err != nil {
			s.log.Debug("thetvdb search failed", zap.String("query", query), zap.Error(err))
			lookupErrors = append(lookupErrors, fmt.Errorf("thetvdb: %w", err))
		}
	}
	result.Err = errors.Join(lookupErrors...)
	return result
}

func isTVMetadataKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "anime", "tv", "variety", "show", "shows":
		return true
	default:
		return false
	}
}

// EnrichLibrary runs the provider chain for every pending media in a library.
// When retryNoMatch is true it also retries rows previously marked no_match,
// which is the expected behaviour for a manual "重新刮削" action. Scanner-driven
// automatic enrichment keeps the default false path to avoid repeated scraping.
//
// Pending status includes both the canonical "pending" string and the
// empty / NULL values, because MediaRepository.Upsert can wipe the GORM
// default when re-running a scan over an already-existing row.
func (s *ScraperService) EnrichLibrary(ctx context.Context, libraryID string, retryNoMatch ...bool) (int, error) {
	result, err := s.EnrichLibraryDetailed(ctx, libraryID, retryNoMatch...)
	return result.Matched, err
}

type EnrichLibraryResult struct {
	LibraryID  string
	Matched    int
	Processed  int
	Failed     int
	Candidates int
}

type scrapeCandidateGroup struct {
	MetadataID     string
	Representative model.Media
	MediaIDs       []string
}

func (s *ScraperService) EnrichLibraryDetailed(ctx context.Context, libraryID string, retryNoMatch ...bool) (EnrichLibraryResult, error) {
	options := ScrapeOptions{}
	if len(retryNoMatch) > 0 {
		options.RetryNoMatch = retryNoMatch[0]
	}
	return s.EnrichLibraryDetailedWithOptions(ctx, libraryID, options)
}

func (s *ScraperService) EnrichLibraryDetailedWithOptions(ctx context.Context, libraryID string, options ScrapeOptions) (EnrichLibraryResult, error) {
	s.scrapeRunMu.Lock()
	defer s.scrapeRunMu.Unlock()
	return s.enrichLibraryDetailedWithOptions(ctx, libraryID, options)
}

func (s *ScraperService) enrichLibraryDetailedWithOptions(ctx context.Context, libraryID string, options ScrapeOptions) (EnrichLibraryResult, error) {
	result := EnrichLibraryResult{LibraryID: libraryID}
	rows, err := s.scrapeCandidateRows(ctx, libraryID, options)
	if err != nil {
		return result, err
	}
	groups, err := groupScrapeCandidateRows(rows)
	if err != nil {
		return result, err
	}
	result.Candidates = len(groups)
	runOptions := options
	runOptions.DeferEpisodeDetails = true
	representatives := make([]model.Media, 0, len(groups))
	for i := range groups {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		representative := &groups[i].Representative
		enrichErr := s.enrichOneWithOptions(ctx, representative, runOptions)
		if err := s.syncScrapeCandidateGroup(ctx, groups[i]); err != nil {
			return result, err
		}
		representatives = append(representatives, *representative)
		if enrichErr != nil {
			s.log.Warn("enrich failed", zap.String("media", representative.ID), zap.Error(enrichErr))
			s.notifyScrapeFailed(*representative, enrichErr)
			result.Failed++
			continue
		}
		result.Processed++
		if s.mediaIsMatched(ctx, representative.ID) {
			result.Matched++
		}
		if i < len(groups)-1 {
			if delay := s.scrapeDelay(ctx); delay > 0 {
				select {
				case <-ctx.Done():
					return result, ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
	if err := s.enrichDeferredEpisodeDetails(ctx, representatives, options); err != nil {
		return result, err
	}
	s.hub.Publish("scrape", map[string]any{
		"library_id": libraryID,
		"finished":   true,
		"matched":    result.Matched,
		"processed":  result.Processed,
		"failed":     result.Failed,
		"candidates": result.Candidates,
	})
	return result, nil
}

func groupScrapeCandidateRows(rows []model.Media) ([]scrapeCandidateGroup, error) {
	groups := make([]scrapeCandidateGroup, 0, len(rows))
	groupIndexes := make(map[string]int, len(rows))
	for i := range rows {
		metadataID := strings.TrimSpace(rows[i].MetadataID)
		seriesID := strings.TrimSpace(rows[i].SeriesID)
		groupKey := ""
		if seriesID != "" {
			groupKey = "series:" + seriesID
		} else if metadataID != "" {
			groupKey = metadataID
		} else {
			groupKey = "media:" + rows[i].ID
		}
		if groupIndex, ok := groupIndexes[groupKey]; ok {
			groups[groupIndex].MediaIDs = append(groups[groupIndex].MediaIDs, rows[i].ID)
			continue
		}
		groupIndexes[groupKey] = len(groups)
		groups = append(groups, scrapeCandidateGroup{
			MetadataID:     metadataID,
			Representative: rows[i],
			MediaIDs:       []string{rows[i].ID},
		})
	}
	return groups, nil
}

func (s *ScraperService) syncScrapeCandidateGroup(ctx context.Context, group scrapeCandidateGroup) error {
	fresh, err := s.repo.Media.FindByID(ctx, group.Representative.ID)
	if err != nil {
		return err
	}
	if fresh == nil {
		return fmt.Errorf("representative media %s not found after scrape", group.Representative.ID)
	}
	metadataIDs := make([]string, 0, 2)
	if group.MetadataID != "" {
		metadataIDs = append(metadataIDs, group.MetadataID)
	}
	if fresh.MetadataID != "" && fresh.MetadataID != group.MetadataID {
		metadataIDs = append(metadataIDs, fresh.MetadataID)
	}
	mediaIDs := make([]string, 0, len(group.MediaIDs))
	mediaQuery := s.repo.DB.WithContext(ctx).Model(&model.Media{})
	if len(metadataIDs) > 0 {
		mediaQuery = mediaQuery.Where("metadata_id IN ? OR id IN ?", metadataIDs, group.MediaIDs)
	} else {
		mediaQuery = mediaQuery.Where("id IN ?", group.MediaIDs)
	}
	if err := mediaQuery.Pluck("id", &mediaIDs).Error; err != nil {
		return err
	}
	updates := map[string]any{
		"scrape_status":       fresh.ScrapeStatus,
		"scrape_error":        fresh.ScrapeError,
		"local_metadata_hint": fresh.LocalMetadataHint,
		"lookup_tmdb_id":      fresh.TMDbID,
		"lookup_bangumi_id":   fresh.BangumiID,
		"lookup_douban_id":    fresh.DoubanID,
		"lookup_thetvdb_id":   fresh.TheTVDBID,
	}
	if fresh.MetadataID != "" {
		updates["metadata_id"] = fresh.MetadataID
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("id IN ?", mediaIDs).Updates(updates).Error; err != nil {
		return err
	}
	s.repo.MediaView.RefreshMetadataIDs(ctx, metadataIDs...)
	return nil
}

func (s *ScraperService) scrapeCandidateRows(ctx context.Context, libraryID string, options ScrapeOptions) ([]model.Media, error) {
	var rows []model.Media
	libraryIDs := []string{}
	if strings.TrimSpace(libraryID) != "" {
		libraryIDs = []string{strings.TrimSpace(libraryID)}
	}
	statusFilter := "scrape_status IS NULL OR scrape_status = '' OR scrape_status = ? OR scrape_status = ?"
	statusArgs := []any{"pending", "error"}
	if options.RetryNoMatch {
		statusFilter += " OR scrape_status = ?"
		statusArgs = append(statusArgs, "no_match")
	}
	if options.IncludeMatched || options.RefreshWeakMatched {
		statusFilter += " OR scrape_status = ?"
		statusArgs = append(statusArgs, "matched")
	}
	q := s.repo.DB.WithContext(ctx).Where(statusFilter, statusArgs...)
	if len(libraryIDs) > 0 {
		q = q.Where("library_id IN ?", libraryIDs)
	}
	if err := q.
		Order("CASE WHEN COALESCE(season_num, 0) > 0 OR COALESCE(episode_num, 0) > 0 THEN 1 ELSE 0 END").
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if options.RefreshWeakMatched && !options.IncludeMatched {
		rows = filterWeakMatchedScrapeRows(rows)
	}
	return rows, nil
}

func filterWeakMatchedScrapeRows(rows []model.Media) []model.Media {
	out := rows[:0]
	for _, row := range rows {
		if shouldScrapeCandidateRow(row) {
			out = append(out, row)
		}
	}
	return out
}

func shouldScrapeCandidateRow(media model.Media) bool {
	if strings.TrimSpace(media.ScrapeStatus) != "matched" {
		return true
	}
	return organizeMediaTitleLooksLikeRelease(media.Title)
}

func (s *ScraperService) notifyScrapeFailed(m model.Media, err error) {
	if s == nil || s.notify == nil || err == nil {
		return
	}
	body := strings.TrimSpace(m.Title)
	if body == "" {
		body = m.Path
	}
	body = "媒体：" + body + "\n错误：" + err.Error()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.notify.Broadcast(ctx, "MediaStationGo 刮削失败", body, EventScrapeFailed)
	}()
}

func (s *ScraperService) scrapeDelay(ctx context.Context) time.Duration {
	minMS := s.scrapeDelaySetting(ctx, "scrape.delay_min_ms", defaultScrapeDelayMinMS)
	maxMS := s.scrapeDelaySetting(ctx, "scrape.delay_max_ms", defaultScrapeDelayMaxMS)
	if minMS < 0 {
		minMS = 0
	}
	if maxMS < 0 {
		maxMS = 0
	}
	if minMS > maxScrapeDelayMS {
		minMS = maxScrapeDelayMS
	}
	if maxMS > maxScrapeDelayMS {
		maxMS = maxScrapeDelayMS
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	if maxMS == 0 {
		return 0
	}
	if maxMS == minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+secureRandomIntn(maxMS-minMS+1)) * time.Millisecond
}

func (s *ScraperService) scrapeDelaySetting(ctx context.Context, key string, fallback int) int {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return fallback
	}
	value, err := s.repo.Setting.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return fallback
	}
	return parseIntSettingDefault(strings.TrimSpace(value), fallback)
}

func (s *ScraperService) mediaIsMatched(ctx context.Context, mediaID string) bool {
	var status string
	err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Select("scrape_status").
		Where("id = ?", mediaID).
		Scan(&status).Error
	return err == nil && status == "matched"
}
