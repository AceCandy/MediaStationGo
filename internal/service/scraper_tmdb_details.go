package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *ScraperService) fetchAndSaveTMDbExtendedMetadata(ctx context.Context, metadataID string, tmdbID int, mediaType string) {
	detailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tmdbDetailsTimeout)
	var details *Match
	var err error
	if mediaType == "tv" {
		details, err = s.tmdb.GetTVMatch(detailCtx, tmdbID)
	} else {
		details, err = s.tmdb.GetMovieMatch(detailCtx, tmdbID)
	}
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
	s.persistTMDbSnapshot(ctx, metadataID, details.RawJSON)
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

func (s *ScraperService) fetchAndSaveTMDbSeasonSnapshot(ctx context.Context, episodeMetadataID string, tmdbID, seasonNum int) bool {
	item, err := s.repo.Metadata.FindByID(ctx, episodeMetadataID)
	if err != nil || item == nil || item.ParentID == nil {
		return false
	}
	seasonID := *item.ParentID
	if snapshot, findErr := s.repo.Metadata.FindProviderSnapshot(ctx, seasonID, "tmdb"); findErr == nil && snapshot != nil {
		return false
	}
	seasonCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tmdbDetailsTimeout)
	details, err := s.tmdb.GetTVSeasonDetails(seasonCtx, tmdbID, seasonNum)
	cancel()
	if err != nil || details == nil || details.SeasonNumber != seasonNum {
		if err != nil && s.log != nil {
			s.log.Debug("failed to get tmdb season details", zap.String("metadata_id", seasonID), zap.Error(err))
		}
		return false
	}
	return s.persistTMDbSnapshot(ctx, seasonID, details.RawJSON)
}

func (s *ScraperService) fetchAndSaveTMDbEpisodeDetails(ctx context.Context, m *model.Media, metadataID string, tmdbID int, matchYear int, options ScrapeOptions) bool {
	if s == nil || s.tmdb == nil || !s.tmdb.Enabled() || m == nil || metadataID == "" || tmdbID <= 0 || m.EpisodeNum <= 0 {
		return false
	}
	seasonSnapshotUpdated := s.fetchAndSaveTMDbSeasonSnapshot(ctx, metadataID, tmdbID, m.SeasonNum)
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
		return seasonSnapshotUpdated
	}
	if episode == nil {
		return seasonSnapshotUpdated
	}
	episodeSnapshotUpdated := s.persistTMDbSnapshot(ctx, metadataID, episode.RawJSON)
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
	if len(metadataUpdates) == 0 && len(mediaUpdates) == 0 && !artworkUpdated && !creditsUpdated && !seasonSnapshotUpdated && !episodeSnapshotUpdated {
		return false
	}
	if len(metadataUpdates) > 0 {
		item, updateErr := s.repo.Metadata.FindByID(ctx, metadataID)
		if updateErr == nil && item != nil {
			applyTMDbMetadataUpdates(item, metadataUpdates)
			updateErr = s.repo.Metadata.Update(ctx, item)
		}
		if updateErr != nil {
			s.log.Warn("failed to save tmdb episode metadata",
				zap.String("media_id", m.ID), zap.String("metadata_id", metadataID), zap.Error(updateErr))
			return seasonSnapshotUpdated || episodeSnapshotUpdated
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
			return seasonSnapshotUpdated || episodeSnapshotUpdated
		}
	}
	return true
}

func applyTMDbMetadataUpdates(item *model.MetadataItem, updates map[string]any) {
	if value, ok := updates["title"].(string); ok {
		item.Title = value
		item.OriginalName = ""
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
	if value, ok := updates["release_date"].(string); ok {
		item.ReleaseDate = value
	}
}

func tmdbEpisodeMetadataUpdates(_ *model.Media, episode *TMDbEpisodeDetails, matchYear int) (map[string]any, map[string]any) {
	mediaUpdates := map[string]any{}
	if episode == nil {
		return map[string]any{}, mediaUpdates
	}
	item := &model.MetadataItem{Title: episode.Name, Overview: episode.Overview, Rating: episode.Rating, Year: episode.AirYear, ReleaseDate: episode.AirDate}
	if matchYear > 0 {
		item.Year = 0
	}
	return tmdbMetadataUpdates(item), mediaUpdates
}

// tmdbMetadataUpdates 只投影来源非空字段，季和集复查共用同一保存规则。
func tmdbMetadataUpdates(item *model.MetadataItem) map[string]any {
	metadataUpdates := map[string]any{}
	if strings.TrimSpace(item.Title) != "" {
		metadataUpdates["title"] = strings.TrimSpace(item.Title)
	}
	if strings.TrimSpace(item.Overview) != "" {
		metadataUpdates["overview"] = strings.TrimSpace(item.Overview)
	}
	if item.Rating > 0 {
		metadataUpdates["rating"] = item.Rating
	}
	if item.Year > 0 {
		metadataUpdates["year"] = item.Year
	}
	if strings.TrimSpace(item.ReleaseDate) != "" {
		metadataUpdates["release_date"] = strings.TrimSpace(item.ReleaseDate)
	}
	return metadataUpdates
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
		if media.EpisodeNum <= 0 || media.MetadataID == "" || media.SeriesID == "" {
			continue
		}
		identifiers, identifierErr := s.repo.Metadata.ListIdentifiers(ctx, media.SeriesID)
		if identifierErr != nil {
			s.log.Debug("deferred episode series identity lookup failed", zap.String("media_id", rows[i].ID), zap.Error(identifierErr))
			continue
		}
		seriesTMDbID := 0
		for _, identifier := range identifiers {
			if identifier.Provider == "tmdb" && identifier.EntityKind == model.MetadataKindSeries {
				seriesTMDbID, _ = strconv.Atoi(identifier.ExternalID)
				break
			}
		}
		if seriesTMDbID <= 0 {
			continue
		}
		lib, _ := s.repo.Library.FindByID(ctx, media.LibraryID)
		if !mediaIsEpisodic(&media.Media, lib) {
			continue
		}
		if s.fetchAndSaveTMDbEpisodeDetails(ctx, &media.Media, media.MetadataID, seriesTMDbID, media.Year, options) {
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
