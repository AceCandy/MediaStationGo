package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// MediaRepository persists model.Media records.
type MediaRepository struct {
	db   *gorm.DB
	view *MediaViewRepository
}

type MediaSearchBackend interface {
	SearchMediaIDs(ctx context.Context, query string, offset, limit int, filter MediaQueryFilter) ([]string, int64, error)
}

type MediaSearchSyncBackend interface {
	MediaSearchBackend
	EnsureIndex(ctx context.Context) error
	IndexMedia(ctx context.Context, rows []model.MediaView) error
}

func (r *MediaRepository) SetSearchBackend(backend MediaSearchBackend) {
	if r != nil && r.viewRepository() != nil {
		r.viewRepository().SetSearchBackend(backend)
	}
}

// MediaQueryFilter is applied to user-facing media queries so NSFW items and
// profile-restricted libraries are filtered in SQL instead of only in React.
type MediaQueryFilter struct {
	IncludeNSFW       bool
	AllowedLibraryIDs []string
	HiddenLibraryIDs  []string
}

func (r *MediaRepository) indexMediaBestEffort(ctx context.Context, media model.Media) {
	viewRepo := r.viewRepository()
	if viewRepo == nil {
		return
	}
	viewRepo.indexMediaIDsBestEffort(ctx, []string{media.ID})
}

func (r *MediaRepository) viewRepository() *MediaViewRepository {
	if r == nil || r.db == nil {
		return nil
	}
	if r.view == nil {
		r.view = &MediaViewRepository{db: r.db}
	}
	return r.view
}

// FindByID returns the media row or (nil, nil).
func (r *MediaRepository) FindByID(ctx context.Context, id string) (*model.Media, error) {
	var m model.Media
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByLibrary returns paginated media items for a library.
func (r *MediaRepository) ListByLibrary(ctx context.Context, libraryID string, offset, limit int) ([]model.Media, int64, error) {
	return r.ListByLibraryFiltered(ctx, libraryID, offset, limit, MediaQueryFilter{IncludeNSFW: true})
}

func (r *MediaRepository) ListByLibraryFiltered(ctx context.Context, libraryID string, offset, limit int, filter MediaQueryFilter) ([]model.Media, int64, error) {
	return r.ListByLibrariesFiltered(ctx, []string{libraryID}, offset, limit, filter)
}

func (r *MediaRepository) ListByLibrariesFiltered(ctx context.Context, libraryIDs []string, offset, limit int, filter MediaQueryFilter) ([]model.Media, int64, error) {
	views, total, err := r.viewRepository().ListByLibrariesFiltered(ctx, libraryIDs, offset, limit, filter)
	if err != nil {
		return nil, 0, err
	}
	return mediaViewsToMedia(views), total, nil
}

// DeleteByLibrary purges all media tied to a library.
func (r *MediaRepository) DeleteByLibrary(ctx context.Context, libraryID string) error {
	// FTS 行由 media 表上的触发器同步清理（软删/硬删都覆盖）。
	return r.db.WithContext(ctx).Where("library_id = ?", libraryID).Delete(&model.Media{}).Error
}

func (r *MediaRepository) DeleteByLibraryRoot(ctx context.Context, libraryID, rootID string) error {
	return r.db.WithContext(ctx).
		Where("library_id = ? AND library_root_id = ?", libraryID, rootID).
		Delete(&model.Media{}).Error
}

// PurgeByLibrary permanently removes media tied to a library. Used for virtual
// cloud mounts where "remove mount" must not populate the recycle bin.
func (r *MediaRepository) PurgeByLibrary(ctx context.Context, libraryID string) error {
	return r.db.WithContext(ctx).Unscoped().Where("library_id = ?", libraryID).Delete(&model.Media{}).Error
}
