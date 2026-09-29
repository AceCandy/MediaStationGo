package service

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// containerEpisodeScope 让整剧/季的写入和汇总使用同一组可见、有文件的分集。
func (e *EmbyService) containerEpisodeScope(ctx context.Context, userID string, ids []string) *gorm.DB {
	q := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), userID)
	// 先收紧季范围，避免跨表 OR 让规划器先连接整库的季与剧。
	seasons := e.repo.DB.WithContext(ctx).Table("metadata_items").Select("id,parent_id").
		Where("kind = 'season' AND (id IN ? OR parent_id IN ?)", ids, ids)
	return q.Joins("JOIN (? OFFSET 0) AS scope_season ON scope_season.id = emby_metadata.parent_id", seasons).
		Joins("JOIN metadata_items AS scope_series ON scope_series.id = scope_season.parent_id AND scope_series.kind = 'series'").
		Where("emby_metadata.kind = 'episode'").
		Where("scope_season.id IN ? OR scope_series.id IN ?", ids, ids)
}

type embyContainerPlayback struct {
	ID                string
	Played            bool
	UnplayedItemCount int
}

// playbackForContainers 批量汇总当前页可见分集；多版本去重，旧容器历史不覆盖单集状态。
func (e *EmbyService) playbackForContainers(ctx context.Context, userID string, ids []string) map[string]embyContainerPlayback {
	result := make(map[string]embyContainerPlayback, len(ids))
	if len(ids) == 0 {
		return result
	}
	var rows []embyContainerPlayback
	q := e.containerEpisodeScope(ctx, userID, ids).
		Joins("CROSS JOIN LATERAL (VALUES (scope_season.id), (scope_series.id)) AS container(id)").
		Joins("LEFT JOIN LATERAL (? OFFSET 0) AS history ON TRUE", repository.CompletedPlaybackStates(ctx, e.repo.DB, "legacy", userID, e.mediaQueryFilter(ctx, userID)).Where("metadata_id = media.metadata_id")).
		Where("container.id IN ?", ids).
		Select(`container.id, BOOL_AND(history.metadata_id IS NOT NULL) AS played,
 COUNT(DISTINCT media.metadata_id) FILTER (WHERE history.metadata_id IS NULL) AS unplayed_item_count`).Group("container.id")
	if err := q.Scan(&rows).Error; err == nil {
		for _, row := range rows {
			result[row.ID] = row
		}
	}
	return result
}

// markContainerPlayed 在一个事务内更新全部可见分集，不生成真实播放事件。
func (e *EmbyService) markContainerPlayed(ctx context.Context, userID, itemID string, played bool) error {
	scope := e.containerEpisodeScope(ctx, userID, []string{itemID})
	return e.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			MetadataID string
			MediaID    string
			DurationMs int64
		}
		query := scope.Select("DISTINCT ON (media.metadata_id) media.metadata_id, media.id AS media_id, COALESCE(probe.duration_ms, 0) AS duration_ms").
			Joins("LEFT JOIN media_probe_metadata AS probe ON probe.media_id = media.id").
			Order("media.metadata_id, media.created_at DESC, media.id DESC")
		if err := tx.Table("(?) AS episodes", query).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return errors.New("media not found")
		}
		if !played {
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.MetadataID)
			}
			return tx.Where("user_id = ? AND metadata_id IN ?", userID, ids).Delete(&model.PlaybackHistory{}).Error
		}
		histories := make([]*model.PlaybackHistory, 0, len(rows))
		now := time.Now()
		for _, row := range rows {
			histories = append(histories, &model.PlaybackHistory{
				UserID: userID, MetadataID: row.MetadataID, MediaID: row.MediaID,
				PositionMs: row.DurationMs, DurationMs: row.DurationMs, WatchedAt: now, Completed: true,
			})
		}
		return repository.New(tx).History.UpsertBatch(ctx, histories)
	})
}
