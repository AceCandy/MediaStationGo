package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// HistoryRepository persists model.PlaybackHistory entries by metadata identity.
type HistoryRepository struct{ db *gorm.DB }

// Upsert atomically inserts/updates the resume position.
func (r *HistoryRepository) Upsert(ctx context.Context, h *model.PlaybackHistory) error {
	if h == nil || strings.TrimSpace(h.MetadataID) == "" {
		return errors.New("metadata id is required")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "metadata_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil},
		}},
		DoUpdates: clause.AssignmentColumns([]string{
			"media_id", "position_ms", "duration_ms", "watched_at", "completed", "updated_at",
		}),
	}).Create(h).Error
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
		Joins("JOIN media AS m ON m.metadata_id = ph.metadata_id").
		Joins("JOIN metadata_items AS mi ON mi.id = ph.metadata_id").
		Where("ph.deleted_at IS NULL AND ph.user_id = ?", userID)
	q = applyMediaViewFilter(q, filter)
	if completed != nil {
		q = q.Where("ph.completed = ?", *completed)
	}
	if completed != nil && !*completed {
		q = q.Where("ph.position_ms >= ?", int64(20_000))
	}
	var rows []model.PlaybackHistory
	err := q.Select("DISTINCT ph.*").Order("ph.watched_at desc").Limit(limit).Scan(&rows).Error
	return rows, err
}
