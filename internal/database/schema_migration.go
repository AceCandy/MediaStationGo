package database

import (
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// AutoMigrate creates tables for every model registered in the model package.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return err
	}
	if err := ensurePostgresColumnCompatibility(db); err != nil {
		return err
	}
	if err := enforceTelegramBindingOneToOne(db); err != nil {
		return err
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		return err
	}
	if err := ensureLibraryRootsCompatibility(db); err != nil {
		return err
	}
	if isSQLite(db) {
		return ensureMediaSearchIndex(db)
	}
	return nil
}

func ensurePostgresColumnCompatibility(db *gorm.DB) error {
	if !isPostgres(db) {
		return nil
	}
	statements := []string{
		`ALTER TABLE media ALTER COLUMN container TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN series_hint TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN duplicate_of TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN metadata_id DROP NOT NULL`,
		`ALTER TABLE playback_histories ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE favorites ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE playlist_items ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE strm_records ALTER COLUMN media_id TYPE varchar(128)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensurePerformanceIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_library_created_active ON media(library_id, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_scan_year_active ON media(library_id, scan_year DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_episode_active ON media(library_id, season_num, episode_num, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_root_active ON media(library_id, library_root_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_metadata_active ON media(metadata_id, library_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_series_hint_active ON media(series_hint, season_num, episode_num) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_kind_release_active ON metadata_items(kind, release_date DESC, year DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_parent_season_active ON metadata_items(parent_id, season_num) WHERE deleted_at IS NULL AND kind = 'season'`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_parent_episode_active ON metadata_items(parent_id, episode_num) WHERE deleted_at IS NULL AND kind = 'episode'`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_user_media_active ON favorites(user_id, media_id) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_favorites_user_metadata_active ON favorites(user_id, metadata_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_media_active ON playback_histories(user_id, media_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_metadata_active ON playback_histories(user_id, metadata_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_playback_histories_user_metadata_active ON playback_histories(user_id, metadata_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_resume_active ON playback_histories(user_id, completed, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_play_profiles_user_created_active ON play_profiles(user_id, created_at DESC) WHERE deleted_at IS NULL`,
	}
	if db.Migrator().HasTable(&model.PlaylistItem{}) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_playlist_items_metadata_active ON playlist_items(metadata_id, playlist_id) WHERE deleted_at IS NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uniq_playlist_items_metadata_active ON playlist_items(playlist_id, metadata_id) WHERE deleted_at IS NULL`,
		)
	}
	if isSQLite(db) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_metadata_title_active ON metadata_items(title COLLATE NOCASE) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_metadata_original_name_active ON metadata_items(original_name COLLATE NOCASE) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_media_scan_title_active ON media(scan_title COLLATE NOCASE) WHERE deleted_at IS NULL`,
		)
	} else {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_metadata_title_active ON metadata_items(title) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_metadata_original_name_active ON metadata_items(original_name) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_media_scan_title_active ON media(scan_title) WHERE deleted_at IS NULL`,
		)
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func enforceTelegramBindingOneToOne(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.TelegramBinding{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
DELETE FROM telegram_bindings
WHERE deleted_at IS NULL
  AND user_id IN (
    SELECT user_id
    FROM telegram_bindings
    WHERE deleted_at IS NULL
    GROUP BY user_id
    HAVING COUNT(*) > 1
  )
  AND id NOT IN (
    SELECT id
    FROM (
      SELECT id,
             ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY updated_at DESC, created_at DESC, id DESC) AS rn
      FROM telegram_bindings
      WHERE deleted_at IS NULL
    ) AS ranked_bindings
    WHERE rn = 1
  )
`).Error; err != nil {
			return err
		}
		return tx.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_bindings_user_id_active
ON telegram_bindings(user_id)
WHERE deleted_at IS NULL
`).Error
	})
}
