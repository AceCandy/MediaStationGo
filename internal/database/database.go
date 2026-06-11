// Package database wires up GORM against SQLite (WAL mode) and exposes the
// auto-migration entry point used at startup.
package database

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Open initialises the SQLite database file applying WAL pragmas for
// better concurrent read performance — same defaults as nowen-video.
func Open(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	dsn := buildDSN(cfg)

	gormLogger := logger.New(
		zapStdLogger{log: log},
		logger.Config{
			SlowThreshold:             0,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                                   gormLogger,
		PrepareStmt:                              true,
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	installSQLiteWriteGate(db)
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gorm sqldb: %w", err)
	}
	if cfg.Database.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	}
	return db, nil
}

func installSQLiteWriteGate(db *gorm.DB) {
	if db == nil {
		return
	}
	gate := &sqliteWriteGate{}
	lock := func(tx *gorm.DB) {
		gate.Lock()
	}
	unlock := func(tx *gorm.DB) {
		gate.Unlock()
	}
	_ = db.Callback().Create().Before("gorm:create").Register("mediastation:sqlite_write_lock", lock)
	_ = db.Callback().Create().After("gorm:create").Register("mediastation:sqlite_write_unlock", unlock)
	_ = db.Callback().Update().Before("gorm:update").Register("mediastation:sqlite_write_lock", lock)
	_ = db.Callback().Update().After("gorm:update").Register("mediastation:sqlite_write_unlock", unlock)
	_ = db.Callback().Delete().Before("gorm:delete").Register("mediastation:sqlite_write_lock", lock)
	_ = db.Callback().Delete().After("gorm:delete").Register("mediastation:sqlite_write_unlock", unlock)
	_ = db.Callback().Raw().Before("gorm:raw").Register("mediastation:sqlite_write_lock", lock)
	_ = db.Callback().Raw().After("gorm:raw").Register("mediastation:sqlite_write_unlock", unlock)
}

type sqliteWriteGate struct {
	mu sync.Mutex
}

func (g *sqliteWriteGate) Lock() {
	g.mu.Lock()
}

func (g *sqliteWriteGate) Unlock() {
	g.mu.Unlock()
}

func buildDSN(cfg *config.Config) string {
	dbPath := cfg.Database.DBPath
	if !filepath.IsAbs(dbPath) {
		// keep as-is to respect user-provided relative paths.
		dbPath = filepath.Clean(dbPath)
	}
	dsn := dbPath + "?_pragma=foreign_keys(1)"
	if cfg.Database.WALMode {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	if cfg.Database.BusyTimeout > 0 {
		dsn += fmt.Sprintf("&_pragma=busy_timeout(%d)", cfg.Database.BusyTimeout)
	}
	if cfg.Database.CacheSize != 0 {
		dsn += fmt.Sprintf("&_pragma=cache_size(%d)", cfg.Database.CacheSize)
	}
	return dsn
}

// AutoMigrate creates tables for every model registered in the model package.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return err
	}
	if err := enforceTelegramBindingOneToOne(db); err != nil {
		return err
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		return err
	}
	return ensureMediaSearchIndex(db)
}

func ensurePerformanceIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_library_created_active ON media(library_id, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_episode_active ON media(library_id, season_num, episode_num, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_series_active ON media(series_id, season_num, episode_num) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_title_active ON media(title COLLATE NOCASE) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_original_name_active ON media(original_name COLLATE NOCASE) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_user_media_active ON favorites(user_id, media_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_media_active ON playback_histories(user_id, media_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_resume_active ON playback_histories(user_id, completed, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_play_profiles_user_created_active ON play_profiles(user_id, created_at DESC) WHERE deleted_at IS NULL`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureMediaSearchIndex(db *gorm.DB) error {
	if mediaSearchIndexNeedsRebuild(db) {
		_ = db.Exec(`DROP TABLE IF EXISTS media_search_fts`).Error
	}
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(media_id UNINDEXED, title, original_name, path, genres, tokenize='trigram')`).Error; err != nil {
		if fallbackErr := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(media_id UNINDEXED, title, original_name, path, genres, tokenize='unicode61')`).Error; fallbackErr != nil {
			// FTS is an acceleration path. Some embedded SQLite builds may omit
			// FTS5; keep startup working and let repository queries fall back to
			// LIKE-based Chinese fuzzy search.
			return nil
		}
	}
	return nil
}

func mediaSearchIndexNeedsRebuild(db *gorm.DB) bool {
	var cols []struct {
		Name string
	}
	if err := db.Raw(`PRAGMA table_info(media_search_fts)`).Scan(&cols).Error; err != nil || len(cols) == 0 {
		return false
	}
	for _, col := range cols {
		if col.Name == "genres" {
			return false
		}
	}
	return true
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
             ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY created_at ASC, id ASC) AS rn
      FROM telegram_bindings
      WHERE deleted_at IS NULL
    )
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

// zapStdLogger adapts a *zap.Logger to GORM's tiny logger interface.
type zapStdLogger struct{ log *zap.Logger }

func (z zapStdLogger) Printf(format string, args ...interface{}) {
	z.log.Sugar().Infof(format, args...)
}
