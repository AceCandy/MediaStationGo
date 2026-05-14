// Package repository implements a thin GORM-based data-access layer over the
// types declared in internal/model. Each method takes a context.Context so we
// can plug in cancellation / tracing later.
//
// Repositories are intentionally narrow: they only know how to persist data,
// not how to interpret it. Domain logic lives in internal/service.
package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Container is the registry of all repositories injected into services.
type Container struct {
	DB              *gorm.DB
	User            *UserRepository
	Library         *LibraryRepository
	Media           *MediaRepository
	Series          *SeriesRepository
	History         *HistoryRepository
	Favorite        *FavoriteRepository
	Playlist        *PlaylistRepository
	Download        *DownloadRepository
	Subscription    *SubscriptionRepository
	Setting         *SettingRepository
	Log             *AccessLogRepository
}

// New wires every repository to a single *gorm.DB.
func New(db *gorm.DB) *Container {
	return &Container{
		DB:           db,
		User:         &UserRepository{db: db},
		Library:      &LibraryRepository{db: db},
		Media:        &MediaRepository{db: db},
		Series:       &SeriesRepository{db: db},
		History:      &HistoryRepository{db: db},
		Favorite:     &FavoriteRepository{db: db},
		Playlist:     &PlaylistRepository{db: db},
		Download:     &DownloadRepository{db: db},
		Subscription: &SubscriptionRepository{db: db},
		Setting:      &SettingRepository{db: db},
		Log:          &AccessLogRepository{db: db},
	}
}

// ─── User ────────────────────────────────────────────────────────────────────

// UserRepository persists model.User records.
type UserRepository struct{ db *gorm.DB }

// Create inserts a new user. Caller must pre-hash the password.
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// FindByUsername returns the user matching username, or (nil, nil) when absent.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID returns the user with the matching primary key, or (nil, nil).
func (r *UserRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CountAdmins returns the number of users that hold the admin role.
func (r *UserRepository) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("role = ?", "admin").Count(&n).Error
	return n, err
}

// List returns all users ordered by creation time desc.
func (r *UserRepository) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&users).Error
	return users, err
}

// UpdatePassword sets a new password hash and clears ForcePasswordReset.
func (r *UserRepository) UpdatePassword(ctx context.Context, id, hash string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"password_hash": hash, "force_password_reset": false}).Error
}

// TouchLogin updates the last login timestamp.
func (r *UserRepository) TouchLogin(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("last_login_at", &now).Error
}

// Delete removes a user (soft-delete via gorm.DeletedAt).
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.User{}, "id = ?", id).Error
}

// ─── Library ─────────────────────────────────────────────────────────────────

// LibraryRepository persists model.Library records.
type LibraryRepository struct{ db *gorm.DB }

// Create persists a new library row.
func (r *LibraryRepository) Create(ctx context.Context, l *model.Library) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// List returns all enabled+disabled libraries.
func (r *LibraryRepository) List(ctx context.Context) ([]model.Library, error) {
	var ls []model.Library
	err := r.db.WithContext(ctx).Order("created_at asc").Find(&ls).Error
	return ls, err
}

// FindByID returns the library, or (nil, nil) when missing.
func (r *LibraryRepository) FindByID(ctx context.Context, id string) (*model.Library, error) {
	var l model.Library
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Delete removes a library and (soft) cascades to its media via repository
// callers — we do not run CASCADE here to keep this method narrow.
func (r *LibraryRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.Library{}, "id = ?", id).Error
}

// ─── Media ───────────────────────────────────────────────────────────────────

// MediaRepository persists model.Media records.
type MediaRepository struct{ db *gorm.DB }

// Upsert inserts or updates a media row keyed by Path (unique index).
func (r *MediaRepository) Upsert(ctx context.Context, m *model.Media) error {
	return r.db.WithContext(ctx).Where("path = ?", m.Path).
		Assign(*m).FirstOrCreate(m).Error
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
	var items []model.Media
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Media{}).Where("library_id = ?", libraryID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at desc").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

// Search runs a LIKE search against the title field. Empty query returns the
// most recently added items.
func (r *MediaRepository) Search(ctx context.Context, query string, limit int) ([]model.Media, error) {
	var items []model.Media
	q := r.db.WithContext(ctx).Model(&model.Media{}).Limit(limit)
	if query != "" {
		like := "%" + query + "%"
		q = q.Where("title LIKE ? OR original_name LIKE ?", like, like)
	}
	err := q.Order("created_at desc").Find(&items).Error
	return items, err
}

// DeleteByLibrary purges all media tied to a library.
func (r *MediaRepository) DeleteByLibrary(ctx context.Context, libraryID string) error {
	return r.db.WithContext(ctx).Where("library_id = ?", libraryID).Delete(&model.Media{}).Error
}

// ─── Series ──────────────────────────────────────────────────────────────────

// SeriesRepository persists model.Series records.
type SeriesRepository struct{ db *gorm.DB }

// FindByID returns the series or (nil, nil).
func (r *SeriesRepository) FindByID(ctx context.Context, id string) (*model.Series, error) {
	var s model.Series
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// List returns all series (ordered by title).
func (r *SeriesRepository) List(ctx context.Context) ([]model.Series, error) {
	var s []model.Series
	err := r.db.WithContext(ctx).Order("title asc").Find(&s).Error
	return s, err
}

// ─── Playback History ────────────────────────────────────────────────────────

// HistoryRepository persists model.PlaybackHistory entries. The application
// upserts on (UserID, MediaID) so resume always reads the latest position.
type HistoryRepository struct{ db *gorm.DB }

// Upsert atomically inserts/updates the resume position.
func (r *HistoryRepository) Upsert(ctx context.Context, h *model.PlaybackHistory) error {
	var existing model.PlaybackHistory
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND media_id = ?", h.UserID, h.MediaID).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(h).Error
	}
	if err != nil {
		return err
	}
	existing.PositionMs = h.PositionMs
	existing.DurationMs = h.DurationMs
	existing.WatchedAt = h.WatchedAt
	existing.Completed = h.Completed
	return r.db.WithContext(ctx).Save(&existing).Error
}

// ListByUser returns the most recent history rows for the user.
func (r *HistoryRepository) ListByUser(ctx context.Context, userID string, limit int) ([]model.PlaybackHistory, error) {
	var rows []model.PlaybackHistory
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("watched_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// ─── Favorite ────────────────────────────────────────────────────────────────

// FavoriteRepository persists model.Favorite records.
type FavoriteRepository struct{ db *gorm.DB }

// Toggle flips the favourite flag for (user, media). Returns the new state.
func (r *FavoriteRepository) Toggle(ctx context.Context, userID, mediaID string) (bool, error) {
	var f model.Favorite
	err := r.db.WithContext(ctx).Where("user_id = ? AND media_id = ?", userID, mediaID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fav := model.Favorite{UserID: userID, MediaID: mediaID}
		return true, r.db.WithContext(ctx).Create(&fav).Error
	}
	if err != nil {
		return false, err
	}
	return false, r.db.WithContext(ctx).Delete(&f).Error
}

// ListByUser returns all favourite media IDs for a user.
func (r *FavoriteRepository) ListByUser(ctx context.Context, userID string) ([]model.Favorite, error) {
	var rows []model.Favorite
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error
	return rows, err
}

// ─── Playlist ────────────────────────────────────────────────────────────────

// PlaylistRepository persists model.Playlist + PlaylistItem.
type PlaylistRepository struct{ db *gorm.DB }

// Create inserts a new playlist.
func (r *PlaylistRepository) Create(ctx context.Context, p *model.Playlist) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// ListByUser returns playlists owned by a user.
func (r *PlaylistRepository) ListByUser(ctx context.Context, userID string) ([]model.Playlist, error) {
	var rows []model.Playlist
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ─── Download ────────────────────────────────────────────────────────────────

// DownloadRepository persists model.DownloadTask records.
type DownloadRepository struct{ db *gorm.DB }

// Create inserts a new download task.
func (r *DownloadRepository) Create(ctx context.Context, t *model.DownloadTask) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// List returns all download tasks (admin view).
func (r *DownloadRepository) List(ctx context.Context) ([]model.DownloadTask, error) {
	var rows []model.DownloadTask
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ─── Subscription ────────────────────────────────────────────────────────────

// SubscriptionRepository persists model.Subscription records.
type SubscriptionRepository struct{ db *gorm.DB }

// Create inserts a new subscription rule.
func (r *SubscriptionRepository) Create(ctx context.Context, s *model.Subscription) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// List returns all subscription rules.
func (r *SubscriptionRepository) List(ctx context.Context) ([]model.Subscription, error) {
	var rows []model.Subscription
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ─── Setting ─────────────────────────────────────────────────────────────────

// SettingRepository persists key/value preferences.
type SettingRepository struct{ db *gorm.DB }

// Get returns the value or empty string when absent.
func (r *SettingRepository) Get(ctx context.Context, key string) (string, error) {
	var s model.Setting
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return s.Value, err
}

// Set upserts a setting value.
func (r *SettingRepository) Set(ctx context.Context, key, value string) error {
	s := model.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Save(&s).Error
}

// All returns every key/value pair (used by the admin UI).
func (r *SettingRepository) All(ctx context.Context) ([]model.Setting, error) {
	var rows []model.Setting
	err := r.db.WithContext(ctx).Find(&rows).Error
	return rows, err
}

// ─── Access Log ──────────────────────────────────────────────────────────────

// AccessLogRepository persists model.AccessLog records.
type AccessLogRepository struct{ db *gorm.DB }

// Create inserts one structured audit-trail entry.
func (r *AccessLogRepository) Create(ctx context.Context, l *model.AccessLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// Recent returns the latest access-log entries (admin Activity panel).
func (r *AccessLogRepository) Recent(ctx context.Context, limit int) ([]model.AccessLog, error) {
	var rows []model.AccessLog
	err := r.db.WithContext(ctx).Order("created_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}
