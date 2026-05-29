// Package service — library / media bookkeeping.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// MediaService offers high-level CRUD over libraries and media items.
type MediaService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

type MediaVisibility struct {
	IncludeNSFW       bool
	AllowedLibraryIDs []string
	HiddenLibraryIDs  []string
}

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

// CreateLibrary persists a library after validating that its path exists.
func (s *MediaService) CreateLibrary(ctx context.Context, name, path, kind string) (*model.Library, error) {
	if name == "" || path == "" {
		return nil, errors.New("name and path required")
	}
	abs, err := resolveAccessibleLibraryPath(path)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		kind = "movie"
	}
	lib := &model.Library{Name: name, Path: abs, Type: kind, Enabled: true}
	if err := s.repo.Library.Create(ctx, lib); err != nil {
		return nil, err
	}
	return lib, nil
}

func resolveAccessibleLibraryPath(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if isAccessibleDir(abs) {
		return abs, nil
	}
	for _, candidate := range dockerVolumePathCandidates(abs) {
		if isAccessibleDir(candidate) {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("path is not an accessible directory: %s", abs)
}

func isAccessibleDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func dockerVolumePathCandidates(path string) []string {
	normalized := filepath.ToSlash(filepath.Clean(path))
	var candidates []string
	addCandidate := func(candidate string) {
		candidate = filepath.Clean(filepath.FromSlash(candidate))
		for _, existing := range candidates {
			if sameLibraryPath(existing, candidate) {
				return
			}
		}
		candidates = append(candidates, candidate)
	}

	for _, mapping := range []struct {
		env       string
		container string
	}{
		{env: "MEDIASTATION_MEDIA_DIR", container: envOrDefault("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media")},
		{env: "MEDIASTATION_DOWNLOAD_DIR", container: envOrDefault("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "/downloads")},
	} {
		host := filepath.ToSlash(filepath.Clean(os.Getenv(mapping.env)))
		if host == "." || host == "" || strings.HasPrefix(host, ".") {
			continue
		}
		if normalized == host {
			addCandidate(mapping.container)
			continue
		}
		if strings.HasPrefix(normalized, host+"/") {
			addCandidate(mapping.container + strings.TrimPrefix(normalized, host))
		}
	}

	for _, marker := range []struct {
		part      string
		container string
	}{
		{part: "/media/", container: "/media/"},
		{part: "/downloads/", container: "/downloads/"},
	} {
		if idx := strings.Index(normalized, marker.part); idx >= 0 {
			addCandidate(marker.container + strings.TrimPrefix(normalized[idx+len(marker.part):], "/"))
		}
	}

	return candidates
}

func sameLibraryPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// ListLibraries returns every library configured on the server.
func (s *MediaService) ListLibraries(ctx context.Context) ([]model.Library, error) {
	return s.repo.Library.List(ctx)
}

// DeleteLibrary removes a library and its media rows. The on-disk files are
// left untouched.
func (s *MediaService) DeleteLibrary(ctx context.Context, id string) error {
	if err := s.repo.Media.DeleteByLibrary(ctx, id); err != nil {
		return err
	}
	return s.repo.Library.Delete(ctx, id)
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
	return s.repo.Media.ListByLibraryFiltered(ctx, libraryID, (page-1)*pageSize, pageSize, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
}

// SearchMedia performs a simple LIKE search across titles.
func (s *MediaService) SearchMedia(ctx context.Context, query string, limit int) ([]model.Media, error) {
	return s.SearchMediaVisible(ctx, query, limit, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) SearchMediaVisible(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]model.Media, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.Media.SearchFiltered(ctx, query, limit, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
}

// GetMedia returns a single media row.
func (s *MediaService) GetMedia(ctx context.Context, id string) (*model.Media, error) {
	return s.repo.Media.FindByID(ctx, id)
}

// SoftDelete moves a media row to the recycle bin (gorm soft delete).
// The on-disk file is kept; admins can purge it later.
func (s *MediaService) SoftDelete(ctx context.Context, id string) error {
	return s.repo.DB.Where("id = ?", id).Delete(&model.Media{}).Error
}

// RestoreDeleted unsets DeletedAt for a single media row.
func (s *MediaService) RestoreDeleted(ctx context.Context, id string) error {
	return s.repo.DB.Unscoped().Model(&model.Media{}).
		Where("id = ?", id).Update("deleted_at", nil).Error
}

// ListRecycleBin returns every soft-deleted row, newest first.
func (s *MediaService) ListRecycleBin(ctx context.Context, limit int) ([]model.Media, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []model.Media
	err := s.repo.DB.Unscoped().
		Where("deleted_at IS NOT NULL").
		Order("deleted_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// PurgeDeleted permanently removes a soft-deleted row from the database.
func (s *MediaService) PurgeDeleted(ctx context.Context, id string) error {
	return s.repo.DB.Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
}
