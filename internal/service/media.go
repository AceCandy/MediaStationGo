// Package service — library / media bookkeeping.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// NewMediaService is the constructor.
func NewMediaService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *MediaService {
	return &MediaService{cfg: cfg, log: log, repo: repo}
}

// CreateLibrary persists a library after validating that its path exists.
func (s *MediaService) CreateLibrary(ctx context.Context, name, path, kind string) (*model.Library, error) {
	if name == "" || path == "" {
		return nil, errors.New("name and path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("path is not an accessible directory: %s", abs)
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
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 2000 {
		pageSize = 2000
	}
	if page < 1 {
		page = 1
	}
	return s.repo.Media.ListByLibrary(ctx, libraryID, (page-1)*pageSize, pageSize)
}

// SearchMedia performs a simple LIKE search across titles.
func (s *MediaService) SearchMedia(ctx context.Context, query string, limit int) ([]model.Media, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.Media.Search(ctx, query, limit)
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
