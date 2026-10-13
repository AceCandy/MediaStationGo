package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// AutoMigrate 建立现行模型、业务约束和增量维护，不转换旧版数据结构。
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return err
	}
	if err := reclassifyTaskSystems(db); err != nil {
		return err
	}
	if err := ensureHuangGuoAIBindingIdentity(db); err != nil {
		return err
	}
	// 当前 GORM 不识别带数字的约束名，显式维护现行海报归属约束。
	if err := db.Exec(`DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_hongguo_artwork_owner_v2' AND conrelid='hongguo_artworks'::regclass) THEN
 ALTER TABLE hongguo_artworks ADD CONSTRAINT chk_hongguo_artwork_owner_v2 CHECK (
 (person_id IS NOT NULL AND work_id IS NULL AND source_id IS NULL) OR
 (person_id IS NULL AND (source_id IS NOT NULL OR work_id IS NOT NULL)));
 END IF; END $$`).Error; err != nil {
		return err
	}
	if err := ensurePlayerRequestLogSchema(db); err != nil {
		return err
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		return err
	}
	if err := EnsureTMDbRecheckTriggers(db); err != nil {
		return err
	}
	if err := EnsureDoubanCatalogSchema(db); err != nil {
		return err
	}
	if err := ensureTMDbCatalogSchema(db); err != nil {
		return err
	}
	if err := EnsurePlaybackProgressRevisionTriggers(db); err != nil {
		return err
	}
	return EnsureLatestMediaAddedTriggers(db)
}

// reclassifyTaskSystems 按现行任务归属更新执行历史，任务标识和日志路径保持稳定。
func reclassifyTaskSystems(db *gorm.DB) error {
	var kinds []string
	if err := db.Model(&model.TaskExecution{}).Distinct("kind").Pluck("kind", &kinds).Error; err != nil {
		return err
	}
	for _, kind := range kinds {
		system := model.TaskSystemForKind(kind)
		if err := db.Model(&model.TaskExecution{}).Where("kind = ? AND system IS DISTINCT FROM ?", kind, system).UpdateColumn("system", system).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensurePlayerRequestLogSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// 串行化多实例的序列与分区迁移，避免重复初始化。
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('player_request_logs_schema'))`).Error; err != nil {
			return err
		}
		for _, stmt := range []string{
			`CREATE SEQUENCE IF NOT EXISTS player_request_logs_serial_no_seq AS bigint`,
			`CREATE TABLE IF NOT EXISTS player_request_logs (
	serial_no bigint NOT NULL DEFAULT nextval('player_request_logs_serial_no_seq'),
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
	body text NOT NULL DEFAULT '',
	response_body text NOT NULL DEFAULT '',
	PRIMARY KEY (id, requested_at)
) PARTITION BY RANGE (requested_at)`,
			`ALTER SEQUENCE player_request_logs_serial_no_seq OWNED BY player_request_logs.serial_no`,
			`CREATE INDEX IF NOT EXISTS idx_player_request_logs_serial_no ON player_request_logs (serial_no)`,
			`CREATE INDEX IF NOT EXISTS idx_player_request_logs_requested_at ON player_request_logs (requested_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_player_request_logs_method_time ON player_request_logs (method, requested_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_player_request_logs_status_time ON player_request_logs (status, requested_at DESC)`,
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return EnsurePlayerRequestLogPartitions(tx, time.Now().UTC())
	})
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

func ensurePerformanceIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_library_created_active ON media(library_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_parent_path ON media(library_id, lower(regexp_replace(path, '[^/]*$', '')))`,
		`CREATE INDEX IF NOT EXISTS idx_media_recent_metadata ON media(created_at DESC, id DESC) WHERE metadata_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_people_pending_translation ON people(id) WHERE deleted_at IS NULL AND original_name <> '' AND name = original_name AND original_name !~ '[一-鿿]'`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_credits_type_pending_translation ON metadata_credits(type, metadata_id, id) WHERE original_role <> '' AND role = original_role AND original_role !~ '[一-鿿]'`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_scan_year_active ON media(library_id, scan_year DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_episode_active ON media(library_id, season_num, episode_num, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_root_active ON media(library_id, library_root_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_metadata_active ON media(metadata_id, library_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_series_hint_active ON media(series_hint, season_num, episode_num)`,
		`CREATE INDEX IF NOT EXISTS idx_media_scrape_pending_pick ON media ((CASE WHEN COALESCE(season_num, 0) > 0 OR COALESCE(episode_num, 0) > 0 THEN 1 ELSE 0 END), id) WHERE COALESCE(catalog_source, '') = '' AND (scrape_status IS NULL OR scrape_status = '' OR scrape_status = 'pending')`,
		`CREATE INDEX IF NOT EXISTS idx_media_scrape_group ON media (BTRIM(COALESCE(series_hint, '')), BTRIM(COALESCE(metadata_id, '')), id) WHERE COALESCE(catalog_source, '') = '' AND (scrape_status IS NULL OR scrape_status = '' OR scrape_status = 'pending')`,
		`CREATE INDEX IF NOT EXISTS idx_media_scrape_running ON media (id) WHERE COALESCE(catalog_source, '') = '' AND scrape_status = 'running'`,
		`CREATE INDEX IF NOT EXISTS idx_media_scrape_issues ON media (library_id, updated_at DESC, id DESC) WHERE scrape_status IN ('error', 'no_match')`,
		`CREATE INDEX IF NOT EXISTS idx_media_probe_automatic_candidates ON media (id) INCLUDE (library_id, metadata_id) WHERE COALESCE(episode_num, 0) = 0 AND LOWER(path) NOT LIKE '%.iso'`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_kind_release_active ON metadata_items(kind, release_date DESC, year DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_parent_season_active ON metadata_items(parent_id, season_num) WHERE kind = 'season'`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_parent_episode_active ON metadata_items(parent_id, episode_num) WHERE kind = 'episode'`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_recheck_kind_id ON metadata_items(kind, id) WHERE kind IN ('episode', 'season')`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_user_media_active ON favorites(user_id, media_id) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_favorites_user_metadata_active ON favorites(user_id, metadata_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_media_active ON playback_histories(user_id, media_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_metadata_active ON playback_histories(user_id, metadata_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_playback_histories_user_metadata_active ON playback_histories(user_id, metadata_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_resume_active ON playback_histories(user_id, completed, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_play_profiles_user_created_active ON play_profiles(user_id, created_at DESC) WHERE deleted_at IS NULL`,
	}
	if db.Migrator().HasTable(&model.TaskExecution{}) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_task_executions_kind_latest ON task_executions(kind, started_at DESC, id DESC) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_task_executions_kind_name_latest ON task_executions(kind, name, started_at DESC, id DESC) WHERE deleted_at IS NULL`,
		)
	}
	if db.Migrator().HasTable(&model.PlaylistItem{}) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_playlist_items_metadata_active ON playlist_items(metadata_id, playlist_id) WHERE deleted_at IS NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uniq_playlist_items_metadata_active ON playlist_items(playlist_id, metadata_id) WHERE deleted_at IS NULL`,
		)
	}
	if db.Migrator().HasTable(&model.HongGuoDownload{}) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_hg_download_work_created_c ON hongguo_downloads(source_id COLLATE "C", created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_hg_download_status_work_c ON hongguo_downloads(status, source_id COLLATE "C")`,
			`CREATE INDEX IF NOT EXISTS idx_hg_download_transfer_claim ON hongguo_downloads(created_at, id) WHERE status IN ('queued', 'waiting_verify', 'downloading', 'verifying', 'publishing') AND raw_size = 0 AND COALESCE(sha256, '') = ''`,
			`CREATE INDEX IF NOT EXISTS idx_hg_download_verification_claim ON hongguo_downloads(created_at, id) WHERE status IN ('queued', 'waiting_verify', 'downloading', 'verifying', 'publishing') AND (raw_size > 0 OR COALESCE(sha256, '') <> '')`,
		)
	}
	if db.Migrator().HasTable(&model.MediaProbeMetadata{}) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_media_probe_nonempty_document ON media_probe_metadata (media_id) INCLUDE (schema_version) WHERE probe_json <> ''`,
		)
	}
	statements = append(statements,
		`CREATE INDEX IF NOT EXISTS idx_metadata_title_active ON metadata_items(title)`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_original_name_active ON metadata_items(original_name)`,
		`CREATE INDEX IF NOT EXISTS idx_media_scan_title_active ON media(scan_title)`,
	)
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
