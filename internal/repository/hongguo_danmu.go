package repository

import (
	"context"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/clause"
)

func (r *HongGuoRepository) Danmus(ctx context.Context, sourceID string, episode int) ([]model.HongGuoDanmu, error) {
	var rows []model.HongGuoDanmu
	err := r.db.WithContext(ctx).Where("source_id = ? AND episode_number = ?", sourceID, episode).Find(&rows).Error
	return rows, err
}

// InsertDanmus 只积累新条目；同 ID 内容变化或源站删除均不覆盖历史。
func (r *HongGuoRepository) InsertDanmus(ctx context.Context, rows []model.HongGuoDanmu) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 500).Error
}
