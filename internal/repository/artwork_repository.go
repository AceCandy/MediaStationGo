package repository

import (
	"context"
	"errors"
	"strings"
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
				"updated_at": time.Now(),
			}),
		}).Create(&selection).Error
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// SaveCatalogSelection 只填补空缺或悬空 selection，不覆盖手工或本地选择。
func (r *ArtworkRepository) SaveCatalogSelection(ctx context.Context, metadataID, artworkType, sourceProvider, sourceURL string, asset *model.ArtworkAsset) (*model.ArtworkAsset, bool, error) {
	var saved model.ArtworkAsset
	selected := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sha256"}}, DoNothing: true}).Create(asset).Error; err != nil {
			return err
		}
		if err := tx.Where("sha256 = ?", asset.SHA256).First(&saved).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"asset_id": saved.ID, "source_provider": sourceProvider, "source_url": sourceURL, "updated_at": time.Now(),
		}
		repaired := tx.Model(&model.MetadataArtwork{}).
			Where("metadata_id = ? AND artwork_type = ?", metadataID, artworkType).
			Where("NOT EXISTS (SELECT 1 FROM artwork_assets WHERE artwork_assets.id = metadata_artworks.asset_id)").
			Updates(updates)
		if repaired.Error != nil || repaired.RowsAffected > 0 {
			selected = repaired.RowsAffected > 0
			return repaired.Error
		}
		selection := model.MetadataArtwork{
			MetadataID: metadataID, ArtworkType: artworkType, AssetID: saved.ID,
			SourceProvider: sourceProvider, SourceURL: sourceURL,
		}
		created := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "metadata_id"}, {Name: "artwork_type"}},
			DoNothing: true,
		}).Create(&selection)
		selected = created.RowsAffected > 0
		return created.Error
	})
	if err != nil {
		return nil, false, err
	}
	return &saved, selected, nil
}

// SaveCandidate 保存 provider 候选，并只在当前没有 selection 时将其提升为当前图片。
func (r *ArtworkRepository) SaveCandidate(ctx context.Context, metadataID, artworkType, sourceProvider, sourceURL string, asset *model.ArtworkAsset) (*model.ArtworkAsset, bool, error) {
	var saved model.ArtworkAsset
	promoted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sha256"}}, DoNothing: true}).Create(asset).Error; err != nil {
			return err
		}
		if err := tx.Where("sha256 = ?", asset.SHA256).First(&saved).Error; err != nil {
			return err
		}
		candidate := model.MetadataArtworkCandidate{
			MetadataID: metadataID, ArtworkType: artworkType, SourceProvider: sourceProvider,
			SourceURL: sourceURL, AssetID: saved.ID,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "metadata_id"}, {Name: "artwork_type"}, {Name: "source_provider"}},
			DoUpdates: clause.Assignments(map[string]any{
				"asset_id": saved.ID, "source_url": sourceURL, "updated_at": time.Now(),
			}),
		}).Create(&candidate).Error; err != nil {
			return err
		}
		selection := model.MetadataArtwork{
			MetadataID: metadataID, ArtworkType: artworkType, AssetID: saved.ID,
			SourceProvider: sourceProvider, SourceURL: sourceURL,
		}
		created := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "metadata_id"}, {Name: "artwork_type"}}, DoNothing: true,
		}).Create(&selection)
		promoted = created.RowsAffected > 0
		return created.Error
	})
	return &saved, promoted, err
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
		Joins("JOIN metadata_artworks AS ma ON ma.asset_id = aa.id").
		Where("ma.metadata_id = ? AND ma.artwork_type = ?", metadataID, artworkType).
		First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *ArtworkRepository) HasCandidate(ctx context.Context, metadataID, artworkType, sourceProvider string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.MetadataArtworkCandidate{}).
		Where("metadata_id = ? AND artwork_type = ? AND source_provider = ?", metadataID, artworkType, sourceProvider).
		Count(&count).Error
	return count > 0, err
}

// ListSelectionsByMetadataIDs 批量返回作品当前有效的图片选择。
func (r *ArtworkRepository) ListSelectionsByMetadataIDs(ctx context.Context, metadataIDs []string) ([]model.MetadataArtwork, error) {
	if len(metadataIDs) == 0 {
		return []model.MetadataArtwork{}, nil
	}
	var selections []model.MetadataArtwork
	err := r.db.WithContext(ctx).
		Table("metadata_artworks AS ma").
		Joins("JOIN artwork_assets AS aa ON aa.id = ma.asset_id").
		Where("ma.metadata_id IN ?", metadataIDs).
		Select("ma.*").
		Find(&selections).Error
	return selections, err
}

// ListSelectedAssetsAfter 按 ID 分页返回仍被当前选择或候选关系引用的本地资产。
func (r *ArtworkRepository) ListSelectedAssetsAfter(ctx context.Context, afterID string, limit int) ([]model.ArtworkAsset, error) {
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 1000)
	var assets []model.ArtworkAsset
	q := r.db.WithContext(ctx).
		Table("artwork_assets AS aa").
		Where("EXISTS (SELECT 1 FROM metadata_artworks AS ma WHERE ma.asset_id = aa.id) OR EXISTS (SELECT 1 FROM metadata_artwork_candidates AS mac WHERE mac.asset_id = aa.id)")
	if afterID = strings.TrimSpace(afterID); afterID != "" {
		q = q.Where("aa.id > ?", afterID)
	}
	err := q.Select("aa.*").Order("aa.id ASC").Limit(limit).Scan(&assets).Error
	return assets, err
}

// InvalidateSelectionsForMissingAsset 清除缺失本地文件的选择并重新开放补图 checkpoint。
func (r *ArtworkRepository) InvalidateSelectionsForMissingAsset(ctx context.Context, assetID string) (int64, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return 0, errors.New("artwork asset id is required")
	}
	var invalidated int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("asset_id = ?", assetID).Delete(&model.MetadataArtworkCandidate{}).Error; err != nil {
			return err
		}
		var removed []model.MetadataArtwork
		res := tx.Clauses(clause.Returning{Columns: []clause.Column{{Name: "metadata_id"}}}).
			Where("asset_id = ?", assetID).
			Delete(&removed)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		invalidated = res.RowsAffected
		metadataIDs := make([]string, 0, len(removed))
		seen := make(map[string]struct{}, len(removed))
		for _, selection := range removed {
			if _, ok := seen[selection.MetadataID]; ok {
				continue
			}
			seen[selection.MetadataID] = struct{}{}
			metadataIDs = append(metadataIDs, selection.MetadataID)
		}
		return tx.Model(&model.MetadataItem{}).
			Where("id IN ?", metadataIDs).
			Updates(map[string]any{"catalog_artwork_hydrated_at": nil, "updated_at": time.Now().UTC()}).Error
	})
	return invalidated, err
}

func (r *ArtworkRepository) DeleteSelection(ctx context.Context, metadataID, artworkType string) error {
	return r.db.WithContext(ctx).
		Where("metadata_id = ? AND artwork_type = ?", metadataID, artworkType).
		Delete(&model.MetadataArtwork{}).Error
}
