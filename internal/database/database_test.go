package database

import (
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

func TestRemoveUnusedLegacyColumns(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Person{}, &model.UserDevice{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE people ADD COLUMN profile_image_source text`,
		`ALTER TABLE user_devices ADD COLUMN warnings integer`,
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
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"idx_media_library_created_active",
		"idx_media_library_episode_active",
		"idx_media_metadata_active",
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
}
