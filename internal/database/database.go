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
	"sort"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

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

// MigrateSQLiteToCurrentIfNeeded copies an existing SQLite database into
// PostgreSQL. Redis is not migrated because it is a rebuildable cache, not a
// source of truth.
const sqliteMigrationCompleteSettingKey = "database.sqlite_migration_complete"

func MigrateSQLiteToCurrentIfNeeded(cfg *config.Config, target *gorm.DB, log *zap.Logger) error {
	if cfg == nil || target == nil || target.Dialector == nil || target.Dialector.Name() != "postgres" {
		return nil
	}
	sqlitePath, err := sqliteMigrationSourcePath(cfg, log)
	if err != nil {
		return err
	}
	if sqlitePath == "" {
		return nil
	}
	if complete, err := sqliteMigrationMarkedComplete(target); err != nil {
		return err
	} else if complete {
		if log != nil {
			log.Info("skip sqlite to postgres migration: already completed")
		}
		return nil
	}

	src, err := openSQLiteMigrationSource(cfg, sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite migration source: %w", err)
	}
	sqlDB, err := src.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	started := time.Now()
	if err := resetBootstrapTargetBeforeSQLiteMigrationIfSafe(src, target, log); err != nil {
		return err
	}
	copied, err := copyModelTables(src, target, 500)
	if err != nil {
		return err
	}
	if err := markSQLiteMigrationComplete(target); err != nil {
		return err
	}
	if log != nil {
		log.Info("sqlite data migrated to postgres",
			zap.String("source", sqlitePath),
			zap.Int64("rows", copied),
			zap.Duration("duration", time.Since(started)))
	}
	return nil
}

func openSQLiteMigrationSource(cfg *config.Config, sqlitePath string) (*gorm.DB, error) {
	srcCfg := *cfg
	srcCfg.Database.Type = "sqlite"
	srcCfg.Database.DBPath = sqlitePath
	return gorm.Open(sqlite.Open(buildSQLiteDSN(&srcCfg)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
}

func sqliteMigrationSourcePath(cfg *config.Config, log *zap.Logger) (string, error) {
	configured := strings.TrimSpace(cfg.Database.DBPath)
	if configured != "" {
		exists, err := regularFileExists(configured)
		if err != nil {
			return "", fmt.Errorf("stat sqlite migration source: %w", err)
		}
		if exists {
			return configured, nil
		}
	}

	fallback := filepath.Join(strings.TrimSpace(cfg.App.DataDir), "mediastation.db")
	if fallback == "" || sameCleanPath(configured, fallback) {
		return "", nil
	}
	exists, err := regularFileExists(fallback)
	if err != nil {
		return "", fmt.Errorf("stat default sqlite migration source: %w", err)
	}
	if !exists {
		return "", nil
	}
	if log != nil && configured != "" {
		log.Warn("configured sqlite migration source not found; using data-dir default",
			zap.String("configured", configured),
			zap.String("fallback", fallback))
	}
	return fallback, nil
}

func regularFileExists(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func sameCleanPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func resetBootstrapTargetBeforeSQLiteMigrationIfSafe(src, target *gorm.DB, log *zap.Logger) error {
	hasRows, err := sqliteSourceHasMigratableRows(src)
	if err != nil {
		return err
	}
	if !hasRows {
		return nil
	}
	bootstrapOnly, err := targetLooksLikeBootstrapOnly(target)
	if err != nil || !bootstrapOnly {
		return err
	}
	for i := len(model.AllModels()) - 1; i >= 0; i-- {
		m := model.AllModels()[i]
		if !target.Migrator().HasTable(m) {
			continue
		}
		if err := target.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(m).Error; err != nil {
			return fmt.Errorf("clear bootstrap target table %T: %w", m, err)
		}
	}
	if log != nil {
		log.Warn("cleared bootstrap postgres rows before sqlite migration")
	}
	return nil
}

func sqliteSourceHasMigratableRows(src *gorm.DB) (bool, error) {
	for _, table := range []string{"users", "libraries", "media", "settings"} {
		exists, err := sqliteTableExists(src, table)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		var count int64
		if err := src.Raw("SELECT COUNT(1) FROM " + quoteIdent(table)).Scan(&count).Error; err != nil {
			return false, fmt.Errorf("count sqlite table %s: %w", table, err)
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func targetLooksLikeBootstrapOnly(target *gorm.DB) (bool, error) {
	for _, m := range []any{
		&model.Library{},
		&model.Series{},
		&model.Media{},
		&model.PlaybackHistory{},
		&model.Favorite{},
		&model.Playlist{},
		&model.PlaylistItem{},
		&model.DownloadTask{},
		&model.Subscription{},
	} {
		if !target.Migrator().HasTable(m) {
			continue
		}
		var count int64
		if err := target.Unscoped().Model(m).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return false, nil
		}
	}

	var userCount int64
	if !target.Migrator().HasTable(&model.User{}) {
		return true, nil
	}
	if err := target.Model(&model.User{}).Count(&userCount).Error; err != nil {
		return false, err
	}
	if userCount == 0 {
		return true, nil
	}
	if userCount != 1 {
		return false, nil
	}
	var user model.User
	if err := target.Unscoped().Where("username = ?", "admin").First(&user).Error; err != nil {
		return false, nil
	}
	return user.Role == "admin", nil
}

func sqliteMigrationMarkedComplete(db *gorm.DB) (bool, error) {
	var value string
	err := db.Raw("SELECT value FROM "+quoteIdent("settings")+" WHERE "+quoteIdent("key")+" = ?", sqliteMigrationCompleteSettingKey).Scan(&value).Error
	if err != nil {
		return false, fmt.Errorf("check sqlite migration marker: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(value), "true"), nil
}

func markSQLiteMigrationComplete(db *gorm.DB) error {
	now := time.Now()
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&model.Setting{
		Key:       sqliteMigrationCompleteSettingKey,
		Value:     "true",
		UpdatedAt: now,
	}).Error; err != nil {
		return fmt.Errorf("mark sqlite migration complete: %w", err)
	}
	return nil
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
		primaryColumns, err := modelPrimaryColumns(src, m)
		if err != nil {
			return copied, fmt.Errorf("inspect model %T primary keys: %w", m, err)
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
		modelType := reflect.TypeOf(m)
		if modelType.Kind() != reflect.Ptr {
			return copied, fmt.Errorf("model %T is not a pointer", m)
		}
		sliceType := reflect.SliceOf(modelType.Elem())
		slicePtr := reflect.New(sliceType)
		if err := src.Unscoped().Find(slicePtr.Interface()).Error; err != nil {
			return copied, fmt.Errorf("read sqlite table %s: %w", table, err)
		}
		filtered := slicePtr.Elem()
		if targetCount > 0 {
			primaryKeySet, err := targetPrimaryKeySet(target, table, primaryColumns)
			if err != nil {
				return copied, err
			}
			filtered = filterRowsMissingInTarget(target, table, primaryColumns, filtered, primaryKeySet)
		}
		if filtered.Len() == 0 {
			continue
		}
		filteredPtr := reflect.New(filtered.Type())
		filteredPtr.Elem().Set(filtered)
		if err := target.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(filteredPtr.Interface(), batchSize).Error; err != nil {
			return copied, fmt.Errorf("copy sqlite table %s: %w", table, err)
		}
		copied += int64(filtered.Len())
	}
	return copied, nil
}

func modelPrimaryColumns(db *gorm.DB, m any) ([]string, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(m); err != nil {
		return nil, err
	}
	var cols []string
	for _, field := range stmt.Schema.PrimaryFields {
		cols = append(cols, field.DBName)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no primary key columns")
	}
	return cols, nil
}

func targetPrimaryKeySet(target *gorm.DB, table string, primaryColumns []string) (map[string]struct{}, error) {
	if len(primaryColumns) != 1 {
		return nil, nil
	}
	var values []string
	if err := target.Raw("SELECT " + quoteIdent(primaryColumns[0]) + " FROM " + quoteIdent(table)).Scan(&values).Error; err != nil {
		return nil, fmt.Errorf("read target primary keys for table %s: %w", table, err)
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set, nil
}

func filterRowsMissingInTarget(target *gorm.DB, table string, primaryColumns []string, rows reflect.Value, primaryKeySet map[string]struct{}) reflect.Value {
	if rows.Kind() != reflect.Slice || rows.Len() == 0 || len(primaryColumns) == 0 {
		return rows
	}
	out := reflect.MakeSlice(rows.Type(), 0, rows.Len())
	for i := 0; i < rows.Len(); i++ {
		row := rows.Index(i)
		keys, ok := rowPrimaryKeys(row, primaryColumns)
		if !ok {
			out = reflect.Append(out, row)
			continue
		}
		if primaryKeySet != nil {
			if _, exists := primaryKeySet[fmt.Sprint(keys[primaryColumns[0]])]; !exists {
				out = reflect.Append(out, row)
			}
			continue
		}
		if !targetHasPrimaryKey(target, table, keys) {
			out = reflect.Append(out, row)
		}
	}
	return out
}

func rowPrimaryKeys(row reflect.Value, primaryColumns []string) (map[string]any, bool) {
	if row.Kind() == reflect.Pointer {
		if row.IsNil() {
			return nil, false
		}
		row = row.Elem()
	}
	if row.Kind() != reflect.Struct {
		return nil, false
	}
	keys := make(map[string]any, len(primaryColumns))
	for _, column := range primaryColumns {
		value, ok := fieldByDBName(row, column)
		if !ok || value.IsZero() {
			return nil, false
		}
		keys[column] = value.Interface()
	}
	return keys, true
}

func fieldByDBName(row reflect.Value, column string) (reflect.Value, bool) {
	rowType := row.Type()
	for i := 0; i < row.NumField(); i++ {
		fieldType := rowType.Field(i)
		field := row.Field(i)
		if fieldType.Anonymous {
			if value, ok := fieldByDBName(field, column); ok {
				return value, true
			}
		}
		if columnNameForStructField(fieldType) == column {
			if field.Kind() == reflect.Pointer && field.IsNil() {
				return reflect.Value{}, false
			}
			return field, field.CanInterface()
		}
	}
	return reflect.Value{}, false
}

func columnNameForStructField(field reflect.StructField) string {
	if field.PkgPath != "" && !field.Anonymous {
		return ""
	}
	tag := field.Tag.Get("gorm")
	settings := schema.ParseTagSetting(tag, ";")
	if column := settings["COLUMN"]; column != "" {
		return column
	}
	return schema.NamingStrategy{}.ColumnName("", field.Name)
}

func targetHasPrimaryKey(target *gorm.DB, table string, keys map[string]any) bool {
	where := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys))
	for _, column := range sortedMapKeys(keys) {
		where = append(where, quoteIdent(column)+" = ?")
		args = append(args, keys[column])
	}
	var count int64
	err := target.Raw("SELECT COUNT(1) FROM "+quoteIdent(table)+" WHERE "+strings.Join(where, " AND "), args...).Scan(&count).Error
	return err == nil && count > 0
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
	if err := ensurePostgresColumnCompatibility(db); err != nil {
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

func ensurePostgresColumnCompatibility(db *gorm.DB) error {
	if !isPostgres(db) {
		return nil
	}
	statements := []string{
		`ALTER TABLE media ALTER COLUMN container TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN genres TYPE text`,
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

func isPostgres(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres"
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
