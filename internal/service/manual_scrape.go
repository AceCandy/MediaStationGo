package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type ManualScrapeRequest struct {
	Source         string   `json:"source"`
	MediaType      string   `json:"media_type"`
	Title          string   `json:"title"`
	OriginalName   string   `json:"original_name"`
	Overview       string   `json:"overview"`
	PosterURL      string   `json:"poster_url"`
	BackdropURL    string   `json:"backdrop_url"`
	Year           int      `json:"year"`
	ReleaseDate    string   `json:"release_date"`
	Rating         float32  `json:"rating"`
	TMDbID         int      `json:"tmdb_id"`
	BangumiID      int      `json:"bangumi_id"`
	DoubanID       string   `json:"douban_id"`
	TheTVDBID      string   `json:"thetvdb_id"`
	Languages      []string `json:"languages"`
	Countries      []string `json:"countries"`
	Genres         []string `json:"genres"`
	EpisodeArtwork *bool    `json:"episode_artwork,omitempty"`
	EpisodeImages  *bool    `json:"episode_images,omitempty"`
}

func (r ManualScrapeRequest) EpisodeArtworkOption() *bool {
	if r.EpisodeImages != nil {
		return r.EpisodeImages
	}
	return r.EpisodeArtwork
}

func (s *ScraperService) ApplyManualMatch(ctx context.Context, mediaID string, req ManualScrapeRequest) (*model.Media, error) {
	return s.ApplyManualMatchWithOptions(ctx, mediaID, req, ScrapeOptions{EpisodeArtwork: req.EpisodeArtworkOption()})
}

func (s *ScraperService) ApplyManualMatchWithOptions(ctx context.Context, mediaID string, req ManualScrapeRequest, options ScrapeOptions) (*model.Media, error) {
	media, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil || media == nil {
		return nil, errors.New("media not found")
	}
	if media.CatalogSource != "" {
		return nil, errors.New("独立资料体系媒体不能使用旧体系手动匹配")
	}
	lib, _ := s.repo.Library.FindBasicByID(ctx, media.LibraryID)
	if libraryUsesNFOOnly(lib) {
		return nil, errors.New("NFO-only library does not support provider matching")
	}
	match, err := s.manualRequestMatch(ctx, req)
	if err != nil {
		return nil, err
	}
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return nil, errors.New("manual match title required")
	}
	match.AllowIdentifierMerge = true
	if err := s.applyProviderMatchWithOptions(ctx, media, lib, match, options); err != nil {
		return nil, err
	}
	return s.repo.Media.FindByID(ctx, mediaID)
}

func (s *ScraperService) manualRequestMatch(ctx context.Context, req ManualScrapeRequest) (*Match, error) {
	source := strings.ToLower(strings.TrimSpace(req.Source))
	mediaType := normalizeMediaType(req.MediaType, req.Title, "")
	switch {
	case req.TMDbID > 0 && (source == "" || source == "tmdb"):
		if match := s.manualTMDbMatchByID(ctx, req.TMDbID, mediaType); match != nil {
			match.Source = "tmdb"
			return match, nil
		}
	case strings.TrimSpace(req.DoubanID) != "" && (source == "" || source == "douban"):
		if s.douban != nil {
			return s.douban.GetMatchByID(ctx, req.DoubanID)
		}
	default:
		return nil, ErrSourceMetadataReadOnly
	}
	return nil, errors.New("source details unavailable")
}
