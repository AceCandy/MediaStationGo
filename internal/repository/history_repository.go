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

// ListByUserMetadataIDs 读取已由调用方过滤可见性的分集进度，不截断为最近若干条。
func (r *HistoryRepository) ListByUserMetadataIDs(ctx context.Context, userID string, metadataIDs []string, filters ...MediaQueryFilter) ([]model.PlaybackHistory, error) {
	rows := []model.PlaybackHistory{}
	filter := MediaQueryFilter{IncludeNSFW: true}
	if len(filters) > 0 {
		filter = filters[0]
	}
	if len(metadataIDs) == 0 {
		return rows, nil
	}
	localIDs, ordinaryIDs := []string{}, []string{}
	for _, id := range metadataIDs {
		if strings.HasPrefix(id, "nfo-") {
			localIDs = append(localIDs, strings.TrimPrefix(id, "nfo-"))
		} else {
			ordinaryIDs = append(ordinaryIDs, id)
		}
	}
	if len(localIDs) > 0 {
		var local []model.PlaybackHistory
		if err := r.db.WithContext(ctx).Table("(?) AS state", PlaybackStates(ctx, r.db, "nfo", userID, filter)).Where("item_id IN ? AND watched_at IS NOT NULL", localIDs).
			Select("'nfo-' || item_id AS id, 'nfo-' || item_id AS metadata_id,user_id,media_id,position_ms,duration_ms,completed,watched_at").Scan(&local).Error; err != nil {
			return nil, err
		}
		ordinary, err := r.ListByUserMetadataIDs(ctx, userID, ordinaryIDs, filter)
		return append(ordinary, local...), err
	}
	err := r.db.WithContext(ctx).Table("(?) AS history", PlaybackStates(ctx, r.db, "legacy", userID, filter)).Where("metadata_id = ANY(?)", &metadataIDs).Order("watched_at DESC").Scan(&rows).Error
	return rows, err
}

// Upsert atomically inserts/updates the resume position.
func (r *HistoryRepository) Upsert(ctx context.Context, h *model.PlaybackHistory) error {
	return r.UpsertBatch(ctx, []*model.PlaybackHistory{h})
}

// UpsertProgress 保留已看标记，同时独立更新当前播放的续播位置。
func (r *HistoryRepository) UpsertProgress(ctx context.Context, h *model.PlaybackHistory) error {
	if strings.TrimSpace(h.MetadataID) == "" {
		return errors.New("metadata id is required")
	}
	h.ResumePositionMs = ResumePosition(h.PositionMs, h.Completed)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "user_id"}, {Name: "metadata_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil}}},
		DoUpdates:   progressUpdates("playback_histories"),
	}).Create(h).Error
}

// UpsertBatch 按用户和作品身份批量保存历史，调用方须先按作品去重。
func (r *HistoryRepository) UpsertBatch(ctx context.Context, rows []*model.PlaybackHistory) error {
	return r.upsertBatch(ctx, rows, clause.Where{})
}

func (r *HistoryRepository) upsertBatch(ctx context.Context, rows []*model.PlaybackHistory, condition clause.Where) error {
	if len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.MetadataID) == "" {
			return errors.New("metadata id is required")
		}
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "metadata_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil},
		}},
		DoUpdates: clause.AssignmentColumns([]string{
			"media_id", "position_ms", "duration_ms", "resume_position_ms", "watched_at", "completed", "updated_at",
		}),
		Where: condition,
	}).CreateInBatches(rows, 200).Error
}

// ListByUser returns the most recent history rows for the user.
func (r *HistoryRepository) ListByUser(ctx context.Context, userID string, limit int) ([]model.PlaybackHistory, error) {
	var rows []model.PlaybackHistory
	err := r.db.WithContext(ctx).Table("(?) AS history", PlaybackStates(ctx, r.db, "legacy", userID, MediaQueryFilter{IncludeNSFW: true})).
		Order("watched_at desc").Limit(limit).Scan(&rows).Error
	return rows, err
}

// ListByUserFiltered 在媒体可见性过滤后再应用历史分页。
func (r *HistoryRepository) ListByUserFiltered(ctx context.Context, userID string, limit int, completed *bool, filter MediaQueryFilter) ([]model.PlaybackHistory, error) {
	q := r.db.WithContext(ctx).
		Table("(?) AS ph", PlaybackStates(ctx, r.db, "legacy", userID, filter)).
		Joins("JOIN media AS m ON m.metadata_id = ph.metadata_id").
		Joins("JOIN metadata_items AS mi ON mi.id = ph.metadata_id").
		Where("ph.deleted_at IS NULL AND ph.user_id = ?", userID)
	q = applyMediaViewFilter(q, filter)
	if completed != nil && *completed {
		q = q.Where("ph.completed = ?", *completed)
	}
	if completed != nil && !*completed {
		q = q.Where("ph.position_ms >= ?", int64(20_000))
		groupKey := "CASE WHEN mi.kind = 'episode' AND season.kind = 'season' THEN season.parent_id ELSE ph.metadata_id END"
		grouped := q.Joins("LEFT JOIN metadata_items AS season ON season.id = mi.parent_id AND mi.kind = 'episode' AND season.kind = 'season'").
			Select("DISTINCT ON (" + groupKey + ") ph.*").
			Order(groupKey + ", ph.watched_at DESC, ph.id DESC")
		var rows []model.PlaybackHistory
		err := r.db.WithContext(ctx).Table("(?) AS grouped_history", grouped).
			Order("watched_at DESC, id DESC").Limit(limit).Scan(&rows).Error
		return rows, err
	}
	var rows []model.PlaybackHistory
	err := q.Select("DISTINCT ph.*").Order("ph.watched_at desc").Limit(limit).Scan(&rows).Error
	return rows, err
}
