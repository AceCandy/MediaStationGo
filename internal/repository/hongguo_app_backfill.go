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
	rows := make([]model.HongGuoDiscovery, 0, len(works))
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
		rows = append(rows, model.HongGuoDiscovery{SourceID: w.SourceID, SourceCategory: category, Title: w.Title, Overview: w.Overview, CoverURL: w.CoverURL, EpisodeCount: w.EpisodeCount, UpdateText: w.UpdateText})
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
		for _, name := range []string{"title", "overview", "cover_url", "update_text"} {
			updates[name] = gorm.Expr("COALESCE(NULLIF(hongguo_discoveries." + name + ", ''), EXCLUDED." + name + ")")
		}
		updates["episode_count"] = gorm.Expr("CASE WHEN hongguo_discoveries.episode_count=0 THEN EXCLUDED.episode_count ELSE hongguo_discoveries.episode_count END")
		// 有明确分类的正式资料优先用于补齐摘要；既有摘要分类不改动。
		updates["source_category"] = gorm.Expr("hongguo_discoveries.source_category")
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}}, DoUpdates: clause.Assignments(updates)}).CreateInBatches(&rows, 100).Error; err != nil {
			return err
		}
		var filled []string
		if err := tx.Raw(`UPDATE hongguo_discoveries d SET source_category=COALESCE((SELECT NULLIF(w.source_category,'') FROM hongguo_works w WHERE w.source_id=d.source_id), ?), updated_at=now() WHERE d.source_id=ANY(?) AND COALESCE(d.source_category,'')='' RETURNING d.source_id`, category, &ids).Scan(&filled).Error; err != nil {
			return err
		}
		var canonical []string
		if err := tx.Raw(`UPDATE hongguo_works w SET source_category=d.source_category, updated_at=now() FROM hongguo_discoveries d WHERE w.source_id=d.source_id AND w.source_id=ANY(?) AND COALESCE(w.source_category,'')='' AND d.source_category<>'' RETURNING w.source_id`, &ids).Scan(&canonical).Error; err != nil {
			return err
		}
		classified := map[string]bool{}
		for _, id := range append(filled, canonical...) {
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
