package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveDiscoveryPage 将整页摘要与下一页检查点一起提交；失败或取消不会跳过未保存的页。
// 摘要只写发现表，不能覆盖详情、人物、图片或现有文件绑定。
func (r *HongGuoRepository) SaveDiscoveryPage(ctx context.Context, works []hongguo.Work, state model.HongGuoSyncState) error {
	rows := make([]model.HongGuoDiscovery, 0, len(works))
	for _, work := range works {
		if !hongguo.ValidID(work.SourceID) {
			return errors.New("红果发现 ID 无效")
		}
		rows = append(rows, model.HongGuoDiscovery{SourceID: work.SourceID, Title: work.Title, Overview: work.Overview, CoverURL: work.CoverURL, EpisodeCount: work.EpisodeCount, UpdateText: work.UpdateText})
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"title", "overview", "cover_url", "episode_count", "update_text", "updated_at"})}).CreateInBatches(&rows, 100).Error; err != nil {
				return err
			}
		}
		return (&HongGuoRepository{db: tx}).SaveSyncState(ctx, state)
	})
}

// PendingDiscoveries 从业务表恢复待补齐项，详情成功后自然退出；失败项只经冷却队列重试。
func (r *HongGuoRepository) PendingDiscoveries(ctx context.Context, after string, cutoff time.Time) ([]model.HongGuoDiscovery, error) {
	rows := []model.HongGuoDiscovery{}
	err := r.db.WithContext(ctx).Where("source_id > ? AND created_at <= ?", after, cutoff).
		Where("NOT EXISTS (SELECT 1 FROM hongguo_works w WHERE w.source_id = hongguo_discoveries.source_id)").
		Where("NOT EXISTS (SELECT 1 FROM hongguo_sync_failures f WHERE f.source_id = hongguo_discoveries.source_id)").
		Order("source_id").Limit(100).Find(&rows).Error
	return rows, err
}
