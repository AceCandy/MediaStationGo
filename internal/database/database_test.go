package database

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestOpenRequiresConfig(t *testing.T) {
	db, err := Open(nil, nil)
	if err == nil {
		t.Fatal("expected nil config to return an error")
	}
	if db != nil {
		t.Fatal("db should be nil when config is missing")
	}
	if !strings.Contains(err.Error(), "database config") {
		t.Fatalf("error = %v, want database config message", err)
	}
}

func TestDatabaseDialectorRejectsNonPostgres(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Type = "mysql"
	cfg.Database.DSN = "ignored"
	if _, err := databaseDialector(cfg, false); err == nil || !strings.Contains(err.Error(), "supported: postgres") {
		t.Fatalf("error = %v, want postgres-only error", err)
	}
}

func TestDatabaseDialectorRequiresDSN(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Type = "postgres"
	if _, err := databaseDialector(cfg, false); err == nil || !strings.Contains(err.Error(), "database.dsn") {
		t.Fatalf("error = %v, want database.dsn error", err)
	}
}

func TestPostgresMigrationDialectorUsesSimpleProtocol(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Type = "postgres"
	cfg.Database.DSN = "postgres://example.invalid/mediastation"

	dialector, err := databaseDialector(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	postgresDialector, ok := dialector.(*postgres.Dialector)
	if !ok {
		t.Fatalf("dialector type = %T, want postgres", dialector)
	}
	if !postgresDialector.Config.PreferSimpleProtocol {
		t.Fatal("postgres migration dialector should use simple protocol")
	}

	runtimeDialector, err := databaseDialector(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeDialector.(*postgres.Dialector).Config.PreferSimpleProtocol {
		t.Fatal("postgres runtime dialector should retain prepared statements")
	}
}

func TestMigrationConnectionClosesBeforePreparedRuntimeQuery(t *testing.T) {
	isolationDB, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := isolationDB.Raw(`SELECT current_schema()`).Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	isolationSQLDB, err := isolationDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := isolationSQLDB.Close(); err != nil {
		t.Fatal(err)
	}

	dsn := strings.TrimSpace(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN"))
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid PostgreSQL test DSN")
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}

	cfg := &config.Config{}
	cfg.Database.Type = "postgres"
	cfg.Database.DSN = dsn
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1

	migrationDB, err := OpenForMigration(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	migrationSQLDB, err := migrationDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrationSQLDB.Close() })
	if _, ok := migrationDB.ConnPool.(*gorm.PreparedStmtDB); ok {
		t.Fatal("migration connection should not prepare statements")
	}
	if err := AutoMigrate(migrationDB); err != nil {
		t.Fatal(err)
	}
	want := model.Setting{Key: "runtime.prepared_query_check", Value: "ready"}
	if err := migrationDB.Create(&want).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrationSQLDB.Close(); err != nil {
		t.Fatal(err)
	}

	runtimeDB, err := Open(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSQLDB, err := runtimeDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeSQLDB.Close() })
	prepared, ok := runtimeDB.ConnPool.(*gorm.PreparedStmtDB)
	if !ok {
		t.Fatal("runtime connection should prepare statements")
	}

	var got model.Setting
	if err := runtimeDB.First(&got, "key = ?", want.Key).Error; err != nil {
		t.Fatal(err)
	}
	if got.Value != want.Value {
		t.Fatalf("setting value = %q, want %q", got.Value, want.Value)
	}
	prepared.Mux.RLock()
	preparedCount := len(prepared.PreparedSQL)
	prepared.Mux.RUnlock()
	if preparedCount == 0 {
		t.Fatal("first runtime query was not prepared")
	}
}

func TestRemoveUnusedLegacyColumns(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Person{}, &model.UserDevice{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE people ADD COLUMN profile_image_source text`,
		`ALTER TABLE user_devices ADD COLUMN warnings integer`,
		`ALTER TABLE media ADD COLUMN duration_sec integer`,
		`ALTER TABLE media ADD COLUMN size_bytes bigint`,
		`ALTER TABLE media ADD COLUMN container varchar(128)`,
		`ALTER TABLE media ADD COLUMN width integer`,
		`ALTER TABLE media ADD COLUMN height integer`,
		`ALTER TABLE media ADD COLUMN video_codec varchar(32)`,
		`ALTER TABLE media ADD COLUMN audio_codec varchar(32)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := removeUnusedLegacyColumns(db); err != nil {
		t.Fatal(err)
	}
	for _, column := range []struct {
		table string
		name  string
	}{
		{table: "people", name: "profile_image_source"},
		{table: "user_devices", name: "warnings"},
		{table: "media", name: "duration_sec"},
		{table: "media", name: "size_bytes"},
		{table: "media", name: "container"},
		{table: "media", name: "width"},
		{table: "media", name: "height"},
		{table: "media", name: "video_codec"},
		{table: "media", name: "audio_codec"},
	} {
		var count int
		if err := db.Raw(`
SELECT COUNT(1)
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, column.table, column.name).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy column %s.%s still exists", column.table, column.name)
		}
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	if err := removeUnusedLegacyColumns(db); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"duration_sec", "size_bytes", "container", "width", "height", "video_codec", "audio_codec"} {
		if db.Migrator().HasColumn(&model.Media{}, column) {
			t.Fatalf("AutoMigrate restored retired media column %s", column)
		}
	}
	for _, column := range []string{"scan_file_size_bytes", "scan_file_mtime_ns"} {
		if !db.Migrator().HasColumn(&model.Media{}, column) {
			t.Fatalf("required media scan column %s is missing", column)
		}
	}
}

func TestEnforceTelegramBindingOneToOneCleansDuplicatesAndAddsIndex(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TelegramBinding{}); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().Add(-time.Hour)
	rows := []model.TelegramBinding{
		{TelegramUserID: 10001, ChatID: 10001, UserID: "user-1"},
		{TelegramUserID: 10002, ChatID: 10002, UserID: "user-1"},
	}
	for i := range rows {
		rows[i].CreatedAt = createdAt.Add(time.Duration(i) * time.Minute)
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := enforceTelegramBindingOneToOne(db); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := db.Model(&model.TelegramBinding{}).Where("user_id = ?", "user-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active bindings for user-1 = %d, want 1", count)
	}
	var kept model.TelegramBinding
	if err := db.First(&kept, "user_id = ?", "user-1").Error; err != nil {
		t.Fatal(err)
	}
	if kept.TelegramUserID != 10002 {
		t.Fatalf("kept telegram binding = %d, want newest 10002", kept.TelegramUserID)
	}
	if err := db.Create(&model.TelegramBinding{TelegramUserID: 10003, ChatID: 10003, UserID: "user-1"}).Error; err == nil {
		t.Fatal("expected unique index to reject another active binding for the same user")
	}
}

func TestEnsurePerformanceIndexesCreatesHotPathIndexes(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataCredit{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE INDEX idx_metadata_credits_pending_translation ON metadata_credits(metadata_id, id)
		WHERE original_role <> '' AND role = original_role AND original_role !~ '[一-鿿]'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	for _, name := range []string{
		"idx_people_pending_translation",
		"idx_metadata_credits_type_pending_translation",
		"idx_media_library_created_active",
		"idx_media_library_episode_active",
		"idx_media_metadata_active",
		"idx_media_scrape_pending_pick",
		"idx_media_scrape_group",
		"idx_media_scrape_running",
		"idx_media_recent_metadata",
		"idx_metadata_parent_episode_active",
		"idx_favorites_user_media_active",
		"idx_playback_histories_user_media_active",
		"idx_play_profiles_user_created_active",
	} {
		var count int
		if err := db.Raw(`SELECT COUNT(1) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", name, count)
		}
	}
	if db.Migrator().HasIndex(&model.MetadataCredit{}, "idx_metadata_credits_pending_translation") {
		t.Fatal("obsolete pending translation index remains")
	}
	person := model.Person{Name: "演员", OriginalName: "演员"}
	work := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "电影"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO metadata_credits (id, metadata_id, person_id, type, original_role, role)
		SELECT 'credit-' || lpad(i::text, 6, '0'), ?, ?,
		CASE WHEN i <= 50 THEN 'Actor' WHEN i <= 100 THEN 'GuestStar' WHEN i <= 15000 THEN 'Director' ELSE 'Writer' END,
		'Role ' || i, 'Role ' || i
		FROM generate_series(1, 30000) AS i`, work.ID, person.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE metadata_credits").Error; err != nil {
		t.Fatal(err)
	}
	// 通用预编译计划不知道角色类型的绑定值，也必须能使用部分索引。
	if err := db.Exec("SET plan_cache_mode = force_generic_plan").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`PREPARE pending_roles(text, text) AS
		SELECT id, metadata_id, original_role FROM metadata_credits WHERE type IN ($1, $2)
		AND original_role <> '' AND role = original_role AND original_role !~ '[一-鿿]'
		AND (metadata_id, id) > ('', '') ORDER BY metadata_id, id LIMIT 100`).Error; err != nil {
		t.Fatal(err)
	}
	var plan []string
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) EXECUTE pending_roles('Actor', 'GuestStar')").Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_metadata_credits_type_pending_translation") || strings.Contains(joined, "Seq Scan on metadata_credits") || !strings.Contains(joined, "Index Cond:") || strings.Contains(joined, "Rows Removed by Filter:") {
		t.Fatalf("pending role query did not use partial index:\n%s", joined)
	}
	var candidates []model.MetadataCredit
	if err := db.Raw("EXECUTE pending_roles('Actor', 'GuestStar')").Scan(&candidates).Error; err != nil || len(candidates) != 100 {
		t.Fatalf("candidate count = %d, err = %v", len(candidates), err)
	}
	t.Log(joined)
	if err := db.Exec(`INSERT INTO people (id, normalized_name, source, name, original_name)
		SELECT 'person-' || lpad(i::text, 6, '0'),
		'person-' || i, 'local',
		CASE WHEN i <= 112 THEN 'Name ' || i ELSE '中文' END,
		CASE WHEN i <= 112 THEN 'Name ' || i ELSE '中文' END
		FROM generate_series(1, 30000) AS i`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE people").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`PREPARE pending_people(int) AS SELECT id, original_name FROM people
		WHERE deleted_at IS NULL AND original_name <> '' AND name = original_name
		AND original_name !~ '[一-鿿]' ORDER BY id LIMIT $1`).Error; err != nil {
		t.Fatal(err)
	}
	plan = nil
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) EXECUTE pending_people(1000)").Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_people_pending_translation") || strings.Contains(joined, "Seq Scan on people") {
		t.Fatalf("pending people query did not use partial index:\n%s", joined)
	}
	var people []model.Person
	if err := db.Raw("EXECUTE pending_people(1000)").Scan(&people).Error; err != nil || len(people) != 112 {
		t.Fatalf("people count = %d, err = %v", len(people), err)
	}
	t.Log(joined)
}
