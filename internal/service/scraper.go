// Package service — scraper orchestrator.
//
// ScraperService takes a Media row and tries to enrich it with metadata from
// explicit provider IDs after reading local NFO hints. Fanart.tv is
// artwork-only and upgrades poster/backdrop after a metadata match.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// EnrichOne runs the provider chain for a single media row.
func (s *ScraperService) EnrichOne(ctx context.Context, m *model.Media) error {
	return s.EnrichOneWithOptions(ctx, m, ScrapeOptions{})
}

func (s *ScraperService) EnrichOneWithOptions(ctx context.Context, m *model.Media, options ScrapeOptions) error {
	s.scrapeRunMu.Lock()
	defer s.scrapeRunMu.Unlock()
	return s.enrichOneWithOptions(ctx, m, options)
}

func (s *ScraperService) enrichOneWithOptions(ctx context.Context, m *model.Media, options ScrapeOptions) error {
	if m != nil && m.CatalogSource != "" {
		return errors.New("独立资料媒体请使用对应来源的刷新任务")
	}
	lib, err := s.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil {
		return err
	}
	if lib != nil && lib.Type == model.LibraryTypeHongGuo {
		return errors.New("红果媒体库不能运行现有资料刮削")
	}

	seriesLike := mediaIsEpisodic(m, lib)
	if seriesLike {
		if err := s.repairInvalidScrapeSeason(ctx, m); err != nil {
			return s.markScrapeError(ctx, m.ID, err)
		}
	}
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
	if libraryUsesNFOOnly(lib) {
		if localErr != nil {
			return s.markScrapeError(ctx, m.ID, localErr)
		}
		if local == nil || !local.HasNFO {
			return s.markScrapeNoMatch(ctx, m.ID, "")
		}
		if strings.TrimSpace(local.Title) == "" {
			local.Title = strings.TrimSpace(m.Title)
		}
		return recordScrapeSource(options, "local_nfo", s.applyLocalMetadataMatch(ctx, m, local))
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
	existing, err := s.useCanonicalTMDbLookupIDs(ctx, &lookupMedia, seriesLike)
	if err != nil {
		return s.markScrapeError(ctx, m.ID, err)
	}
	if !options.IncludeMatched && !options.RefreshWeakMatched {
		exact, err := s.repo.Media.FindExactMetadata(ctx, &lookupMedia)
		if err != nil {
			return s.markScrapeError(ctx, m.ID, err)
		}
		if exact != nil && (strings.TrimSpace(m.MetadataID) == "" || exact.ID == m.MetadataID) && !episodeMetadataNeedsRefresh(exact) {
			if err := s.markMetadataMatched(ctx, m, &lookupMedia, exact.ID); err != nil {
				return err
			}
			return recordScrapeSource(options, "existing_metadata", nil)
		}
	}

	lookupStartedAt := time.Now()
	externalResult := s.matchFromMediaExternalIDsWithOutcome(ctx, &lookupMedia, lib)
	if options.timings != nil {
		options.timings.ProviderLookup += time.Since(lookupStartedAt)
	}
	if match := externalResult.Match; match != nil {
		if match.Source == "tmdb" && match.TMDbID == lookupMedia.TMDbID {
			if existing != nil {
				mergeLocalMetadataIntoMatch(match, &LocalMetadata{
					Title: existing.Title, OriginalName: existing.OriginalName, Overview: existing.Overview,
					Year: existing.Year, Rating: existing.Rating, ReleaseDate: existing.ReleaseDate,
					Languages: existing.Languages, Countries: existing.Countries, Genres: existing.Genres, NSFW: existing.NSFW,
				})
			}
			mergeLocalMetadataIntoMatch(match, &LocalMetadata{TMDbID: lookupMedia.TMDbID, BangumiID: lookupMedia.BangumiID, DoubanID: lookupMedia.DoubanID, TheTVDBID: lookupMedia.TheTVDBID, PathHint: true})
			mergeLocalMetadataIntoMatch(match, local)
		}
		mergeLocalCreditsIntoMatch(match, local)
		s.applyFanartArtwork(ctx, match, s.determineMediaTypeForMedia(lib, &lookupMedia, match))
		return recordScrapeSource(options, metadataMatchSource(match), s.applyProviderMatchWithOptions(ctx, m, lib, match, options))
	}
	if externalResult.Err != nil {
		return s.markScrapeError(ctx, m.ID, externalResult.Err)
	}
	if localErr != nil {
		return s.markScrapeError(ctx, m.ID, localErr)
	}
	if localMetadataEligibleForFallback(local) {
		if strings.TrimSpace(local.Title) == "" {
			local.Title = strings.TrimSpace(m.Title)
		}
		return recordScrapeSource(options, "local_nfo", s.applyLocalMetadataMatch(ctx, m, local))
	}
	return s.markScrapeNoMatch(ctx, m.ID, "")
}

func (s *ScraperService) markScrapeNoMatch(ctx context.Context, mediaID, query string) error {
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", mediaID).
		Updates(map[string]any{"scrape_status": "no_match", "scrape_error": "", "local_metadata_hint": ""}).Error; err != nil {
		return err
	}
	s.invalidateMediaCache(ctx)
	s.log.Info("metadata scrape no match", zap.String("media_id", mediaID), zap.String("query", query))
	return nil
}

func (s *ScraperService) markMetadataMatched(ctx context.Context, media, lookup *model.Media, metadataID string) error {
	updates := map[string]any{
		"metadata_id":         metadataID,
		"scrape_status":       "matched",
		"scrape_error":        "",
		"local_metadata_hint": "",
		"series_hint":         lookup.SeriesID,
		"lookup_tmdb_id":      lookup.TMDbID,
		"lookup_bangumi_id":   lookup.BangumiID,
		"lookup_douban_id":    lookup.DoubanID,
		"lookup_thetvdb_id":   lookup.TheTVDBID,
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", media.ID).Updates(updates).Error; err != nil {
		return err
	}
	oldMetadataID := media.MetadataID
	media.MetadataID = metadataID
	media.SeriesID = lookup.SeriesID
	media.ScrapeStatus = "matched"
	s.repo.MediaView.RefreshMetadataIDs(ctx, oldMetadataID, media.MetadataID)
	s.invalidateMediaCache(ctx)
	return nil
}

func episodeMetadataNeedsRefresh(metadata *model.MetadataItem) bool {
	if metadata == nil || metadata.Kind != model.MetadataKindEpisode || !tmdbEntityTitleIsGenerated(metadata.Title, model.MetadataKindEpisode) {
		return false
	}
	return !metadata.UpdatedAt.After(time.Now().UTC().Add(-7 * 24 * time.Hour))
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
	persistStartedAt := time.Now()
	persisted, err := s.persistProviderMetadata(ctx, m, lib, match)
	if options.timings != nil {
		persistDuration := time.Since(persistStartedAt)
		if persisted != nil {
			options.timings.Artwork += persisted.ArtworkDuration
			persistDuration -= persisted.ArtworkDuration
		}
		options.timings.MetadataPersist += persistDuration
	}
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
	oldMetadataID := m.MetadataID
	m.MetadataID = persisted.Target.ID
	m.TMDbID = match.TMDbID
	m.BangumiID = match.BangumiID
	m.DoubanID = match.DoubanID
	m.TheTVDBID = match.TheTVDBID
	s.repo.MediaView.RefreshMetadataIDs(ctx, oldMetadataID, m.MetadataID)

	// Fetch extended metadata after the selected match is already saved.
	// Manual and batch applies must not fail just because an optional provider
	// details request is slow or unavailable.
	if match.TMDbID > 0 && s.tmdb != nil && s.tmdb.Enabled() {
		mediaType := s.determineMediaTypeForMedia(lib, m, match)
		detailsMetadataID := persisted.Target.ID
		if persisted.Series != nil {
			detailsMetadataID = persisted.Series.ID
		}
		if !match.TMDbDetailsLoaded {
			detailsStartedAt := time.Now()
			s.fetchAndSaveTMDbExtendedMetadata(ctx, detailsMetadataID, match.TMDbID, mediaType)
			if options.timings != nil {
				options.timings.TMDbExtendedDetails += time.Since(detailsStartedAt)
			}
		}
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
