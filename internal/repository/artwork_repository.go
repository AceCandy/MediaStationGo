package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ArtworkRepository 管理内容去重后的图片资产和每种类型的当前选择。
type ArtworkRepository struct {
	db *gorm.DB
}

func (r *ArtworkRepository) SaveSelection(ctx context.Context, metadataID, artworkType, sourceProvider, sourceURL string, asset *model.ArtworkAsset) (*model.ArtworkAsset, error) {
	var saved model.ArtworkAsset
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "sha256"}},
			DoNothing: true,
		}).Create(asset).Error; err != nil {
			return err
		}
		if err := tx.Where("sha256 = ?", asset.SHA256).First(&saved).Error; err != nil {
			return err
		}
		selection := model.MetadataArtwork{
			MetadataID: metadataID, ArtworkType: artworkType, AssetID: saved.ID,
			SourceProvider: sourceProvider, SourceURL: sourceURL,
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "metadata_id"}, {Name: "artwork_type"}},
			DoUpdates: clause.Assignments(map[string]any{
				"asset_id": saved.ID, "source_provider": sourceProvider, "source_url": sourceURL,
				"updated_at": time.Now(), "deleted_at": nil,
			}),
		}).Create(&selection).Error
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func (r *ArtworkRepository) FindAssetByID(ctx context.Context, id string) (*model.ArtworkAsset, error) {
	var asset model.ArtworkAsset
	if err := r.db.WithContext(ctx).First(&asset, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &asset, nil
}

func (r *ArtworkRepository) FindSelection(ctx context.Context, metadataID, artworkType string) (*model.ArtworkAsset, error) {
	var asset model.ArtworkAsset
	err := r.db.WithContext(ctx).
		Table("artwork_assets AS aa").
		Joins("JOIN metadata_artworks AS ma ON ma.asset_id = aa.id AND ma.deleted_at IS NULL").
		Where("aa.deleted_at IS NULL AND ma.metadata_id = ? AND ma.artwork_type = ?", metadataID, artworkType).
		First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

// ListSelectionsByMetadataIDs 批量返回作品当前有效的图片选择。
func (r *ArtworkRepository) ListSelectionsByMetadataIDs(ctx context.Context, metadataIDs []string) ([]model.MetadataArtwork, error) {
	if len(metadataIDs) == 0 {
		return []model.MetadataArtwork{}, nil
	}
	var selections []model.MetadataArtwork
	err := r.db.WithContext(ctx).
		Table("metadata_artworks AS ma").
		Joins("JOIN artwork_assets AS aa ON aa.id = ma.asset_id AND aa.deleted_at IS NULL").
		Where("ma.deleted_at IS NULL AND ma.metadata_id IN ?", metadataIDs).
		Select("ma.*").
		Find(&selections).Error
	return selections, err
}

func (r *ArtworkRepository) DeleteSelection(ctx context.Context, metadataID, artworkType string) error {
	return r.db.WithContext(ctx).
		Where("metadata_id = ? AND artwork_type = ?", metadataID, artworkType).
		Delete(&model.MetadataArtwork{}).Error
}
