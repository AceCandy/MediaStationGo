package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// StorageConfigRepository persists model.StorageConfig records.
type StorageConfigRepository struct{ db *gorm.DB }

// Get returns the config row by type, or (nil, nil).
func (r *StorageConfigRepository) Get(ctx context.Context, kind string) (*model.StorageConfig, error) {
	var c model.StorageConfig
	err := r.db.WithContext(ctx).Where("type = ?", kind).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// List returns all storage configs.
func (r *StorageConfigRepository) List(ctx context.Context) ([]model.StorageConfig, error) {
	var rows []model.StorageConfig
	err := r.db.WithContext(ctx).Order("type asc").Find(&rows).Error
	return rows, err
}

// Upsert creates or replaces a storage config keyed by Type.
func (r *StorageConfigRepository) Upsert(ctx context.Context, c *model.StorageConfig) error {
	return r.db.WithContext(ctx).Where("type = ?", c.Type).
		Assign(*c).FirstOrCreate(c).Error
}
