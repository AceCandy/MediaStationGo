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

// AppBackfillResult 仅统计实际新增作品和已存在作品的分类补齐。
type AppBackfillResult struct {
	NewWorks        int
	ClassifiedWorks int
}

// SaveAppDiscoveryPage 幂等追加 App 摘要并填空分类，不覆盖已有摘要、手工分类或官网游标。
func (r *HongGuoRepository) SaveAppDiscoveryPage(ctx context.Context, category string, works []hongguo.Work) (AppBackfillResult, error) {
	stats := AppBackfillResult{}
	if !hongguo.ValidCategory(category) {
		return stats, errors.New("红果 App 补录分类无效")
	}
	rows := make([]model.HongGuoWork, 0, len(works))
	ids := []string{}
	seen := map[string]bool{}
	for _, w := range works {
		if !hongguo.ValidID(w.SourceID) || w.Title == "" {
			return stats, errors.New("红果 App 补录摘要无效")
		}
		if seen[w.SourceID] {
			continue
		}
		seen[w.SourceID] = true
		ids = append(ids, w.SourceID)
		rows = append(rows, model.HongGuoWork{SummaryOnly: true, Kind: model.MetadataKindSeries, SourceID: w.SourceID, SourceCategory: category, Title: w.Title, Overview: w.Overview, EpisodeCount: w.EpisodeCount, UpdateText: w.UpdateText})
	}
	if len(rows) == 0 {
		return stats, nil
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		newIDs, err := insertHongGuoDiscoveries(tx, rows)
		if err != nil {
			return err
		}
		stats.NewWorks = len(newIDs)
		updates := map[string]any{}
		for _, name := range []string{"title", "overview", "update_text"} {
			updates[name] = gorm.Expr("COALESCE(NULLIF(hongguo_works." + name + ", ''), EXCLUDED." + name + ")")
		}
		updates["episode_count"] = gorm.Expr("CASE WHEN hongguo_works.episode_count=0 THEN EXCLUDED.episode_count ELSE hongguo_works.episode_count END")
		// 分类统一在后面填空，保留已有明确分类。
		updates["source_category"] = gorm.Expr("hongguo_works.source_category")
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "hongguo_works.summary_only"}}}}).CreateInBatches(&rows, 100).Error; err != nil {
			return err
		}
		var filled []string
		if err := tx.Raw(`UPDATE hongguo_works SET source_category=?, updated_at=now() WHERE source_id=ANY(?) AND source_category='' RETURNING source_id`, category, &ids).Scan(&filled).Error; err != nil {
			return err
		}
		classified := map[string]bool{}
		for _, id := range filled {
			classified[id] = true
		}
		for _, id := range newIDs {
			delete(classified, id)
		}
		stats.ClassifiedWorks = len(classified)
		added := map[string]bool{}
		for _, id := range newIDs {
			added[id] = true
		}
		for _, w := range works {
			if added[w.SourceID] {
				if err := saveHongGuoArtwork(tx, &w.SourceID, nil, nil, w.CoverURL, time.Now().UTC()); err != nil {
					return err
				}
				delete(added, w.SourceID)
			}
		}
		return nil
	})
	if err != nil {
		return AppBackfillResult{}, err
	}
	return stats, nil
}
