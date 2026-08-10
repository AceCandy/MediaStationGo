// Package service — scraper orchestrator.
//
// ScraperService takes a Media row and tries to enrich it with metadata from
// local NFO first, then TMDb -> Douban -> Bangumi -> TheTVDB. Fanart.tv is
// artwork-only and upgrades poster/backdrop after a metadata match.
package service

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// EnrichOne runs the provider chain for a single media row.
func (s *ScraperService) EnrichOne(ctx context.Context, m *model.Media) error {
	return s.EnrichOneWithOptions(ctx, m, ScrapeOptions{})
}

func (s *ScraperService) EnrichOneWithOptions(ctx context.Context, m *model.Media, options ScrapeOptions) error {
	lib, err := s.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil {
		return err
	}

	seriesLike := mediaIsEpisodic(m, lib)
	var local *LocalMetadata
	var localErr error
	if isHTTPish(m.Path) {
		local, localErr = decodeLocalMetadataHint(m.LocalMetadataHint)
	} else {
		if found, err := ReadLocalMetadata(m.Path, lib.Path, seriesLike); err == nil && found != nil {
			local = found
		} else if err != nil {
			localErr = err
			s.log.Warn("read local metadata before scrape failed", zap.String("media_id", m.ID), zap.Error(err))
		}
	}
	if hinted, _ := pathHintMetadata(m.Path, seriesLike); hinted != nil {
		local = mergeScrapePathHintMetadata(local, hinted)
	}
	lookupMedia := *m
	if local != nil {
		applyLocalScanHints(&lookupMedia, local)
		applyLocalIdentityMetadata(&lookupMedia, local)
		applyLocalEpisodeMetadata(&lookupMedia, local)
		m.SeasonNum = lookupMedia.SeasonNum
		m.EpisodeNum = lookupMedia.EpisodeNum
	}
	if strings.TrimSpace(m.MetadataID) == "" {
		if err := s.repo.Media.ResolveMetadata(ctx, &lookupMedia); err != nil {
			return s.markScrapeError(ctx, m.ID, err)
		}
		if lookupMedia.MetadataID != "" {
			updates := map[string]any{
				"metadata_id":         lookupMedia.MetadataID,
				"scrape_status":       "matched",
				"scrape_error":        "",
				"local_metadata_hint": "",
				"series_hint":         lookupMedia.SeriesID,
				"lookup_tmdb_id":      lookupMedia.TMDbID,
				"lookup_bangumi_id":   lookupMedia.BangumiID,
				"lookup_douban_id":    lookupMedia.DoubanID,
				"lookup_thetvdb_id":   lookupMedia.TheTVDBID,
			}
			if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", m.ID).Updates(updates).Error; err != nil {
				return err
			}
			m.MetadataID = lookupMedia.MetadataID
			m.SeriesID = lookupMedia.SeriesID
			m.ScrapeStatus = "matched"
			s.repo.MediaView.ReindexMediaIDs(ctx, m.ID)
			s.invalidateMediaCache(ctx)
			return nil
		}
	}

	year := mediaYearHint(&lookupMedia)
	var lookupErrors []error

	externalResult := s.matchFromMediaExternalIDsWithOutcome(ctx, &lookupMedia, lib)
	if match := externalResult.Match; match != nil {
		mergeLocalCreditsIntoMatch(match, local)
		s.applyFanartArtwork(ctx, match)
		return s.applyProviderMatchWithOptions(ctx, m, lib, match, options)
	}
	if externalResult.Err != nil {
		lookupErrors = append(lookupErrors, externalResult.Err)
	}

	candidates := scrapeQueryCandidatesWithRecognition(ctx, s.repo, &lookupMedia, lib)
	var query string
	match := (*Match)(nil)
	for _, candidate := range candidates {
		query = candidate
		lookupResult := s.lookupWithOutcome(ctx, lib, &lookupMedia, candidate, year)
		if lookupResult.Err != nil {
			lookupErrors = append(lookupErrors, lookupResult.Err)
		}
		candidateMatch := lookupResult.Match
		if candidateMatch == nil {
			continue
		}
		if !organizeMetadataMatchTrusted(candidate, year, candidateMatch) {
			s.log.Warn("metadata scrape match rejected",
				zap.String("media_id", m.ID),
				zap.String("query", candidate),
				zap.String("title", candidateMatch.Title),
				zap.Int("source_year", year),
				zap.Int("match_year", candidateMatch.Year),
				zap.Int("tmdb_id", candidateMatch.TMDbID),
				zap.Int("bangumi_id", candidateMatch.BangumiID),
				zap.String("douban_id", candidateMatch.DoubanID),
				zap.String("thetvdb_id", candidateMatch.TheTVDBID))
			continue
		}
		preferLocalizedSearchTitle(candidate, candidateMatch)
		match = candidateMatch
		if match != nil {
			break
		}
	}
	if match == nil {
		if len(lookupErrors) > 0 {
			return s.markScrapeError(ctx, m.ID, errors.Join(lookupErrors...))
		}
		if localErr != nil {
			return s.markScrapeError(ctx, m.ID, localErr)
		}
		if localMetadataEligibleForFallback(local) {
			if strings.TrimSpace(local.Title) == "" {
				local.Title = strings.TrimSpace(m.Title)
			}
			return s.applyLocalMetadataMatch(ctx, m, local)
		}
		if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", m.ID).
			Updates(map[string]any{"scrape_status": "no_match", "scrape_error": "", "local_metadata_hint": ""}).Error; err != nil {
			return err
		}
		s.invalidateMediaCache(ctx)
		s.log.Info("metadata scrape no match",
			zap.String("media_id", m.ID),
			zap.String("query", query),
			zap.String("library_type", func() string {
				if lib != nil {
					return lib.Type
				}
				return ""
			}()))
		return nil
	}
	mergeLocalCreditsIntoMatch(match, local)
	s.applyFanartArtwork(ctx, match)

	return s.applyProviderMatchWithOptions(ctx, m, lib, match, options)
}

func mergeLocalCreditsIntoMatch(match *Match, local *LocalMetadata) {
	if match == nil || local == nil || len(local.Credits) == 0 {
		return
	}
	for _, typ := range match.LoadedCreditTypes {
		hasType := false
		for _, credit := range match.Credits {
			if credit.Type == typ {
				hasType = true
				break
			}
		}
		if hasType {
			continue
		}
		for _, credit := range local.Credits {
			if credit.Type == typ {
				match.Credits = append(match.Credits, credit)
			}
		}
	}
}

func localMetadataEligibleForFallback(local *LocalMetadata) bool {
	return local != nil && (local.HasNFO || local.HasArtwork || (!local.PathHint && localHasDescriptiveMetadata(local)))
}

func (s *ScraperService) applyProviderMatch(ctx context.Context, m *model.Media, lib *model.Library, match *Match) error {
	return s.applyProviderMatchWithOptions(ctx, m, lib, match, ScrapeOptions{})
}

func (s *ScraperService) applyProviderMatchWithOptions(ctx context.Context, m *model.Media, lib *model.Library, match *Match, options ScrapeOptions) error {
	persisted, err := s.persistProviderMetadata(ctx, m, lib, match)
	if err != nil {
		return s.markScrapeError(ctx, m.ID, err)
	}
	updates := map[string]any{
		"metadata_id":         persisted.Target.ID,
		"scrape_status":       "matched",
		"scrape_error":        "",
		"local_metadata_hint": "",
		"lookup_tmdb_id":      match.TMDbID,
		"lookup_bangumi_id":   match.BangumiID,
		"lookup_douban_id":    match.DoubanID,
		"lookup_thetvdb_id":   match.TheTVDBID,
	}
	if m.SeasonNum > 0 || m.EpisodeNum > 0 {
		updates["season_num"] = m.SeasonNum
	}
	if m.EpisodeNum > 0 {
		updates["episode_num"] = m.EpisodeNum
	}
	applyScrapeMediaTypeResets(updates, match)

	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", m.ID).
		Updates(updates).Error; err != nil {
		return err
	}
	m.MetadataID = persisted.Target.ID
	m.TMDbID = match.TMDbID
	m.BangumiID = match.BangumiID
	m.DoubanID = match.DoubanID
	m.TheTVDBID = match.TheTVDBID
	s.repo.MediaView.ReindexMediaIDs(ctx, m.ID)

	// Fetch extended metadata after the selected match is already saved.
	// Manual and batch applies must not fail just because an optional provider
	// details request is slow or unavailable.
	if match.TMDbID > 0 && s.tmdb != nil && s.tmdb.Enabled() {
		mediaType := s.determineMediaTypeForMedia(lib, m, match)
		detailsMetadataID := persisted.Target.ID
		if persisted.Series != nil {
			detailsMetadataID = persisted.Series.ID
		}
		s.fetchAndSaveTMDbExtendedMetadata(ctx, detailsMetadataID, match.TMDbID, mediaType)
		if mediaType == "tv" && !options.DeferEpisodeDetails {
			s.fetchAndSaveTMDbEpisodeDetails(ctx, m, persisted.Target.ID, match.TMDbID, match.Year, options)
		}
	}
	s.invalidateMediaCache(ctx)
	s.hub.Publish("scrape", map[string]any{
		"media_id":   m.ID,
		"title":      match.Title,
		"tmdb_id":    match.TMDbID,
		"bangumi_id": match.BangumiID,
		"douban_id":  match.DoubanID,
		"thetvdb_id": match.TheTVDBID,
		"source":     map[bool]string{true: "adult"}[match.NSFW],
	})
	return nil
}

func (s *ScraperService) markScrapeError(ctx context.Context, mediaID string, scrapeErr error) error {
	if scrapeErr == nil {
		return nil
	}
	message := scrapeErr.Error()
	if len(message) > 1024 {
		message = message[:1024]
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", mediaID).
		Updates(map[string]any{"scrape_status": "error", "scrape_error": message}).Error; err != nil {
		return errors.Join(scrapeErr, err)
	}
	s.invalidateMediaCache(ctx)
	return scrapeErr
}

func applyScrapeMediaTypeResets(updates map[string]any, match *Match) {
	if updates == nil || match == nil {
		return
	}
	switch normalizeOrganizeMediaType(match.MediaType) {
	case "movie", "adult":
		updates["season_num"] = 0
		updates["episode_num"] = 0
		updates["series_hint"] = ""
	}
}
