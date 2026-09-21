package repository

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/clause"
)

// MarkPreviousEpisodes 补标同一季内可见且有文件的前集；已有完成记录保持原样，不产生播放事件。
func (r *HistoryRepository) MarkPreviousEpisodes(ctx context.Context, userID, metadataID string, filter MediaQueryFilter, watchedAt time.Time) error {
	var rows []*model.PlaybackHistory
	q := r.db.WithContext(ctx).Table("media AS m").
		Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id AND mi.kind = 'episode'").
		Joins("JOIN metadata_items AS current ON current.id = ? AND current.kind = 'episode' AND current.parent_id = mi.parent_id", metadataID).
		Joins("JOIN metadata_items AS season ON season.id = mi.parent_id AND season.kind = 'season'").
		Joins("LEFT JOIN media_probe_metadata AS probe ON probe.media_id = m.id").
		Joins("LEFT JOIN playback_histories AS history ON history.metadata_id = mi.id AND history.user_id = ? AND history.deleted_at IS NULL", userID).
		Where("mi.episode_num > 0 AND mi.episode_num < current.episode_num AND COALESCE(history.completed, FALSE) = FALSE").
		Select("DISTINCT ON (mi.id) mi.id AS metadata_id, m.id AS media_id, COALESCE(NULLIF(history.duration_ms, 0), probe.duration_ms, 0) AS duration_ms").
		Order("mi.id, m.created_at DESC, m.id DESC")
	if err := applyMediaViewFilter(q, filter).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		row.UserID = userID
		row.PositionMs = row.DurationMs
		row.WatchedAt = watchedAt
		row.Completed = true
	}
	return r.upsertBatch(ctx, rows, clause.Where{Exprs: []clause.Expression{clause.Eq{
		Column: clause.Column{Table: "playback_histories", Name: "completed"}, Value: false,
	}}})
}

// MarkPreviousEpisodes 按来源作品隔离红果季，避免人工聚合把其他季一并补标。
func (r *HongGuoRepository) MarkPreviousEpisodes(ctx context.Context, userID, sourceID string, episode int, filter MediaQueryFilter) error {
	var rows []model.HongGuoUserState
	q := r.db.WithContext(ctx).Table("media AS m").
		Joins("JOIN hongguo_media_bindings AS b ON b.media_id = m.id").
		Joins("JOIN hongguo_works AS w ON w.id = b.work_id AND w.kind = 'series'").
		Joins("JOIN hongguo_episodes AS ep ON ep.id = b.episode_id AND ep.work_id = w.id").
		Joins("LEFT JOIN media_probe_metadata AS probe ON probe.media_id = m.id").
		Joins("LEFT JOIN hongguo_user_states AS state ON state.source_id = w.source_id AND state.episode_number = ep.number AND state.user_id = ?", userID).
		Where("m.catalog_source = ? AND w.source_id = ? AND ep.number > 0 AND ep.number < ? AND COALESCE(state.completed, FALSE) = FALSE", model.TaskSystemHongGuo, sourceID, episode).
		Select("DISTINCT ON (ep.number) ep.number AS episode_number, m.id AS media_id, COALESCE(NULLIF(state.duration_ms, 0), probe.duration_ms, 0) AS duration_ms").
		Order("ep.number, m.created_at DESC, m.id DESC")
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if err := q.Scan(&rows).Error; err != nil || len(rows) == 0 {
		return err
	}
	now := time.Now()
	for i := range rows {
		rows[i].UserID = userID
		rows[i].SourceID = sourceID
		rows[i].PositionMs = rows[i].DurationMs
		rows[i].Completed = true
		rows[i].WatchedAt = &now
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}},
		DoUpdates: clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "completed", "watched_at", "updated_at"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Table: "hongguo_user_states", Name: "completed"}, Value: false}}},
	}).CreateInBatches(&rows, 200).Error
}
