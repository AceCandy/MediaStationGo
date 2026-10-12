package database

import "gorm.io/gorm"

// retireCatalogHistory 删除已完成导入的历史副本，中心资料和本地业务关联保持独立。
func retireCatalogHistory(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS metadata_provider_snapshots, tmdb_migration_failures`,
			`DELETE FROM settings WHERE key IN ('metadata.tmdb_center_migration_v1', 'metadata.tmdb_center_migration_enabled', 'metadata.tmdb_center_migration_interval_seconds', 'internal.tmdb_snapshot_backfill_completed')`,
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
