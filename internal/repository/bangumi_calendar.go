package repository

import (
	"context"
	"errors"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BangumiCalendarRepository 只保存周历来源快照，不维护本地媒体资料。
type BangumiCalendarRepository struct{ db *gorm.DB }

func (r *BangumiCalendarRepository) Load(ctx context.Context) (*model.BangumiCalendarSnapshot, error) {
	var row model.BangumiCalendarSnapshot
	err := r.db.WithContext(ctx).First(&row, "key = ?", "weekly").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *BangumiCalendarRepository) Save(ctx context.Context, row model.BangumiCalendarSnapshot) error {
	row.Key = "weekly"
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload", "fetched_at"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "bangumi_calendar_snapshots.fetched_at < EXCLUDED.fetched_at"}}},
	}).Create(&row).Error
}
