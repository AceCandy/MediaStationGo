package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCatalogArtworkChanged = errors.New("catalog artwork source changed")

// QueueCatalogArtwork 只交接已入库资料，不重置已有图片退避时间。
func (r *MetadataRepository) QueueCatalogArtwork(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&model.MetadataItem{}).
		Where("id = ? AND catalog_metadata_hydrated_at IS NOT NULL AND catalog_artwork_hydrated_at IS NULL AND catalog_artwork_due_at IS NULL", id).
		UpdateColumn("catalog_artwork_due_at", time.Now().UTC()).Error
}

// NextCatalogArtworkAt 用业务待办决定唤醒时间，不扫描任务历史。
func (r *MetadataRepository) NextCatalogArtworkAt(ctx context.Context) (*time.Time, error) {
	var row struct{ Next *time.Time }
	err := r.db.WithContext(ctx).Model(&model.MetadataItem{}).
		Select("MIN(catalog_artwork_due_at) AS next").Where("catalog_artwork_due_at IS NOT NULL").Scan(&row).Error
	return row.Next, err
}

func (r *MetadataRepository) ListDueCatalogArtwork(ctx context.Context, cutoff time.Time, limit int) ([]model.MetadataItem, error) {
	var items []model.MetadataItem
	err := r.db.WithContext(ctx).Where("catalog_artwork_due_at <= ?", cutoff).
		Order("catalog_artwork_due_at, id").Limit(limit).Find(&items).Error
	return items, err
}

// RetryCatalogArtwork 条件更新本轮待办的失败退避，不改资料更新时间。
func (r *MetadataRepository) RetryCatalogArtwork(ctx context.Context, item model.MetadataItem, next time.Time) error {
	return r.db.WithContext(ctx).Model(&model.MetadataItem{}).
		Where("id = ? AND catalog_artwork_due_at = ?", item.ID, item.CatalogArtworkDueAt).
		UpdateColumns(map[string]any{"catalog_artwork_due_at": next, "catalog_artwork_attempts": item.CatalogArtworkAttempts + 1}).Error
}

func catalogArtworkSnapshotCurrent(tx *gorm.DB, item model.MetadataItem, snapshot *model.MetadataProviderSnapshot) error {
	var current model.MetadataItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", item.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCatalogArtworkChanged
		}
		return err
	}
	if current.Source != item.Source || current.Kind != item.Kind || current.CatalogArtworkDueAt == nil || !current.CatalogArtworkDueAt.Equal(*item.CatalogArtworkDueAt) {
		return ErrCatalogArtworkChanged
	}
	var currentSnapshot model.MetadataProviderSnapshot
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("metadata_id = ? AND provider = 'tmdb' AND fetched_at = ?", item.ID, snapshot.FetchedAt).
		Where(`EXISTS (SELECT 1 FROM metadata_identifiers mi WHERE mi.metadata_id = metadata_provider_snapshots.metadata_id AND mi.provider = 'tmdb' AND mi.entity_kind = ? AND mi.external_id = metadata_provider_snapshots.payload->>'id')`, item.Kind).
		First(&currentSnapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCatalogArtworkChanged
	}
	return err
}

// SaveCatalogArtworkAsset 在网络请求结束后校验来源，仅填补空图片选择。
func (r *MetadataRepository) SaveCatalogArtworkAsset(ctx context.Context, item model.MetadataItem, snapshot *model.MetadataProviderSnapshot, kind, source string, asset *model.ArtworkAsset) (bool, error) {
	selected := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := catalogArtworkSnapshotCurrent(tx, item, snapshot); err != nil {
			return err
		}
		var err error
		_, selected, err = (&ArtworkRepository{db: tx}).SaveCatalogSelection(ctx, item.ID, kind, "tmdb", source, asset)
		return err
	})
	return selected, err
}

// CompleteCatalogArtwork 图片与头像处理成功后才完成图片检查点；显式无图留给既有复查。
func (r *MetadataRepository) CompleteCatalogArtwork(ctx context.Context, item model.MetadataItem, snapshot *model.MetadataProviderSnapshot, missing []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := catalogArtworkSnapshotCurrent(tx, item, snapshot); err != nil {
			return err
		}
		now := time.Now().UTC()
		if item.Kind == model.MetadataKindMovie || item.Kind == model.MetadataKindSeries {
			artwork := &ArtworkRepository{db: tx}
			for _, kind := range missing {
				selected, err := artwork.FindSelection(ctx, item.ID, kind)
				if err != nil {
					return err
				}
				if selected == nil {
					if err := artwork.UpsertArtworkRecheck(ctx, item.ID, kind, now); err != nil {
						return err
					}
				}
			}
		}
		return tx.Model(&model.MetadataItem{}).Where("id = ?", item.ID).
			UpdateColumns(map[string]any{"catalog_artwork_hydrated_at": now, "catalog_artwork_due_at": nil, "catalog_artwork_attempts": 0}).Error
	})
}
