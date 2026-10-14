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
// 摘要写入作品表，不能覆盖已补齐详情或现有文件绑定。
func (r *HongGuoRepository) SaveDiscoveryPage(ctx context.Context, works []hongguo.Work, state model.HongGuoSyncState) error {
	_, err := r.SaveDiscoveryPageWithNew(ctx, works, state)
	return err
}

// SaveDiscoveryPageWithNew 返回事务内首次发现且尚无正式资料的作品 ID。
func (r *HongGuoRepository) SaveDiscoveryPageWithNew(ctx context.Context, works []hongguo.Work, state model.HongGuoSyncState) ([]string, error) {
	now := time.Now().UTC()
	rows := make([]model.HongGuoWork, 0, len(works))
	for _, work := range works {
		if !hongguo.ValidID(work.SourceID) {
			return nil, errors.New("红果发现 ID 无效")
		}
		rows = append(rows, model.HongGuoWork{SummaryOnly: true, Kind: model.MetadataKindSeries, SourceID: work.SourceID, SourceCategory: state.Category, Title: work.Title, Overview: work.Overview, EpisodeCount: work.EpisodeCount, UpdateText: work.UpdateText})
	}
	var newIDs []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(rows) > 0 {
			var err error
			newIDs, err = insertHongGuoDiscoveries(tx, rows)
			if err != nil {
				return err
			}
			updates := map[string]any{}
			for _, field := range []string{"title", "overview", "episode_count", "update_text", "updated_at"} {
				updates[field] = gorm.Expr("CASE WHEN hongguo_works.summary_only THEN EXCLUDED." + field + " ELSE hongguo_works." + field + " END")
			}
			updates["source_category"] = gorm.Expr("CASE WHEN hongguo_works.summary_only OR hongguo_works.source_category = '' THEN EXCLUDED.source_category ELSE hongguo_works.source_category END")
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates)}).CreateInBatches(&rows, 100).Error; err != nil {
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
	return newIDs, err
}

// ReplaceRank 仅在官网榜单完整读取后替换名次，并把榜单摘要纳入既有详情补齐队列。
func (r *HongGuoRepository) ReplaceRank(ctx context.Context, rankKey, sourceCategory string, works []hongguo.Work) error {
	_, err := r.ReplaceRankWithNew(ctx, rankKey, sourceCategory, works)
	return err
}

// ReplaceRankWithNew 原子替换榜单并返回此前从未出现过的作品 ID。
func (r *HongGuoRepository) ReplaceRankWithNew(ctx context.Context, rankKey, sourceCategory string, works []hongguo.Work) ([]string, error) {
	if !hongguo.ValidRank(rankKey) || len(works) == 0 {
		return nil, errors.New("红果榜单无效")
	}
	discoveries := make([]model.HongGuoWork, 0, len(works))
	entries := make([]model.HongGuoRankEntry, 0, len(works))
	for i, work := range works {
		if !hongguo.ValidID(work.SourceID) {
			return nil, errors.New("红果榜单作品 ID 无效")
		}
		discoveries = append(discoveries, model.HongGuoWork{SummaryOnly: true, Kind: model.MetadataKindSeries, SourceID: work.SourceID, SourceCategory: sourceCategory, Title: work.Title})
		entries = append(entries, model.HongGuoRankEntry{RankKey: rankKey, SourceID: work.SourceID, Position: i + 1})
	}
	updates := map[string]any{"title": gorm.Expr("CASE WHEN hongguo_works.summary_only THEN EXCLUDED.title ELSE hongguo_works.title END"), "updated_at": gorm.Expr("CASE WHEN hongguo_works.summary_only THEN EXCLUDED.updated_at ELSE hongguo_works.updated_at END")}
	if sourceCategory != "" {
		updates["source_category"] = gorm.Expr("CASE WHEN hongguo_works.source_category = '' THEN EXCLUDED.source_category ELSE hongguo_works.source_category END")
	}
	now := time.Now().UTC()
	var newIDs []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		newIDs, err = insertHongGuoDiscoveries(tx, discoveries)
		if err != nil {
			return err
		}
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
	return newIDs, err
}

// insertHongGuoDiscoveries 以数据库实际插入为准，避免多次扫描和并发登记重复计新增。
func insertHongGuoDiscoveries(tx *gorm.DB, rows []model.HongGuoWork) ([]string, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SourceID)
	}
	var inserted []string
	if err := tx.Raw("INSERT INTO hongguo_works (id, source_id, kind, title, summary_only, refreshed_at, created_at, updated_at) SELECT gen_random_uuid()::text, source_id, 'series', '', true, TIMESTAMPTZ '0001-01-01 00:00:00+00', now(), now() FROM unnest(?::text[]) AS input(source_id) ON CONFLICT (source_id) DO NOTHING RETURNING source_id", &ids).Scan(&inserted).Error; err != nil {
		return nil, err
	}
	return inserted, nil
}

// PendingDiscoveries 从业务表恢复待补齐项，详情成功后自然退出；失败项只经冷却队列重试。
func (r *HongGuoRepository) PendingDiscoveries(ctx context.Context, after string, cutoff time.Time) ([]model.HongGuoWork, error) {
	rows := []model.HongGuoWork{}
	err := r.db.WithContext(ctx).Where("source_id > ? AND created_at <= ?", after, cutoff).
		Where("summary_only = true").
		Where("NOT EXISTS (SELECT 1 FROM hongguo_sync_failures f WHERE f.source_id = hongguo_works.source_id)").
		Order("source_id").Limit(100).Find(&rows).Error
	return rows, err
}

// RankUpdatedAt 读取最近一次原子替换的成功时间，空榜返回 nil。
func (r *HongGuoRepository) RankUpdatedAt(ctx context.Context, key string) (*time.Time, error) {
	var result struct{ UpdatedAt *time.Time }
	err := r.db.WithContext(ctx).Model(&model.HongGuoRankEntry{}).Select("MAX(updated_at) AS updated_at").Where("rank_key = ?", key).Scan(&result).Error
	return result.UpdatedAt, err
}
