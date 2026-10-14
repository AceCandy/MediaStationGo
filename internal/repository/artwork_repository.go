package repository

import (
	"context"
	"errors"
	"net/url"
	"path"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var ErrCatalogArtworkChanged = errors.New("catalog artwork source changed")

// ArtworkRepository 管理内容去重后的图片资产和每种类型的当前选择。
type ArtworkRepository struct {
	db *gorm.DB
}

// SaveAsset 只保存去重后的原图，不改变任何媒体的图片选择。
func (r *ArtworkRepository) SaveAsset(ctx context.Context, asset *model.ArtworkAsset) (*model.ArtworkAsset, error) {
	var saved model.ArtworkAsset
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(asset).Error; err != nil {
			return err
		}
		return tx.Where("storage_key = ? AND sha256 = ?", asset.StorageKey, asset.SHA256).First(&saved).Error
	})
	return &saved, err
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
		Joins("JOIN metadata_artwork_presentations AS ma ON ma.asset_id = aa.id").
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

// HasProviderArtwork 检查关联来源中心的可用本地图片。
func (r *ArtworkRepository) HasProviderArtwork(ctx context.Context, metadataID, artworkType, sourceProvider string) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (
 SELECT 1 FROM metadata_identifiers binding
 JOIN tmdb_subjects subject ON subject.kind=binding.entity_kind AND subject.tmdb_id::text=binding.external_id
 JOIN tmdb_artworks artwork ON artwork.subject_id=subject.id AND artwork.type=?
 JOIN artwork_assets asset ON asset.id=artwork.asset_id
 WHERE binding.metadata_id=? AND binding.provider='tmdb' AND ?='tmdb'
 UNION ALL
 SELECT 1 FROM metadata_identifiers binding
 JOIN douban_subjects subject ON subject.douban_id=binding.external_id
 AND subject.entity_kind=COALESCE(NULLIF(binding.douban_entity_kind,''),binding.entity_kind)
 JOIN artwork_assets asset ON asset.id=subject.poster_asset_id
 WHERE binding.metadata_id=? AND binding.provider='douban' AND ?='douban' AND ?='poster'
)`, artworkType, metadataID, sourceProvider, metadataID, sourceProvider, artworkType).Scan(&exists).Error
	return exists, err
}

// ListSelectionsByMetadataIDs 批量返回作品当前有效的图片选择。
func (r *ArtworkRepository) ListSelectionsByMetadataIDs(ctx context.Context, metadataIDs []string) ([]model.MetadataArtwork, error) {
	if len(metadataIDs) == 0 {
		return []model.MetadataArtwork{}, nil
	}
	var selections []model.MetadataArtwork
	err := r.db.WithContext(ctx).
		Table("metadata_artwork_presentations AS ma").
		Joins("JOIN artwork_assets AS aa ON aa.id = ma.asset_id").
		Where("ma.metadata_id IN ?", metadataIDs).
		Select("ma.*").
		Find(&selections).Error
	return selections, err
}

// SaveSourceArtwork 只关联当前 TMDb 来源仍使用的图片路径，拒绝下载期间变更的来源。
func (r *ArtworkRepository) SaveSourceArtwork(ctx context.Context, metadataID, artworkType, sourceURL string, asset *model.ArtworkAsset) (*model.ArtworkAsset, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return nil, err
	}
	var saved *model.ArtworkAsset
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		saved, err = (&ArtworkRepository{db: tx}).SaveAsset(ctx, asset)
		if err != nil {
			return err
		}
		result := tx.Model(&model.TMDbArtwork{}).
			Where(`subject_id IN (SELECT subject.id FROM tmdb_subjects subject JOIN metadata_identifiers binding
ON binding.provider='tmdb' AND binding.entity_kind=subject.kind AND binding.external_id=subject.tmdb_id::text
WHERE binding.metadata_id=?) AND type=? AND source_path=?`, metadataID, artworkType, "/"+path.Base(parsed.Path)).
			Updates(map[string]any{"asset_id": saved.ID, "downloaded_path": "/" + path.Base(parsed.Path), "status": "ready", "retry_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrCatalogArtworkChanged
		}
		return nil
	})
	return saved, err
}
