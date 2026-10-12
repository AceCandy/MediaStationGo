package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func (s *ScraperService) persistTMDbSourceData(ctx context.Context, metadataID string, payload json.RawMessage) bool {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || !json.Valid(payload) {
		return false
	}
	if err := s.repo.Metadata.SaveProviderData(ctx, metadataID, "tmdb", payload, time.Now().UTC()); err != nil {
		if s.log != nil {
			s.log.Warn("failed to persist tmdb source data", zap.String("metadata_id", metadataID), zap.Error(err))
		}
		return false
	}
	return true
}

// ensureTMDbSourceData 为已接受的其他来源资料按需补资料中心正文；失败保留资料和缺口供后续重试。
func (s *ScraperService) ensureTMDbSourceData(ctx context.Context, metadataID, entityKind string, tmdbID int) {
	if tmdbID <= 0 || s.tmdb == nil || s.tmdb.resolveAPIKey(ctx) == "" {
		return
	}
	snapshot, err := s.repo.Metadata.FindProviderData(ctx, metadataID, "tmdb")
	if err == nil && snapshot != nil {
		return
	}
	if err == nil {
		candidate := repository.TMDbSourceIdentity{MetadataID: metadataID, EntityKind: entityKind, TMDbID: tmdbID}
		var payload json.RawMessage
		payload, err = s.fetchTMDbSourceData(ctx, candidate)
		if err == nil {
			err = s.repo.Metadata.InsertMissingTMDbData(ctx, candidate, payload, time.Now().UTC())
		}
	}
	if err != nil && s.log != nil {
		s.log.Warn("failed to fill tmdb source data", zap.String("metadata_id", metadataID), zap.Error(sanitizeTaskLogError(err)))
	}
}

func (s *ScraperService) fetchTMDbSourceData(ctx context.Context, candidate repository.TMDbSourceIdentity) (json.RawMessage, error) {
	detailCtx, cancel := context.WithTimeout(ctx, tmdbDetailsTimeout)
	defer cancel()
	var payload json.RawMessage
	switch candidate.EntityKind {
	case model.MetadataKindMovie:
		match, err := s.tmdb.GetMovieMatch(detailCtx, candidate.TMDbID)
		if err != nil {
			return nil, err
		}
		if match == nil || match.TMDbID != candidate.TMDbID {
			return nil, errors.New("TMDB movie details unavailable")
		}
		payload = match.RawJSON
	case model.MetadataKindSeries:
		match, err := s.tmdb.GetTVMatch(detailCtx, candidate.TMDbID)
		if err != nil {
			return nil, err
		}
		if match == nil || match.TMDbID != candidate.TMDbID {
			return nil, errors.New("TMDB series details unavailable")
		}
		payload = match.RawJSON
	case model.MetadataKindSeason:
		if candidate.SeriesTMDbID <= 0 {
			return nil, errors.New("TMDB series identity unavailable")
		}
		details, err := s.tmdb.GetTVSeasonDetails(detailCtx, candidate.SeriesTMDbID, candidate.SeasonNum)
		if err != nil {
			return nil, err
		}
		if details == nil || details.ID != candidate.TMDbID {
			return nil, errors.New("TMDB season details unavailable")
		}
		payload = details.RawJSON
	case model.MetadataKindEpisode:
		if candidate.SeriesTMDbID <= 0 {
			return nil, errors.New("TMDB series identity unavailable")
		}
		details, err := s.tmdb.GetTVEpisodeDetails(detailCtx, candidate.SeriesTMDbID, candidate.SeasonNum, candidate.EpisodeNum)
		if err != nil {
			return nil, err
		}
		if details == nil || details.ID != candidate.TMDbID {
			return nil, errors.New("TMDB episode details unavailable")
		}
		payload = details.RawJSON
	default:
		return nil, errors.New("unsupported metadata kind")
	}
	if !json.Valid(payload) {
		return nil, errors.New("TMDB details returned invalid JSON")
	}
	return payload, nil
}
