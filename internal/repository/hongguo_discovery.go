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
	now := time.Now().UTC()
	rows := make([]model.HongGuoDiscovery, 0, len(works))
	for _, work := range works {
		if !hongguo.ValidID(work.SourceID) {
			return errors.New("红果发现 ID 无效")
		}
		rows = append(rows, model.HongGuoDiscovery{SourceID: work.SourceID, SourceCategory: state.Category, Title: work.Title, Overview: work.Overview, CoverURL: work.CoverURL, EpisodeCount: work.EpisodeCount, UpdateText: work.UpdateText})
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"source_category", "title", "overview", "cover_url", "episode_count", "update_text", "updated_at"})}).CreateInBatches(&rows, 100).Error; err != nil {
				return err
			}
			for _, work := range works {
				if err := saveHongGuoArtwork(tx, &work.SourceID, nil, nil, work.CoverURL, now); err != nil {
					return err
				}
			}
		}
		return (&HongGuoRepository{db: tx}).SaveSyncState(ctx, state)
	})
}

// ReplaceRank 仅在官网榜单完整读取后替换名次，并把榜单摘要纳入既有详情补齐队列。
func (r *HongGuoRepository) ReplaceRank(ctx context.Context, rankKey, sourceCategory string, works []hongguo.Work) error {
	if !hongguo.ValidRank(rankKey) || len(works) == 0 {
		return errors.New("红果榜单无效")
	}
	discoveries := make([]model.HongGuoDiscovery, 0, len(works))
	entries := make([]model.HongGuoRankEntry, 0, len(works))
	for i, work := range works {
		if !hongguo.ValidID(work.SourceID) {
			return errors.New("红果榜单作品 ID 无效")
		}
		discoveries = append(discoveries, model.HongGuoDiscovery{SourceID: work.SourceID, SourceCategory: sourceCategory, Title: work.Title, CoverURL: work.CoverURL})
		entries = append(entries, model.HongGuoRankEntry{RankKey: rankKey, SourceID: work.SourceID, Position: i + 1})
	}
	updates := map[string]any{"title": gorm.Expr("EXCLUDED.title"), "cover_url": gorm.Expr("EXCLUDED.cover_url"), "updated_at": gorm.Expr("EXCLUDED.updated_at")}
	if sourceCategory != "" {
		updates["source_category"] = gorm.Expr("CASE WHEN hongguo_discoveries.source_category = '' THEN EXCLUDED.source_category ELSE hongguo_discoveries.source_category END")
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates)}).CreateInBatches(&discoveries, 100).Error; err != nil {
			return err
		}
		for _, work := range works {
			if err := saveHongGuoArtwork(tx, &work.SourceID, nil, nil, work.CoverURL, now); err != nil {
				return err
			}
		}
		if err := tx.Where("rank_key = ?", rankKey).Delete(&model.HongGuoRankEntry{}).Error; err != nil {
			return err
		}
		return tx.CreateInBatches(&entries, 100).Error
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
