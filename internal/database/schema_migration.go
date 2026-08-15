package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// AutoMigrate creates tables for every model registered in the model package.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return err
	}
	if err := ensurePlayerRequestLogSchema(db); err != nil {
		return err
	}
	if err := removeLegacyMetadataIdentityConstraint(db); err != nil {
		return err
	}
	if err := migrateLegacyEpisodeTitle(db); err != nil {
		return err
	}
	if err := ensureAPIConfigColumns(db); err != nil {
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
	if err := removeCloudStorageSchema(db); err != nil {
		return err
	}
	if err := ensureLibraryRootsCompatibility(db); err != nil {
		return err
	}
	if err := removeDownloadSubscriptionSchema(db); err != nil {
		return err
	}
	if err := removeSystemUpdateSettings(db); err != nil {
		return err
	}
	if err := removePTSiteSchema(db); err != nil {
		return err
	}
	if err := removeUnusedLegacyColumns(db); err != nil {
		return err
	}
	return nil
}

func ensurePlayerRequestLogSchema(db *gorm.DB) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS player_request_logs (
	id varchar(36) NOT NULL,
	requested_at timestamptz NOT NULL,
	method varchar(16) NOT NULL,
	route text NOT NULL,
	status integer NOT NULL,
	duration_ms bigint NOT NULL,
	ip varchar(64) NOT NULL DEFAULT '',
	path_params jsonb NOT NULL DEFAULT '{}'::jsonb,
	headers jsonb NOT NULL DEFAULT '{}'::jsonb,
	query jsonb NOT NULL DEFAULT '{}'::jsonb,
	PRIMARY KEY (id, requested_at)
) PARTITION BY RANGE (requested_at)`,
		`CREATE INDEX IF NOT EXISTS idx_player_request_logs_requested_at ON player_request_logs (requested_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_player_request_logs_method_time ON player_request_logs (method, requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_player_request_logs_status_time ON player_request_logs (status, requested_at DESC)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return EnsurePlayerRequestLogPartitions(db, time.Now().UTC())
}

// EnsurePlayerRequestLogPartitions 幂等创建指定月份及下一月份的日志分区。
func EnsurePlayerRequestLogPartitions(db *gorm.DB, at time.Time) error {
	month := time.Date(at.UTC().Year(), at.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, start := range []time.Time{month, month.AddDate(0, 1, 0)} {
		end := start.AddDate(0, 1, 0)
		name := fmt.Sprintf("player_request_logs_%04d_%02d", start.Year(), start.Month())
		stmt := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF player_request_logs FOR VALUES FROM ('%s') TO ('%s')`,
			name, start.Format(time.RFC3339), end.Format(time.RFC3339),
		)
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// removePTSiteSchema permanently removes retired tracker credentials and
// their dedicated permission without changing any other user permissions.
func removePTSiteSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS sites CASCADE`,
			`ALTER TABLE IF EXISTS user_permissions DROP COLUMN IF EXISTS can_manage_sites`,
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// removeCloudStorageSchema retires provider-backed storage without touching
// protocol-neutral local, HTTP, or HTTPS media sources.
func removeCloudStorageSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		statements := []string{
			`CREATE TEMP TABLE retired_cloud_roots ON COMMIT DROP AS
SELECT id, library_id
FROM library_roots
WHERE LOWER(BTRIM(path)) LIKE 'cloud://%'`,
			`CREATE TEMP TABLE retired_cloud_media ON COMMIT DROP AS
SELECT DISTINCT m.id, m.library_id
FROM media AS m
WHERE LOWER(BTRIM(m.path)) LIKE 'cloud://%'
   OR m.library_root_id IN (SELECT id FROM retired_cloud_roots)
   OR EXISTS (
       SELECT 1
       FROM strm_records AS sr
       WHERE sr.media_id = m.id
         AND LOWER(BTRIM(sr.protocol)) IN ('alist', 'alists', 'openlist', 'openlists', 'webdav', 'davs', 's3')
   )`,
			`CREATE TEMP TABLE retired_cloud_libraries ON COMMIT DROP AS
SELECT DISTINCT id
FROM (
    SELECT id FROM libraries WHERE LOWER(BTRIM(path)) LIKE 'cloud://%'
    UNION
    SELECT library_id FROM retired_cloud_roots WHERE BTRIM(COALESCE(library_id, '')) <> ''
    UNION
    SELECT library_id FROM retired_cloud_media WHERE BTRIM(COALESCE(library_id, '')) <> ''
) AS candidates`,
			`DELETE FROM media_probe_metadata WHERE media_id IN (SELECT id FROM retired_cloud_media)`,
			`DELETE FROM strm_records
WHERE media_id IN (SELECT id FROM retired_cloud_media)
   OR LOWER(BTRIM(protocol)) IN ('alist', 'alists', 'openlist', 'openlists', 'webdav', 'davs', 's3')`,
			`DELETE FROM media WHERE id IN (SELECT id FROM retired_cloud_media)`,
			`DELETE FROM library_roots WHERE id IN (SELECT id FROM retired_cloud_roots)`,
		}
		for _, stmt := range statements {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}

		var unsafeLibraries int64
		if err := tx.Raw(`
SELECT COUNT(*)
FROM retired_cloud_libraries AS retired
WHERE EXISTS (SELECT 1 FROM media WHERE library_id = retired.id)
  AND NOT EXISTS (SELECT 1 FROM library_roots WHERE library_id = retired.id)
`).Scan(&unsafeLibraries).Error; err != nil {
			return err
		}
		if unsafeLibraries > 0 {
			return fmt.Errorf("cloud storage retirement found %d libraries with media but no surviving root", unsafeLibraries)
		}

		for _, stmt := range []string{
			`UPDATE libraries AS l
SET path = (
    SELECT r.path
    FROM library_roots AS r
    WHERE r.library_id = l.id
    ORDER BY r.sort_order, r.created_at, r.id
    LIMIT 1
)
WHERE l.id IN (SELECT id FROM retired_cloud_libraries)
  AND EXISTS (SELECT 1 FROM library_roots WHERE library_id = l.id)`,
			`DELETE FROM libraries AS l
WHERE l.id IN (SELECT id FROM retired_cloud_libraries)
  AND NOT EXISTS (SELECT 1 FROM library_roots WHERE library_id = l.id)
  AND NOT EXISTS (SELECT 1 FROM media WHERE library_id = l.id)`,
			`DELETE FROM settings
WHERE LOWER(key) LIKE 'cloud.%'
   OR LOWER(key) LIKE 'app.cloud\_%' ESCAPE '\'
   OR LOWER(key) LIKE 'transcode.%'
   OR LOWER(key) LIKE 'transcoder.%'
   OR LOWER(key) IN ('ffmpeg.path', 'app.ffmpeg_path')`,
			`DROP TABLE IF EXISTS storage_configs CASCADE`,
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// removeLegacyMetadataIdentityConstraint removes the old named check after
// AutoMigrate has installed its replacement.
func removeLegacyMetadataIdentityConstraint(db *gorm.DB) error {
	const name = "chk_metadata_identity"
	if !db.Migrator().HasConstraint(&model.MetadataItem{}, name) {
		return nil
	}
	return db.Migrator().DropConstraint(&model.MetadataItem{}, name)
}

// migrateLegacyEpisodeTitle preserves old Episode names before removing the
// redundant column. The update and drop are atomic to avoid partial upgrades.
func migrateLegacyEpisodeTitle(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&model.MetadataItem{}, "episode_title") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
UPDATE metadata_items
SET title = BTRIM(episode_title)
WHERE kind = 'episode'
  AND BTRIM(COALESCE(episode_title, '')) <> ''
`).Error; err != nil {
			return err
		}
		return tx.Exec(`ALTER TABLE metadata_items DROP COLUMN episode_title`).Error
	})
}

// removeDownloadSubscriptionSchema removes the tables and columns owned by
// the retired subscription/download feature. AutoMigrate only creates or
// alters models, so these explicitly destructive removals are kept here as a
// one-time compatibility migration for existing installations.
func removeDownloadSubscriptionSchema(db *gorm.DB) error {
	statements := []string{
		`DROP TABLE IF EXISTS download_tasks CASCADE`,
		`DROP TABLE IF EXISTS download_clients CASCADE`,
		`DROP TABLE IF EXISTS subscriptions CASCADE`,
		`ALTER TABLE IF EXISTS user_permissions DROP COLUMN IF EXISTS can_manage_downloads`,
		`ALTER TABLE IF EXISTS user_permissions DROP COLUMN IF EXISTS can_manage_subscriptions`,
		`DELETE FROM settings WHERE key LIKE 'subscription.%' OR key LIKE 'qbittorrent.%' OR key LIKE 'transmission.%' OR key LIKE 'aria2.%' OR key IN ('organize.keep_seeding', 'downloads.smart_classify', 'organizer.auto_after_download')`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// removeSystemUpdateSettings 删除已退役系统更新功能留下的配置。
func removeSystemUpdateSettings(db *gorm.DB) error {
	return db.Exec(`DELETE FROM settings WHERE key IN ('system.update.image', 'system.update.watchtower_image', 'system.update.command', 'system.update.compose_dir')`).Error
}

// removeUnusedLegacyColumns removes columns that have no current model or
// runtime consumer. AutoMigrate never drops columns removed from a model.
func removeUnusedLegacyColumns(db *gorm.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE IF EXISTS people DROP COLUMN IF EXISTS profile_image_source`,
		`ALTER TABLE IF EXISTS user_devices DROP COLUMN IF EXISTS warnings`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureAPIConfigColumns covers databases created before the API config model
// gained editable model and web-search fields. GORM's AutoMigrate can skip
// this when the legacy duplicate config model already owns the table.
func ensureAPIConfigColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.APIConfig{}) {
		return nil
	}
	for _, column := range []string{"Model", "WebSearchEnabled"} {
		if db.Migrator().HasColumn(&model.APIConfig{}, column) {
			continue
		}
		if err := db.Migrator().AddColumn(&model.APIConfig{}, column); err != nil {
			return err
		}
	}
	return nil
}

func ensurePostgresColumnCompatibility(db *gorm.DB) error {
	statements := []string{
		`ALTER TABLE media ALTER COLUMN container TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN series_hint TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN duplicate_of TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN metadata_id DROP NOT NULL`,
		`ALTER TABLE playback_histories ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE favorites ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE playlist_items ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE strm_records ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE metadata_credits ALTER COLUMN original_role TYPE text`,
		`ALTER TABLE metadata_credits ALTER COLUMN role TYPE text`,
		`ALTER TABLE translation_caches ALTER COLUMN source_text TYPE text`,
		`ALTER TABLE translation_caches ALTER COLUMN translated_text TYPE text`,
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
	statements = append(statements,
		`CREATE INDEX IF NOT EXISTS idx_metadata_title_active ON metadata_items(title) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_original_name_active ON metadata_items(original_name) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_scan_title_active ON media(scan_title) WHERE deleted_at IS NULL`,
	)
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
