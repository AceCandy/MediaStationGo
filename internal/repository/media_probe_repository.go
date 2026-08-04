package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// MediaProbeRepository 持久化与 Media 一对一的完整探测文档。
type MediaProbeRepository struct {
	db *gorm.DB
}

func (r *MediaProbeRepository) FindByMediaID(ctx context.Context, mediaID string) (*model.MediaProbeMetadata, error) {
	var row model.MediaProbeMetadata
	err := r.db.WithContext(ctx).Where("media_id = ?", mediaID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *MediaProbeRepository) ListByMediaIDs(ctx context.Context, mediaIDs []string) (map[string]model.MediaProbeMetadata, error) {
	rows := make([]model.MediaProbeMetadata, 0, len(mediaIDs))
	if len(mediaIDs) == 0 {
		return map[string]model.MediaProbeMetadata{}, nil
	}
	if err := r.db.WithContext(ctx).Where("media_id IN ?", mediaIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]model.MediaProbeMetadata, len(rows))
	for _, row := range rows {
		out[row.MediaID] = row
	}
	return out, nil
}

func (r *MediaProbeRepository) Upsert(ctx context.Context, row *model.MediaProbeMetadata) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "media_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"probe_json", "schema_version", "probed_at"}),
	}).Create(row).Error
}
