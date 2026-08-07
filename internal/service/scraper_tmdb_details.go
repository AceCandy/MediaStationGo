package service

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *ScraperService) fetchAndSaveTMDbExtendedMetadata(ctx context.Context, metadataID string, tmdbID int, mediaType string) {
	detailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tmdbDetailsTimeout)
	details, err := s.tmdb.GetDetails(detailCtx, tmdbID, mediaType)
	cancel()
	if err != nil {
		s.log.Warn("failed to get details from tmdb",
			zap.Int("tmdb_id", tmdbID),
			zap.String("type", mediaType),
			zap.Error(err))
		return
	}
	if details == nil {
		return
	}
	updates := map[string]any{}
	if len(details.Languages) > 0 {
		updates["languages"] = strings.Join(details.Languages, ",")
	}
	if len(details.Countries) > 0 {
		updates["countries"] = strings.Join(details.Countries, ",")
	}
	if len(details.Genres) > 0 {
		updates["genres"] = strings.Join(details.Genres, ",")
	}
	if len(updates) > 0 {
		item, findErr := s.repo.Metadata.FindByID(ctx, metadataID)
		if findErr == nil && item != nil {
			if value, ok := updates["languages"].(string); ok {
				item.Languages = value
			}
			if value, ok := updates["countries"].(string); ok {
				item.Countries = value
			}
			if value, ok := updates["genres"].(string); ok {
				item.Genres = value
			}
			findErr = s.repo.Metadata.Update(ctx, item)
		}
		if findErr != nil {
			s.log.Warn("failed to save tmdb extended metadata",
				zap.String("metadata_id", metadataID),
				zap.Int("tmdb_id", tmdbID),
				zap.Error(findErr))
		}
	}
	s.log.Debug("enrich: saved extended metadata",
		zap.String("metadata_id", metadataID),
		zap.Strings("languages", details.Languages),
		zap.Strings("countries", details.Countries),
		zap.Strings("genres", details.Genres))
}

func (s *ScraperService) fetchAndSaveTMDbEpisodeDetails(ctx context.Context, m *model.Media, metadataID string, tmdbID int, matchYear int, options ScrapeOptions) bool {
	if s == nil || s.tmdb == nil || !s.tmdb.Enabled() || m == nil || metadataID == "" || tmdbID <= 0 || m.EpisodeNum <= 0 {
		return false
	}
	episodeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tmdbDetailsTimeout)
	episode, err := s.tmdb.GetTVEpisodeDetails(episodeCtx, tmdbID, m.SeasonNum, m.EpisodeNum)
	cancel()
	if err != nil {
		s.log.Debug("failed to get tmdb episode details",
			zap.String("media_id", m.ID),
			zap.Int("tmdb_id", tmdbID),
			zap.Int("season", m.SeasonNum),
			zap.Int("episode", m.EpisodeNum),
			zap.Error(err))
		return false
	}
	if episode == nil {
		return false
	}
	creditsUpdated := false
	if len(episode.LoadedCreditTypes) > 0 {
		if err := s.persistCredits(ctx, metadataID, episode.LoadedCreditTypes, episode.Credits); err != nil {
			s.log.Warn("failed to save tmdb episode credits", zap.String("media_id", m.ID), zap.Error(err))
		} else {
			creditsUpdated = true
		}
	}
	metadataUpdates, mediaUpdates := tmdbEpisodeMetadataUpdates(m, episode, matchYear)
	artworkUpdated := false
	if strings.TrimSpace(episode.StillURL) != "" && options.episodeArtworkEnabled() {
		if stillURL, artworkErr := s.persistOneMetadataArtwork(ctx, metadataID, model.ArtworkTypeStill, "tmdb", strings.TrimSpace(episode.StillURL)); artworkErr != nil {
			s.log.Warn("failed to persist tmdb episode still", zap.String("media_id", m.ID), zap.Error(artworkErr))
		} else if stillURL != "" {
			artworkUpdated = true
		}
	}
	if len(metadataUpdates) == 0 && len(mediaUpdates) == 0 && !artworkUpdated && !creditsUpdated {
		return false
	}
	if len(metadataUpdates) > 0 {
		item, updateErr := s.repo.Metadata.FindByID(ctx, metadataID)
		if updateErr == nil && item != nil {
			applyTMDbEpisodeMetadataUpdates(item, metadataUpdates)
			updateErr = s.repo.Metadata.Update(ctx, item)
		}
		if updateErr != nil {
			s.log.Warn("failed to save tmdb episode metadata",
				zap.String("media_id", m.ID), zap.String("metadata_id", metadataID), zap.Error(updateErr))
			return false
		}
	}
	if len(mediaUpdates) > 0 {
		if err := s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).Updates(mediaUpdates).Error; err != nil {
			s.log.Warn("failed to save tmdb episode metadata",
				zap.String("media_id", m.ID),
				zap.Int("tmdb_id", tmdbID),
				zap.Int("season", m.SeasonNum),
				zap.Int("episode", m.EpisodeNum),
				zap.Error(err))
			return false
		}
	}
	return true
}

func applyTMDbEpisodeMetadataUpdates(item *model.MetadataItem, updates map[string]any) {
	if value, ok := updates["episode_title"].(string); ok {
		item.EpisodeTitle = value
	}
	if value, ok := updates["overview"].(string); ok {
		item.Overview = value
	}
	if value, ok := updates["rating"].(float32); ok {
		item.Rating = value
	}
	if value, ok := updates["year"].(int); ok {
		item.Year = value
	}
}

func tmdbEpisodeMetadataUpdates(m *model.Media, episode *TMDbEpisodeDetails, matchYear int) (map[string]any, map[string]any) {
	metadataUpdates := map[string]any{}
	mediaUpdates := map[string]any{}
	if episode == nil {
		return metadataUpdates, mediaUpdates
	}
	// Keep original_name at series level. Per-episode names can split one show
	// into multiple cards because original_name participates in grouping.
	if strings.TrimSpace(episode.Name) != "" {
		metadataUpdates["episode_title"] = strings.TrimSpace(episode.Name)
	}
	if strings.TrimSpace(episode.Overview) != "" {
		metadataUpdates["overview"] = strings.TrimSpace(episode.Overview)
	}
	if episode.Rating > 0 {
		metadataUpdates["rating"] = episode.Rating
	}
	if episode.AirYear > 0 && matchYear <= 0 {
		metadataUpdates["year"] = episode.AirYear
	}
	if m != nil && episode.Runtime > 0 && m.DurationSec <= 0 {
		mediaUpdates["duration_sec"] = episode.Runtime * 60
	}
	return metadataUpdates, mediaUpdates
}

func (s *ScraperService) enrichDeferredEpisodeDetails(ctx context.Context, rows []model.Media, options ScrapeOptions) error {
	if s == nil || s.tmdb == nil || !s.tmdb.Enabled() {
		return nil
	}
	for i := range rows {
		if rows[i].EpisodeNum <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		media, err := s.repo.MediaView.FindByID(ctx, rows[i].ID)
		if err != nil || media == nil {
			s.log.Debug("deferred episode metadata media missing", zap.String("media_id", rows[i].ID), zap.Error(err))
			continue
		}
		if media.TMDbID <= 0 || media.EpisodeNum <= 0 || media.MetadataID == "" {
			continue
		}
		lib, _ := s.repo.Library.FindByID(ctx, media.LibraryID)
		if !mediaIsEpisodic(&media.Media, lib) {
			continue
		}
		if s.fetchAndSaveTMDbEpisodeDetails(ctx, &media.Media, media.MetadataID, media.TMDbID, media.Year, options) {
			s.invalidateMediaCache(ctx)
		}
		if i < len(rows)-1 {
			if delay := s.scrapeDelay(ctx); delay > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
	return nil
}
