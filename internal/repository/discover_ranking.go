package repository

import (
	"context"
	"errors"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DiscoverRankingRepository 只保存成功榜单快照，不管理作品资料或图片任务。
type DiscoverRankingRepository struct{ db *gorm.DB }

func (r *DiscoverRankingRepository) Load(ctx context.Context, key string, page int) (*model.DiscoverRankingPage, error) {
	var row model.DiscoverRankingPage
	err := r.db.WithContext(ctx).Where("key = ? AND page = ?", key, page).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *DiscoverRankingRepository) Save(ctx context.Context, row model.DiscoverRankingPage) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		saved := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}, {Name: "page"}},
			DoUpdates: clause.AssignmentColumns([]string{"payload", "fetched_at"}),
			Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "discover_ranking_pages.fetched_at < EXCLUDED.fetched_at"}}},
		}).Create(&row)
		if saved.Error != nil {
			return saved.Error
		}
		if row.Page == 1 && saved.RowsAffected > 0 {
			return tx.Where("key = ? AND page > 1 AND fetched_at < ?", row.Key, row.FetchedAt).Delete(&model.DiscoverRankingPage{}).Error
		}
		return nil
	})
}
