package repository

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func (r *HuangGuoAIRepository) DueArtwork(ctx context.Context, after string, cutoff time.Time) ([]model.HuangGuoAIArtwork, error) {
	rows := []model.HuangGuoAIArtwork{}
	err := r.db.WithContext(ctx).Where("id > ? AND created_at <= ? AND (next_attempt_at <= ? OR (next_attempt_at IS NULL AND local_key = ''))", after, cutoff, cutoff).Order("id").Limit(50).Find(&rows).Error
	return rows, err
}

// FinishArtwork only accepts the source URL claimed by this attempt; old files survive failed refreshes.
func (r *HuangGuoAIRepository) FinishArtwork(ctx context.Context, row model.HuangGuoAIArtwork, key string, retry *time.Time) error {
	updates := map[string]any{"next_attempt_at": retry, "updated_at": time.Now()}
	if retry == nil {
		updates["local_key"] = key
		updates["attempts"] = 0
	} else {
		updates["attempts"] = gorm.Expr("attempts + 1")
	}
	return r.db.WithContext(ctx).Model(&model.HuangGuoAIArtwork{}).Where("id = ? AND source_url = ?", row.ID, row.SourceURL).Updates(updates).Error
}

func (r *HuangGuoAIRepository) Artwork(ctx context.Context, id string) (*model.HuangGuoAIArtwork, error) {
	var row model.HuangGuoAIArtwork
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	return &row, err
}

func (r *HuangGuoAIRepository) ScheduleMissingArtwork(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&model.HuangGuoAIArtwork{}).Where("id = ? AND next_attempt_at IS NULL", id).Update("next_attempt_at", time.Now()).Error
}
