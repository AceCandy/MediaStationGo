package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CatalogImageMigrationResult 只报告数量，不记录来源 URL 或磁盘路径。
type CatalogImageMigrationResult struct{ Migrated, Missing int }

// MigrateCatalogImages 在服务对外和后台写入开始前迁移原图；旧文件保留用于中断恢复。
func MigrateCatalogImages(ctx context.Context, db *gorm.DB, dataDir string) (CatalogImageMigrationResult, error) {
	m := catalogImageMigration{db: db.WithContext(ctx), dataDir: dataDir}
	for _, run := range []func() error{m.artwork, m.people, m.doubanCredits, m.catalogs, m.libraryCovers} {
		if err := run(); err != nil {
			return m.result, err
		}
	}
	return m.result, nil
}

type catalogImageMigration struct {
	db      *gorm.DB
	dataDir string
	result  CatalogImageMigrationResult
}

type catalogImageKeyChange struct{ ID, Old, New string }

// updateKeys 一条语句原子提交一批路径，保留时间戳且不覆盖已经变化的引用。
// 表名、列名只来自迁移内部的固定调用点。
func (m *catalogImageMigration) updateKeys(table, column string, changes []catalogImageKeyChange) error {
	if len(changes) == 0 {
		return nil
	}
	values := make([]string, len(changes))
	args := make([]any, 0, len(changes)*3)
	for i, change := range changes {
		values[i] = "(?::text, ?::text, ?::text)"
		args = append(args, change.ID, change.Old, change.New)
	}
	query := fmt.Sprintf(`UPDATE %s AS target SET %s = source.new_key
FROM (VALUES %s) AS source(id, old_key, new_key)
WHERE target.id = source.id AND target.%s = source.old_key`, table, column, strings.Join(values, ","), column)
	result := m.db.Exec(query, args...)
	if result.Error != nil {
		return result.Error
	}
	m.result.Migrated += int(result.RowsAffected)
	return nil
}

// read 限制原图大小并验证内容；丢失或损坏的文件留给原有修复流程。
func (m *catalogImageMigration) read(path string) (storedImage, []byte, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		m.result.Missing++
		return storedImage{}, nil, nil
	}
	if err != nil {
		return storedImage{}, nil, errors.New("read legacy image failed")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxArtworkBytes+1))
	if err != nil {
		return storedImage{}, nil, errors.New("read legacy image failed")
	}
	stored, err := prepareStoredImage(data)
	if err != nil {
		m.result.Missing++
		return storedImage{}, nil, nil
	}
	return stored, data, nil
}

func (m *catalogImageMigration) copy(source, key string, data []byte) error {
	if err := m.db.Statement.Context.Err(); err != nil {
		return err
	}
	path, err := storedImagePath(m.dataDir, key)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("read migrated image failed")
	}
	if err := copyStoredImage(source, path, data, ".catalog-image-*.tmp"); err != nil {
		return errors.New("write migrated image failed")
	}
	return nil
}

// artwork 按已有引用的来源拆分旧全局资产；没有来源的历史资产明确归入 legacy。
func (m *catalogImageMigration) artwork() error {
	var rows []model.ArtworkAsset
	return m.db.Where("storage_key NOT LIKE 'catalogs/%'").FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
		ids := make([]string, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		var owners []struct{ AssetID, Source string }
		if err := m.db.Raw(`SELECT asset_id, COALESCE(source_provider, '') AS source FROM metadata_artworks WHERE asset_id IN ?
UNION SELECT asset_id, COALESCE(source_provider, '') FROM metadata_artwork_candidates WHERE asset_id IN ?
UNION SELECT poster_asset_id, 'douban' FROM douban_subjects WHERE poster_asset_id IN ?
UNION SELECT poster_asset_id, 'local' FROM nfo_items WHERE poster_asset_id IN ?
UNION SELECT backdrop_asset_id, 'local' FROM nfo_items WHERE backdrop_asset_id IN ?
UNION SELECT poster_asset_id, 'local' FROM nfo_media_bindings WHERE poster_asset_id IN ?
UNION SELECT backdrop_asset_id, 'local' FROM nfo_media_bindings WHERE backdrop_asset_id IN ?`, ids, ids, ids, ids, ids, ids, ids).Scan(&owners).Error; err != nil {
			return err
		}
		byID := make(map[string][]string)
		for _, owner := range owners {
			byID[owner.AssetID] = append(byID[owner.AssetID], owner.Source)
		}
		type artworkPlan struct {
			row     model.ArtworkAsset
			stored  storedImage
			sources []string
			keys    map[string]string
			primary string
		}
		var plans []artworkPlan
		var targetKeys []string
		for _, row := range rows {
			path, err := managedImagePath(m.dataDir, "artwork", row.StorageKey)
			if err != nil {
				return err
			}
			stored, data, err := m.read(path)
			if err != nil {
				return err
			}
			if data == nil {
				continue
			}
			if stored.SHA256 != row.SHA256 {
				m.result.Missing++
				continue
			}
			sources := byID[row.ID]
			if len(sources) == 0 {
				sources = []string{"legacy"}
			}
			keys := make(map[string]string)
			for _, source := range sources {
				provider := source
				if strings.TrimSpace(provider) == "" {
					provider = "legacy"
				}
				key, err := catalogImageKey(provider, "artwork", stored.StorageKey)
				if err != nil {
					return err
				}
				keys[source] = key
				if err := m.copy(path, key, data); err != nil {
					return err
				}
			}
			sort.Strings(sources)
			primary := keys[sources[0]]
			plans = append(plans, artworkPlan{row, stored, sources, keys, primary})
			targetKeys = append(targetKeys, primary)
		}
		if len(plans) == 0 {
			return nil
		}
		var existing []string
		if err := m.db.Model(&model.ArtworkAsset{}).Where("storage_key IN ?", targetKeys).Pluck("storage_key", &existing).Error; err != nil {
			return err
		}
		exists := make(map[string]bool)
		for _, key := range existing {
			exists[key] = true
		}
		var changes []catalogImageKeyChange
		for _, plan := range plans {
			row, stored, sources, keys, primary := plan.row, plan.stored, plan.sources, plan.keys, plan.primary
			singleTarget := true
			for _, key := range keys {
				singleTarget = singleTarget && key == primary
			}
			if singleTarget && !exists[primary] {
				changes = append(changes, catalogImageKeyChange{row.ID, row.StorageKey, primary})
				exists[primary] = true
				continue
			}
			// 先提交普通记录，使同批次后续拆分也能查到其目标资产。
			if err := m.updateKeys("artwork_assets", "storage_key", changes); err != nil {
				return err
			}
			changes = nil
			// 保留原 ID；若目标已由后续补图建立，则原 ID 归入明确的历史目录。
			if exists[primary] {
				path, err := managedImagePath(m.dataDir, "artwork", primary)
				if err != nil {
					return err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return errors.New("read migrated artwork for split failed")
				}
				primary, err = catalogImageKey("legacy", "artwork", stored.StorageKey)
				if err != nil {
					return err
				}
				if err := m.copy(path, primary, data); err != nil {
					return err
				}
			}
			if err := m.db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&model.ArtworkAsset{}).Where("id = ? AND storage_key = ?", row.ID, row.StorageKey).UpdateColumn("storage_key", primary).Error; err != nil {
					return err
				}
				for _, source := range sources {
					// 保留原资产 ID 的来源无需重建资产或重写关联，别名也共用此路径。
					if keys[source] == primary {
						continue
					}
					asset := model.ArtworkAsset{StorageKey: keys[source], SHA256: stored.SHA256, MimeType: stored.MimeType, Width: stored.Width, Height: stored.Height, SizeBytes: stored.SizeBytes}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&asset).Error; err != nil {
						return err
					}
					var saved model.ArtworkAsset
					if err := tx.Where("storage_key = ? AND sha256 = ?", keys[source], stored.SHA256).First(&saved).Error; err != nil {
						return err
					}
					for _, table := range []string{"metadata_artworks", "metadata_artwork_candidates"} {
						if err := tx.Table(table).Where("asset_id = ? AND COALESCE(source_provider, '') = ?", row.ID, source).UpdateColumn("asset_id", saved.ID).Error; err != nil {
							return err
						}
					}
					if source == "douban" {
						if err := tx.Model(&model.DoubanSubject{}).Where("poster_asset_id = ?", row.ID).UpdateColumn("poster_asset_id", saved.ID).Error; err != nil {
							return err
						}
					}
					if source == "local" {
						for _, table := range []string{"nfo_items", "nfo_media_bindings"} {
							for _, column := range []string{"poster_asset_id", "backdrop_asset_id"} {
								if err := tx.Table(table).Where(column+" = ?", row.ID).UpdateColumn(column, saved.ID).Error; err != nil {
									return err
								}
							}
						}
					}
				}
				return nil
			}); err != nil {
				return err
			}
			m.result.Migrated++
			exists[primary] = true
			for _, key := range keys {
				exists[key] = true
			}
		}
		return m.updateKeys("artwork_assets", "storage_key", changes)
	}).Error
}

func (m *catalogImageMigration) migrateKey(root, provider, category, old string) (string, error) {
	path, err := storedImagePath(root, old)
	if err != nil {
		return "", err
	}
	stored, data, err := m.read(path)
	if err != nil || data == nil {
		return "", err
	}
	// 历史键包含内容哈希，损坏但仍可解码的图片也不能悄悄改成新内容。
	if filepath.Base(old) != filepath.Base(stored.StorageKey) {
		m.result.Missing++
		return "", nil
	}
	key, err := catalogImageKey(provider, category, stored.StorageKey)
	if err != nil {
		return "", err
	}
	return key, m.copy(path, key, data)
}

func (m *catalogImageMigration) people() error {
	var rows []model.Person
	return m.db.Unscoped().Where("profile_image_key <> '' AND profile_image_key NOT LIKE 'catalogs/%'").FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
		var changes []catalogImageKeyChange
		for _, row := range rows {
			key, err := m.migrateKey(filepath.Join(m.dataDir, "people"), row.Source, "people", row.ProfileImageKey)
			if err != nil {
				return err
			}
			if key == "" {
				continue
			}
			changes = append(changes, catalogImageKeyChange{row.ID, row.ProfileImageKey, key})
		}
		return m.updateKeys("people", "profile_image_key", changes)
	}).Error
}

func (m *catalogImageMigration) doubanCredits() error {
	var rows []model.DoubanSubject
	return m.db.Where(`EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(rich_payload->'credits', '[]'::jsonb)) AS c WHERE c->>'image_key' <> '' AND c->>'image_key' NOT LIKE 'catalogs/%')`).FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
		for _, row := range rows {
			// 修改单个 key，保留未知补充字段和所有成功/失败时间。
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(*row.RichPayload), &payload); err != nil {
				return err
			}
			var credits []map[string]json.RawMessage
			if err := json.Unmarshal(payload["credits"], &credits); err != nil {
				return err
			}
			changed := false
			for _, credit := range credits {
				var old string
				if err := json.Unmarshal(credit["image_key"], &old); err != nil || old == "" || strings.HasPrefix(old, "catalogs/") {
					continue
				}
				key, err := m.migrateKey(filepath.Join(m.dataDir, "people"), "douban", "people", old)
				if err != nil {
					return err
				}
				if key == "" {
					continue
				}
				credit["image_key"], _ = json.Marshal(key)
				changed = true
			}
			if !changed {
				continue
			}
			payload["credits"], _ = json.Marshal(credits)
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if err := m.db.Model(&model.DoubanSubject{}).Where("id = ? AND rich_payload = ?::jsonb", row.ID, *row.RichPayload).UpdateColumn("rich_payload", string(raw)).Error; err != nil {
				return err
			}
			m.result.Migrated++
		}
		return nil
	}).Error
}

func (m *catalogImageMigration) catalogs() error {
	for _, provider := range []string{"hongguo", "huangguoai"} {
		var rows []struct {
			ID, LocalKey string
			PersonID     *string
		}
		query := m.db.Table(provider + "_artworks").Select("id, local_key")
		if provider == "hongguo" {
			query = query.Select("id, local_key, person_id")
		}
		if err := query.Where("local_key <> '' AND local_key NOT LIKE 'catalogs/%'").FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
			var changes []catalogImageKeyChange
			for _, row := range rows {
				category := "artwork"
				if row.PersonID != nil {
					category = "people"
				}
				key, err := m.migrateKey(filepath.Join(m.dataDir, "catalogs", provider, "artwork"), provider, category, row.LocalKey)
				if err != nil {
					return err
				}
				if key == "" {
					continue
				}
				changes = append(changes, catalogImageKeyChange{row.ID, row.LocalKey, key})
			}
			return m.updateKeys(provider+"_artworks", "local_key", changes)
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (m *catalogImageMigration) libraryCovers() error {
	svc := &MediaService{cfg: &config.Config{App: config.AppConfig{DataDir: m.dataDir}}}
	var rows []model.Library
	return m.db.Unscoped().Where("cover_url LIKE '/api/libraries/%/cover?v=%'").FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
		var changes []catalogImageKeyChange
		for _, row := range rows {
			version, ok := libraryCoverVersion(row.CoverURL, row.ID)
			if !ok || filepath.Ext(version) != "" {
				continue
			}
			path, err := svc.libraryCoverPath(row.ID, version)
			if err != nil {
				return err
			}
			stored, data, err := m.read(path)
			if err != nil {
				return err
			}
			if data == nil {
				continue
			}
			if stored.SHA256 != version {
				m.result.Missing++
				continue
			}
			key, err := catalogImageKey("local", "artwork", stored.StorageKey)
			if err != nil {
				return err
			}
			if err := m.copy(path, key, data); err != nil {
				return err
			}
			url := libraryCoverURL(row.ID) + "?v=" + filepath.Base(stored.StorageKey)
			changes = append(changes, catalogImageKeyChange{row.ID, row.CoverURL, url})
		}
		return m.updateKeys("libraries", "cover_url", changes)
	}).Error
}
