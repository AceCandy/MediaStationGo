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
	SearchMetadataIDs(ctx context.Context, query string, offset, limit int, filter MetadataSearchFilter) ([]string, int64, error)
}

type MediaSearchSyncBackend interface {
	MediaSearchBackend
	PrepareMetadataIndex(ctx context.Context) (string, error)
	IndexMetadata(ctx context.Context, index string, rows []MetadataSearchDocument) error
	ActivateMetadataIndex(ctx context.Context, index string) error
	DiscardMetadataIndex(ctx context.Context, index string) error
	UpsertMetadata(ctx context.Context, row MetadataSearchDocument) error
	DeleteMetadata(ctx context.Context, id string) error
	DeleteMetadataFromIndex(ctx context.Context, index, id string) error
}

func (r *MediaRepository) SetSearchBackend(backend MediaSearchBackend) {
	if r != nil && r.viewRepository() != nil {
		r.viewRepository().SetSearchBackend(backend)
	}
}

// MediaQueryFilter is applied to user-facing media queries so NSFW items and
// profile-restricted libraries are filtered in SQL instead of only in React.
type MediaQueryFilter struct {
	IncludeNSFW         bool
	AllowedLibraryIDs   []string
	HiddenLibraryIDs    []string
	MissingPoster       bool
	MissingChineseTitle bool
}

type MetadataSearchFields string

const (
	MetadataSearchFieldsWeb   MetadataSearchFields = "web"
	MetadataSearchFieldsTitle MetadataSearchFields = "title"
)

// MetadataSearchFilter describes only candidate-work search constraints.
// Response projection and playback-version loading remain consumer-owned.
type MetadataSearchFilter struct {
	MediaQueryFilter
	Fields            MetadataSearchFields
	Kinds             []string
	LibraryRestricted bool
	VisibleLibraryIDs []string
	PersonIDs         []string
	FavoriteUserID    string
	ResumableUserID   string
	ForcePostgres     bool
	// CandidateIDs 在候选截断前限定独立资料来源的可见逻辑身份；nil 表示不限定。
	CandidateIDs []string
}

// MetadataSearchDocument is the complete OpenSearch projection for one
// searchable top-level work. LibraryIDs is the only Media-derived field.
type MetadataSearchDocument struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	OriginalName string   `json:"original_name"`
	Overview     string   `json:"overview"`
	Genres       string   `json:"genres"`
	NSFW         bool     `json:"nsfw"`
	LibraryIDs   []string `json:"library_ids" gorm:"-"`
}

func (r *MediaRepository) refreshMetadataBestEffort(ctx context.Context, metadataIDs ...string) {
	viewRepo := r.viewRepository()
	if viewRepo == nil {
		return
	}
	viewRepo.RefreshMetadataIDs(ctx, metadataIDs...)
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

// FindByPath returns the active media row for a scanner source path.
func (r *MediaRepository) FindByPath(ctx context.Context, path string) (*model.Media, error) {
	var m model.Media
	err := r.db.WithContext(ctx).Where("path = ?", path).First(&m).Error
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
	var metadataIDs []string
	if err := r.db.WithContext(ctx).Model(&model.Media{}).Where("library_id = ?", libraryID).Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("library_id = ?", libraryID).Delete(&model.Media{}).Error; err != nil {
		return err
	}
	r.refreshMetadataBestEffort(ctx, metadataIDs...)
	return nil
}

func (r *MediaRepository) DeleteByLibraryRoot(ctx context.Context, libraryID, rootID string) error {
	var metadataIDs []string
	q := r.db.WithContext(ctx).Model(&model.Media{}).Where("library_id = ? AND library_root_id = ?", libraryID, rootID)
	if err := q.Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).
		Where("library_id = ? AND library_root_id = ?", libraryID, rootID).
		Delete(&model.Media{}).Error; err != nil {
		return err
	}
	r.refreshMetadataBestEffort(ctx, metadataIDs...)
	return nil
}
