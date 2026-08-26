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

type TMDbArtworkSelection struct {
	SelectionID  string
	MetadataID   string
	Title        string
	Kind         string
	SeasonNum    int
	EpisodeNum   int
	TMDbID       string
	SeriesTMDbID string
	ArtworkType  string
	SourceURL    string
	AssetID      string
	StorageKey   string
}

type TMDbArtworkRecheckType struct {
	ArtworkType    string
	SelectionID    string
	AssetID        string
	StorageKey     string
	SourceProvider string
	SourceURL      string
	LastNoImageAt  *time.Time
}

type TMDbArtworkRecheckCandidate struct {
	MetadataID               string
	Title                    string
	Kind                     string
	SeasonNum                int
	EpisodeNum               int
	TMDbID                   string
	SeriesTMDbID             string
	CatalogArtworkHydratedAt *time.Time
	Types                    []TMDbArtworkRecheckType `gorm:"-"`
}

const tmdbArtworkRecheckHasMediaSQL = `EXISTS (
  SELECT 1
  FROM media artwork_media
  LEFT JOIN metadata_items artwork_linked ON artwork_linked.id = artwork_media.metadata_id
  LEFT JOIN metadata_items artwork_parent ON artwork_parent.id = artwork_linked.parent_id
  WHERE artwork_media.metadata_id = mi.id
     OR (mi.kind = 'season' AND artwork_linked.parent_id = mi.id)
     OR (mi.kind = 'series' AND (artwork_linked.parent_id = mi.id OR artwork_parent.parent_id = mi.id))
)`

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

// ListTMDbArtworkSelectionsAfter 返回当前选中的 TMDb 图片，供本地文件修复任务按 selection 游标扫描。
func (r *ArtworkRepository) ListTMDbArtworkSelectionsAfter(ctx context.Context, afterID string, limit int) ([]TMDbArtworkSelection, error) {
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 1000)
	q := r.db.WithContext(ctx).Table("metadata_artworks AS ma").
		Joins("JOIN metadata_items AS mi ON mi.id = ma.metadata_id").
		Joins("JOIN artwork_assets AS aa ON aa.id = ma.asset_id").
		Where("ma.source_provider = 'tmdb' AND btrim(ma.source_url) <> ''")
	if afterID = strings.TrimSpace(afterID); afterID != "" {
		q = q.Where("ma.id > ?", afterID)
	}
	var rows []TMDbArtworkSelection
	err := q.Select(`ma.id AS selection_id, ma.metadata_id, mi.title, mi.kind,
CASE WHEN mi.kind = 'episode' THEN (SELECT season_num FROM metadata_items WHERE id = mi.parent_id) ELSE mi.season_num END AS season_num,
mi.episode_num,
ma.artwork_type, ma.source_url, ma.asset_id, aa.storage_key,
COALESCE((SELECT external_id FROM metadata_identifiers WHERE metadata_id = mi.id AND provider = 'tmdb' AND entity_kind = mi.kind ORDER BY id LIMIT 1), '') AS tm_db_id,
COALESCE((SELECT external_id FROM metadata_identifiers WHERE metadata_id = CASE
  WHEN mi.kind = 'series' THEN mi.id
  WHEN mi.kind = 'season' THEN mi.parent_id
  WHEN mi.kind = 'episode' THEN (SELECT parent_id FROM metadata_items WHERE id = mi.parent_id)
  ELSE NULL END AND provider = 'tmdb' AND entity_kind = 'series' ORDER BY id LIMIT 1), '') AS series_tm_db_id`).
		Order("ma.id ASC").Limit(limit).Scan(&rows).Error
	return rows, err
}

// ListTMDbArtworkRecheckMetadataAfter 返回已完成过图片处理且仍有缺图状态的元数据。
func (r *ArtworkRepository) ListTMDbArtworkRecheckMetadataAfter(ctx context.Context, afterID string, limit int) ([]TMDbArtworkRecheckCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 1000)
	q := r.db.WithContext(ctx).Table("metadata_items AS mi").
		Where("mi.kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries, model.MetadataKindSeason}).
		Where(tmdbArtworkRecheckHasMediaSQL).
		Where("mi.catalog_artwork_hydrated_at IS NOT NULL OR EXISTS (SELECT 1 FROM metadata_artwork_rechecks mar WHERE mar.metadata_id = mi.id)").
		Where("EXISTS (SELECT 1 FROM metadata_identifiers mid WHERE mid.metadata_id = mi.id AND mid.provider = 'tmdb' AND mid.entity_kind = mi.kind AND btrim(mid.external_id) <> '')").
		Where(`EXISTS (SELECT 1 FROM metadata_artwork_rechecks mar WHERE mar.metadata_id = mi.id)
OR (mi.kind IN ('movie','series') AND (
  NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'poster')
  OR NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'backdrop')))
OR (mi.kind = 'season' AND NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'poster'))`)
	if afterID = strings.TrimSpace(afterID); afterID != "" {
		q = q.Where("mi.id > ?", afterID)
	}
	var rows []TMDbArtworkRecheckCandidate
	err := q.Select(`mi.id AS metadata_id, mi.title, mi.kind,
CASE WHEN mi.kind = 'episode' THEN (SELECT season_num FROM metadata_items WHERE id = mi.parent_id) ELSE mi.season_num END AS season_num,
mi.episode_num, mi.catalog_artwork_hydrated_at,
COALESCE((SELECT external_id FROM metadata_identifiers WHERE metadata_id = mi.id AND provider = 'tmdb' AND entity_kind = mi.kind ORDER BY id LIMIT 1), '') AS tm_db_id,
COALESCE((SELECT external_id FROM metadata_identifiers WHERE metadata_id = CASE
  WHEN mi.kind = 'series' THEN mi.id
  WHEN mi.kind = 'season' THEN mi.parent_id
  WHEN mi.kind = 'episode' THEN (SELECT parent_id FROM metadata_items WHERE id = mi.parent_id)
  ELSE NULL END AND provider = 'tmdb' AND entity_kind = 'series' ORDER BY id LIMIT 1), '') AS series_tm_db_id`).
		Order("mi.id ASC").Limit(limit).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	ids := make([]string, len(rows))
	byID := make(map[string]int, len(rows))
	for i := range rows {
		ids[i], byID[rows[i].MetadataID] = rows[i].MetadataID, i
	}
	type relation struct {
		MetadataID     string
		ArtworkType    string
		SelectionID    string
		AssetID        string
		StorageKey     string
		SourceProvider string
		SourceURL      string
		LastNoImageAt  *time.Time
	}
	var relations []relation
	err = r.db.WithContext(ctx).Raw(`
SELECT requested.metadata_id, requested.artwork_type,
       COALESCE(ma.id, '') AS selection_id, COALESCE(ma.asset_id, '') AS asset_id,
       COALESCE(aa.storage_key, '') AS storage_key, COALESCE(ma.source_provider, '') AS source_provider,
       COALESCE(ma.source_url, '') AS source_url, mar.last_no_image_at
FROM (
  SELECT id AS metadata_id, unnest(CASE kind WHEN 'movie' THEN ARRAY['poster','backdrop'] WHEN 'series' THEN ARRAY['poster','backdrop'] WHEN 'season' THEN ARRAY['poster'] ELSE ARRAY['still'] END) AS artwork_type
  FROM metadata_items WHERE id IN ?
) requested
LEFT JOIN metadata_artworks ma ON ma.metadata_id = requested.metadata_id AND ma.artwork_type = requested.artwork_type
LEFT JOIN artwork_assets aa ON aa.id = ma.asset_id
LEFT JOIN metadata_artwork_rechecks mar ON mar.metadata_id = requested.metadata_id AND mar.artwork_type = requested.artwork_type
ORDER BY requested.metadata_id, requested.artwork_type`, ids).Scan(&relations).Error
	if err != nil {
		return nil, err
	}
	for _, item := range relations {
		i := byID[item.MetadataID]
		rows[i].Types = append(rows[i].Types, TMDbArtworkRecheckType{
			ArtworkType: item.ArtworkType, SelectionID: item.SelectionID, AssetID: item.AssetID,
			StorageKey: item.StorageKey, SourceProvider: item.SourceProvider, SourceURL: item.SourceURL,
			LastNoImageAt: item.LastNoImageAt,
		})
	}
	return rows, nil
}

// RepairTMDbSelection 只在 selection 仍与扫描快照一致时替换其本地资产和来源。
func (r *ArtworkRepository) RepairTMDbSelection(ctx context.Context, snapshot TMDbArtworkSelection, sourceURL string, asset *model.ArtworkAsset) (*model.ArtworkAsset, bool, error) {
	var saved model.ArtworkAsset
	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sha256"}}, DoNothing: true}).Create(asset).Error; err != nil {
			return err
		}
		if err := tx.Where("sha256 = ?", asset.SHA256).First(&saved).Error; err != nil {
			return err
		}
		res := tx.Model(&model.MetadataArtwork{}).
			Where("id = ? AND metadata_id = ? AND artwork_type = ? AND asset_id = ?", snapshot.SelectionID, snapshot.MetadataID, snapshot.ArtworkType, snapshot.AssetID).
			Where("source_provider = 'tmdb' AND source_url = ?", snapshot.SourceURL).
			Updates(map[string]any{"asset_id": saved.ID, "source_url": sourceURL, "updated_at": time.Now().UTC()})
		updated = res.RowsAffected > 0
		return res.Error
	})
	return &saved, updated, err
}

func (r *ArtworkRepository) UpsertArtworkRecheck(ctx context.Context, metadataID, artworkType string, at time.Time) error {
	row := model.MetadataArtworkRecheck{MetadataID: metadataID, ArtworkType: artworkType, LastNoImageAt: at.UTC()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "metadata_id"}, {Name: "artwork_type"}},
		DoUpdates: clause.Assignments(map[string]any{"last_no_image_at": at.UTC(), "updated_at": time.Now().UTC()}),
	}).Create(&row).Error
}

func (r *ArtworkRepository) DeleteArtworkRecheck(ctx context.Context, metadataID, artworkType string) error {
	return r.db.WithContext(ctx).Where("metadata_id = ? AND artwork_type = ?", metadataID, artworkType).Delete(&model.MetadataArtworkRecheck{}).Error
}

func (r *ArtworkRepository) DeleteSelection(ctx context.Context, metadataID, artworkType string) error {
	return r.db.WithContext(ctx).
		Where("metadata_id = ? AND artwork_type = ?", metadataID, artworkType).
		Delete(&model.MetadataArtwork{}).Error
}
