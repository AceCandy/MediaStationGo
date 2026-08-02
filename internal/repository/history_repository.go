package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// HistoryRepository persists model.PlaybackHistory entries by metadata identity.
type HistoryRepository struct{ db *gorm.DB }

// Upsert atomically inserts/updates the resume position.
func (r *HistoryRepository) Upsert(ctx context.Context, h *model.PlaybackHistory) error {
	if h == nil || strings.TrimSpace(h.MetadataID) == "" {
		return errors.New("metadata id is required")
	}
	var existing model.PlaybackHistory
	q := r.db.WithContext(ctx).Where("user_id = ? AND metadata_id = ?", h.UserID, h.MetadataID)
	err := q.First(&existing).Error
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
	existing.MetadataID = h.MetadataID
	existing.MediaID = h.MediaID
	return r.db.WithContext(ctx).Save(&existing).Error
}

// ListByUser returns the most recent history rows for the user.
func (r *HistoryRepository) ListByUser(ctx context.Context, userID string, limit int) ([]model.PlaybackHistory, error) {
	var rows []model.PlaybackHistory
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("watched_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListByUserFiltered 在媒体可见性过滤后再应用历史分页。
func (r *HistoryRepository) ListByUserFiltered(ctx context.Context, userID string, limit int, completed *bool, filter MediaQueryFilter) ([]model.PlaybackHistory, error) {
	q := r.db.WithContext(ctx).
		Table("playback_histories AS ph").
		Joins("JOIN media AS m ON m.metadata_id = ph.metadata_id AND m.deleted_at IS NULL").
		Joins("JOIN metadata_items AS mi ON mi.id = ph.metadata_id AND mi.deleted_at IS NULL").
		Where("ph.deleted_at IS NULL AND ph.user_id = ?", userID)
	q = applyMediaViewFilter(q, filter)
	if completed != nil {
		q = q.Where("ph.completed = ?", *completed)
	}
	var rows []model.PlaybackHistory
	err := q.Select("DISTINCT ph.*").Order("ph.watched_at desc").Limit(limit).Scan(&rows).Error
	return rows, err
}
