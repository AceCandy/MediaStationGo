// Package database wires up GORM against the configured database and exposes
// startup migration helpers.
package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Open initialises the configured GORM database. database.type=auto chooses
// PostgreSQL when database.dsn is present (the Docker Compose default) and
// otherwise falls back to SQLite for old/bare-metal installs.
func Open(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	gormLogger := logger.New(
		zapStdLogger{log: log},
		logger.Config{
			SlowThreshold:             0,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	dialect := normalizeDatabaseType(cfg.Database.Type)
	if dialect == "auto" {
		dialect = effectiveAutoDatabaseType(cfg)
	}
	dialector, err := databaseDialector(cfg, dialect)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:                                   gormLogger,
		PrepareStmt:                              true,
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	if dialect == "sqlite" {
		installSQLiteWriteGate(db)
	}
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

func normalizeDatabaseType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto"
	case "sqlite", "sqlite3":
		return "sqlite"
	case "postgres", "postgresql", "pg":
		return "postgres"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func effectiveAutoDatabaseType(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.Database.DSN) != "" {
		return "postgres"
	}
	return "sqlite"
}

func databaseDialector(cfg *config.Config, dialect string) (gorm.Dialector, error) {
	switch dialect {
	case "sqlite":
		return sqlite.Open(buildSQLiteDSN(cfg)), nil
	case "postgres":
		dsn := strings.TrimSpace(cfg.Database.DSN)
		if dsn == "" {
			return nil, fmt.Errorf("database.dsn is required when database.type=postgres")
		}
		return postgres.Open(dsn), nil
	default:
		return nil, fmt.Errorf("unsupported database.type %q (supported: sqlite, postgres)", cfg.Database.Type)
	}
}

// MigrateSQLiteToCurrentIfNeeded copies an existing SQLite database into a new
// PostgreSQL database once. It is intentionally conservative: it only runs when
// the current DB is PostgreSQL, the configured SQLite file exists, and the
// target business tables are empty. Redis is not migrated because it is a
// rebuildable cache, not a source of truth.
func MigrateSQLiteToCurrentIfNeeded(cfg *config.Config, target *gorm.DB, log *zap.Logger) error {
	if cfg == nil || target == nil || target.Dialector == nil || target.Dialector.Name() != "postgres" {
		return nil
	}
	sqlitePath := strings.TrimSpace(cfg.Database.DBPath)
	if sqlitePath == "" {
		return nil
	}
	if _, err := os.Stat(sqlitePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat sqlite migration source: %w", err)
	}
	if empty, err := targetBusinessTablesEmpty(target); err != nil {
		return err
	} else if !empty {
		if log != nil {
			log.Info("skip sqlite to postgres migration: target database already has data")
		}
		return nil
	}

	srcCfg := *cfg
	srcCfg.Database.Type = "sqlite"
	src, err := gorm.Open(sqlite.Open(buildSQLiteDSN(&srcCfg)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("open sqlite migration source: %w", err)
	}
	sqlDB, err := src.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	started := time.Now()
	copied, err := copyModelTables(src, target, 500)
	if err != nil {
		return err
	}
	if copied > 0 && log != nil {
		log.Info("sqlite data migrated to postgres",
			zap.String("source", sqlitePath),
			zap.Int64("rows", copied),
			zap.Duration("duration", time.Since(started)))
	}
	return nil
}

func targetBusinessTablesEmpty(db *gorm.DB) (bool, error) {
	for _, m := range model.AllModels() {
		table, err := modelTableName(db, m)
		if err != nil {
			return false, err
		}
		var count int64
		if err := db.Raw("SELECT COUNT(1) FROM " + quoteIdent(table)).Scan(&count).Error; err != nil {
			return false, fmt.Errorf("count target table %s: %w", table, err)
		}
		if count > 0 {
			return false, nil
		}
	}
	return true, nil
}

func copyModelTables(src, target *gorm.DB, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var copied int64
	for _, m := range model.AllModels() {
		table, err := modelTableName(src, m)
		if err != nil {
			return copied, err
		}
		exists, err := sqliteTableExists(src, table)
		if err != nil {
			return copied, err
		}
		if !exists {
			continue
		}
		var sourceCount int64
		if err := src.Raw("SELECT COUNT(1) FROM " + quoteIdent(table)).Scan(&sourceCount).Error; err != nil {
			return copied, fmt.Errorf("count sqlite table %s: %w", table, err)
		}
		if sourceCount == 0 {
			continue
		}
		var targetCount int64
		if err := target.Raw("SELECT COUNT(1) FROM " + quoteIdent(table)).Scan(&targetCount).Error; err != nil {
			return copied, fmt.Errorf("count target table %s: %w", table, err)
		}
		if targetCount > 0 {
			continue
		}

		modelType := reflect.TypeOf(m)
		if modelType.Kind() != reflect.Ptr {
			return copied, fmt.Errorf("model %T is not a pointer", m)
		}
		sliceType := reflect.SliceOf(modelType.Elem())
		slicePtr := reflect.New(sliceType)
		if err := src.Unscoped().Find(slicePtr.Interface()).Error; err != nil {
			return copied, fmt.Errorf("read sqlite table %s: %w", table, err)
		}
		if slicePtr.Elem().Len() == 0 {
			continue
		}
		if err := target.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(slicePtr.Interface(), batchSize).Error; err != nil {
			return copied, fmt.Errorf("copy sqlite table %s: %w", table, err)
		}
		copied += int64(slicePtr.Elem().Len())
	}
	return copied, nil
}

func sqliteTableExists(db *gorm.DB, table string) (bool, error) {
	var count int64
	if err := db.Raw(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count).Error; err != nil {
		return false, fmt.Errorf("inspect sqlite table %s: %w", table, err)
	}
	return count > 0, nil
}

func modelTableName(db *gorm.DB, m any) (string, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(m); err != nil {
		return "", err
	}
	return stmt.Schema.Table, nil
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func installSQLiteWriteGate(db *gorm.DB) {
	if db == nil {
		return
	}
	const lockedKey = "mediastation:sqlite_write_locked"
	gate := newSQLiteWriteGate()
	lock := func(tx *gorm.DB) {
		ctx := context.Background()
		if tx.Statement != nil && tx.Statement.Context != nil {
			ctx = tx.Statement.Context
		}
		if err := gate.Lock(ctx); err != nil {
			_ = tx.AddError(err)
			return
		}
		tx.InstanceSet(lockedKey, struct{}{})
	}
	unlock := func(tx *gorm.DB) {
		if _, ok := tx.InstanceGet(lockedKey); ok {
			gate.Unlock()
		}
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

// sqliteWriteGate 串行化进程内的 SQLite 写操作，避免多连接写竞争触发
// SQLITE_BUSY。Lock 尊重语句自身的 context：此前用 sync.Mutex 时，一条
// 长写语句（如 FTS 回填批次）会让登录等关键写操作无限期排队——客户端
// 早已超时断开，goroutine 还挂在互斥锁上。现在等待方可随 context 取消
// 及时失败，不再把整个进程的写路径拖死。
type sqliteWriteGate struct {
	ch chan struct{}
}

func newSQLiteWriteGate() *sqliteWriteGate {
	return &sqliteWriteGate{ch: make(chan struct{}, 1)}
}

func (g *sqliteWriteGate) Lock(ctx context.Context) error {
	select {
	case g.ch <- struct{}{}:
		return nil
	default:
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case g.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *sqliteWriteGate) Unlock() {
	select {
	case <-g.ch:
	default:
	}
}

func buildSQLiteDSN(cfg *config.Config) string {
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
	if isSQLite(db) {
		return ensureMediaSearchIndex(db)
	}
	return nil
}

func ensurePerformanceIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_library_created_active ON media(library_id, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_episode_active ON media(library_id, season_num, episode_num, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_series_active ON media(series_id, season_num, episode_num) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_user_media_active ON favorites(user_id, media_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_media_active ON playback_histories(user_id, media_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_resume_active ON playback_histories(user_id, completed, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_play_profiles_user_created_active ON play_profiles(user_id, created_at DESC) WHERE deleted_at IS NULL`,
	}
	if isSQLite(db) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_media_title_active ON media(title COLLATE NOCASE) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_media_original_name_active ON media(original_name COLLATE NOCASE) WHERE deleted_at IS NULL`,
		)
	} else {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_media_title_active ON media(title) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_media_original_name_active ON media(original_name) WHERE deleted_at IS NULL`,
		)
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func isSQLite(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "sqlite"
}

// mediaSearchIndexSchemaVersion 标识 FTS 索引的物理布局版本。
// v2：FTS 行的 rowid 与 media.rowid 对齐，并由触发器实时维护。
const mediaSearchIndexSchemaVersion = 2

func ensureMediaSearchIndex(db *gorm.DB) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS media_search_meta (id INTEGER PRIMARY KEY CHECK (id = 1), version INTEGER NOT NULL)`).Error; err != nil {
		return nil
	}
	var version int
	_ = db.Raw(`SELECT version FROM media_search_meta WHERE id = 1`).Scan(&version).Error
	if version != mediaSearchIndexSchemaVersion {
		// 旧版（v1）FTS 表按 UNINDEXED 的 media_id 寻址。FTS5 的普通列
		// 不支持索引查找，按 media_id 的 DELETE / NOT EXISTS 都是整表
		// 扫描：十几万行的库每次启动回填要做上百亿次行访问，纯 Go
		// sqlite 直接把 CPU 钉满数小时，并隔着全局写锁拖死登录。
		// v2 起 FTS 行的 rowid 与 media.rowid 对齐，所有寻址走 rowid
		// 点查，索引一致性交给下方触发器维护。
		for _, stmt := range []string{
			`DROP TRIGGER IF EXISTS media_search_fts_ai`,
			`DROP TRIGGER IF EXISTS media_search_fts_au`,
			`DROP TRIGGER IF EXISTS media_search_fts_ad`,
			`DROP TABLE IF EXISTS media_search_fts`,
		} {
			_ = db.Exec(stmt).Error
		}
	}
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(media_id UNINDEXED, title, original_name, path, genres, tokenize='trigram')`).Error; err != nil {
		if fallbackErr := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(media_id UNINDEXED, title, original_name, path, genres, tokenize='unicode61')`).Error; fallbackErr != nil {
			// FTS is an acceleration path. Some embedded SQLite builds may omit
			// FTS5; keep startup working and let repository queries fall back to
			// LIKE-based Chinese fuzzy search.
			return nil
		}
	}
	// 触发器让 FTS 与 media 行保持同步（新增/标题刮削改写/软删/恢复/
	// 硬删全覆盖），应用层不再需要按 media_id 手工刷新索引——也顺带
	// 修复了刮削直写 Updates() 后新标题搜不到的问题。
	for _, stmt := range []string{
		`CREATE TRIGGER IF NOT EXISTS media_search_fts_ai AFTER INSERT ON media WHEN new.deleted_at IS NULL BEGIN
			DELETE FROM media_search_fts WHERE rowid = new.rowid;
			INSERT INTO media_search_fts(rowid, media_id, title, original_name, path, genres)
			VALUES (new.rowid, new.id, COALESCE(new.title, ''), COALESCE(new.original_name, ''), COALESCE(new.path, ''), COALESCE(new.genres, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS media_search_fts_au AFTER UPDATE OF title, original_name, path, genres, deleted_at ON media BEGIN
			DELETE FROM media_search_fts WHERE rowid = old.rowid;
			INSERT INTO media_search_fts(rowid, media_id, title, original_name, path, genres)
			SELECT new.rowid, new.id, COALESCE(new.title, ''), COALESCE(new.original_name, ''), COALESCE(new.path, ''), COALESCE(new.genres, '')
			WHERE new.deleted_at IS NULL;
		END`,
		`CREATE TRIGGER IF NOT EXISTS media_search_fts_ad AFTER DELETE ON media BEGIN
			DELETE FROM media_search_fts WHERE rowid = old.rowid;
		END`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	if version != mediaSearchIndexSchemaVersion {
		if err := db.Exec(`INSERT INTO media_search_meta(id, version) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET version = excluded.version`, mediaSearchIndexSchemaVersion).Error; err != nil {
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
             ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY created_at ASC, id ASC) AS rn
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

// zapStdLogger adapts a *zap.Logger to GORM's tiny logger interface.
type zapStdLogger struct{ log *zap.Logger }

func (z zapStdLogger) Printf(format string, args ...interface{}) {
	z.log.Sugar().Infof(format, args...)
}
