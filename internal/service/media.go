// Package service — library / media bookkeeping.
package service

import (
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// MediaService offers high-level CRUD over libraries and media items.
type MediaService struct {
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	cache   *RuntimeCacheService
	artwork *ArtworkStore
}

type MediaVisibility struct {
	IncludeNSFW       bool
	AllowedLibraryIDs []string
	HiddenLibraryIDs  []string
}

const maxMediaSearchLimit = 50000
const maxMediaSearchPageSize = 2000

func (v MediaVisibility) Allows(media *model.Media) bool {
	if media == nil {
		return false
	}
	return v.allows(media.LibraryID, media.NSFW)
}

func (v MediaVisibility) AllowsView(media *model.MediaView) bool {
	if media == nil {
		return false
	}
	return v.allows(media.LibraryID, media.NSFW)
}

func (v MediaVisibility) allows(libraryID string, nsfw bool) bool {
	if !v.IncludeNSFW && nsfw {
		return false
	}
	for _, id := range v.HiddenLibraryIDs {
		if id == libraryID {
			return false
		}
	}
	if len(v.AllowedLibraryIDs) == 0 {
		return true
	}
	for _, id := range v.AllowedLibraryIDs {
		if id == libraryID {
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

func (s *MediaService) SetArtworkStore(artwork *ArtworkStore) *MediaService {
	if s != nil {
		s.artwork = artwork
	}
	return s
}
