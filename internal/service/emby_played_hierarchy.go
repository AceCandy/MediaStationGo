package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// containerEpisodeScope 让整剧/季的写入和汇总使用同一组可见、有文件的分集。
func (e *EmbyService) containerEpisodeScope(ctx context.Context, userID string, ids []string) *gorm.DB {
	q := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), userID)
	return seriesScopeQuery(q).Where("emby_metadata.kind = 'episode'").
		Where("scope_season.id IN ? OR scope_series.id IN ?", ids, ids)
}

// playedForContainers 批量汇总当前页；不使用旧的整剧/季历史覆盖单集状态。
func (e *EmbyService) playedForContainers(ctx context.Context, userID string, ids []string) map[string]bool {
	result := make(map[string]bool, len(ids))
	if strings.TrimSpace(userID) == "" || len(ids) == 0 {
		return result
	}
	var rows []struct {
		ID     string
		Played bool
	}
	q := e.containerEpisodeScope(ctx, userID, ids).
		Joins("CROSS JOIN LATERAL (VALUES (scope_season.id), (scope_series.id)) AS container(id)").
		Joins("LEFT JOIN playback_histories AS history ON history.metadata_id = media.metadata_id AND history.user_id = ? AND history.deleted_at IS NULL", userID).
		Where("container.id IN ?", ids).
		Select("container.id, BOOL_AND(COALESCE(history.completed, FALSE)) AS played").Group("container.id")
	if err := q.Scan(&rows).Error; err == nil {
		for _, row := range rows {
			result[row.ID] = row.Played
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
