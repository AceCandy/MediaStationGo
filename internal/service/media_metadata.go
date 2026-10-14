package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var ErrSourceMetadataReadOnly = errors.New("基础信息由资料中心提供，只能修改 TMDb 关联；豆瓣关联请使用绑定入口")

type MediaMetadataUpdate struct {
	// Scope 为 series 或 season 时修改父来源关联；本地季集和文件身份保持稳定。
	Scope        string   `json:"scope,omitempty"`
	Title        *string  `json:"title"`
	OriginalName *string  `json:"original_name"`
	Overview     *string  `json:"overview"`
	Year         *int     `json:"year"`
	ReleaseDate  *string  `json:"release_date"`
	Rating       *float32 `json:"rating"`
	SeasonNum    *int     `json:"season_num"`
	EpisodeNum   *int     `json:"episode_num"`
	TMDbID       *int     `json:"tmdb_id"`
	BangumiID    *int     `json:"bangumi_id"`
	DoubanID     *string  `json:"douban_id"`
	TheTVDBID    *string  `json:"thetvdb_id"`
	Languages    *string  `json:"languages"`
	Countries    *string  `json:"countries"`
	Genres       *string  `json:"genres"`
}

func (s *MediaService) UpdateMetadata(ctx context.Context, id string, req MediaMetadataUpdate) (*model.MediaView, error) {
	if s == nil || s.repo == nil || s.repo.DB == nil || s.repo.Metadata == nil || s.repo.MediaView == nil {
		return nil, errors.New("media service unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("media id required")
	}
	if req.Scope != "" && req.Scope != "series" && req.Scope != "season" {
		return nil, errors.New("invalid metadata scope")
	}
	if req.Scope != "" && (req.SeasonNum != nil || req.EpisodeNum != nil) {
		return nil, fmt.Errorf("%s metadata cannot change episode coordinates", req.Scope)
	}
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if media == nil {
		return nil, errors.New("media not found")
	}
	if media.CatalogSource == model.CatalogSourceNFO {
		view, err := s.repo.MediaView.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if view == nil {
			return nil, errors.New("media not found")
		}
		return s.updateNFOMetadata(ctx, media, view, req)
	}
	if media.CatalogSource != "" {
		return nil, errors.New("独立资料体系不支持旧体系元数据编辑")
	}
	view, err := s.repo.MediaView.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, errors.New("media not found")
	}

	if req.Title != nil || req.OriginalName != nil || req.Overview != nil || req.Year != nil || req.ReleaseDate != nil || req.Rating != nil || req.Languages != nil || req.Countries != nil || req.Genres != nil || req.SeasonNum != nil || req.EpisodeNum != nil || req.BangumiID != nil || req.TheTVDBID != nil || req.DoubanID != nil {
		return nil, ErrSourceMetadataReadOnly
	}
	if req.TMDbID == nil || *req.TMDbID < 0 {
		return nil, errors.New("valid TMDb ID required")
	}
	targetID := media.MetadataID
	if req.Scope == "series" {
		targetID = view.SeriesID
	}
	if req.Scope == "season" {
		targetID = view.SeasonID
	}
	target, err := s.repo.Metadata.FindByID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, errors.New("metadata not found")
	}
	if *req.TMDbID > 0 {
		snapshot, err := s.fetchManualTMDbSnapshot(ctx, target, *req.TMDbID)
		if err != nil {
			return nil, err
		}
		if err := s.repo.Metadata.RebindTMDb(ctx, *target, strconv.Itoa(*req.TMDbID), snapshot); err != nil {
			return nil, err
		}
	} else if err := s.repo.Metadata.RebindTMDb(ctx, *target, "", nil); err != nil {
		return nil, err
	}
	if req.Scope != "" {
		s.repo.MediaView.RefreshMetadataIDs(ctx, target.ID)
		s.invalidateMediaCache(ctx)
		if req.Scope == "season" {
			return s.GetMediaSeasonVisible(ctx, media.ID, MediaVisibility{IncludeNSFW: true})
		}
		return s.GetMediaSeriesVisible(ctx, media.ID, MediaVisibility{IncludeNSFW: true})
	}
	s.repo.MediaView.RefreshMetadataIDs(ctx, media.MetadataID, target.ID)
	s.invalidateMediaCache(ctx)
	return s.repo.MediaView.FindByID(ctx, id)
}

func (s *MediaService) fetchManualTMDbSnapshot(ctx context.Context, item *model.MetadataItem, tmdbID int) (json.RawMessage, error) {
	if s == nil || s.tmdb == nil || item == nil || tmdbID <= 0 {
		return nil, errors.New("TMDB details unavailable")
	}
	var payload json.RawMessage
	switch item.Kind {
	case model.MetadataKindMovie:
		match, err := s.tmdb.GetMovieMatch(ctx, tmdbID)
		if err != nil {
			return nil, err
		}
		if match == nil || match.TMDbID != tmdbID {
			return nil, errors.New("TMDB movie details unavailable")
		}
		payload = match.RawJSON
	case model.MetadataKindSeries:
		match, err := s.tmdb.GetTVMatch(ctx, tmdbID)
		if err != nil {
			return nil, err
		}
		if match == nil || match.TMDbID != tmdbID {
			return nil, errors.New("TMDB series details unavailable")
		}
		payload = match.RawJSON
	case model.MetadataKindSeason, model.MetadataKindEpisode:
		seriesTMDbID, seasonNum, err := s.manualTMDbSeriesCoordinates(ctx, item)
		if err != nil {
			return nil, err
		}
		if item.Kind == model.MetadataKindSeason {
			details, detailErr := s.tmdb.GetTVSeasonDetails(ctx, seriesTMDbID, seasonNum)
			if detailErr != nil {
				return nil, detailErr
			}
			if details == nil || details.ID != tmdbID {
				return nil, errors.New("TMDB season details unavailable")
			}
			payload = details.RawJSON
		} else {
			details, detailErr := s.tmdb.GetTVEpisodeDetails(ctx, seriesTMDbID, seasonNum, item.EpisodeNum)
			if detailErr != nil {
				return nil, detailErr
			}
			if details == nil || details.ID != tmdbID {
				return nil, errors.New("TMDB episode details unavailable")
			}
			payload = details.RawJSON
		}
	default:
		return nil, fmt.Errorf("unsupported metadata kind %q", item.Kind)
	}
	if !json.Valid(payload) {
		return nil, errors.New("TMDB details returned invalid JSON")
	}
	return payload, nil
}

func (s *MediaService) manualTMDbSeriesCoordinates(ctx context.Context, item *model.MetadataItem) (int, int, error) {
	if item == nil || item.ParentID == nil {
		return 0, 0, errors.New("TMDB series identity unavailable")
	}
	seriesID, seasonNum := *item.ParentID, item.SeasonNum
	if item.Kind == model.MetadataKindEpisode {
		season, err := s.repo.Metadata.FindByID(ctx, *item.ParentID)
		if err != nil {
			return 0, 0, err
		}
		if season == nil || season.Kind != model.MetadataKindSeason || season.ParentID == nil {
			return 0, 0, errors.New("TMDB season hierarchy unavailable")
		}
		seriesID, seasonNum = *season.ParentID, season.SeasonNum
	}
	identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, seriesID)
	if err != nil {
		return 0, 0, err
	}
	for _, identifier := range identifiers {
		if identifier.Provider == "tmdb" && identifier.EntityKind == model.MetadataKindSeries {
			value, parseErr := strconv.Atoi(identifier.ExternalID)
			if parseErr == nil && value > 0 {
				return value, seasonNum, nil
			}
		}
	}
	return 0, 0, errors.New("TMDB series identity unavailable")
}

func normalizeMetadataCSV(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r', '\t':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, ",")
}

func clampNonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func clampRating(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 10 {
		return 10
	}
	return value
}
