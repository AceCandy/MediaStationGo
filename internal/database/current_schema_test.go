package database

import (
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestCurrentSchemaPreservesDataAndConstraints(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := model.APIConfig{Provider: "douban", APIKey: strings.Repeat("test-ciphertext", 100), Enabled: true}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var got model.APIConfig
	if err := db.First(&got, "id = ?", cfg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.APIKey != cfg.APIKey {
		t.Fatal("schema maintenance changed the saved credential")
	}
	var columnType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='api_configs' AND column_name='api_key'`).Scan(&columnType).Error; err != nil {
		t.Fatal(err)
	}
	if columnType != "text" {
		t.Fatal("credential column must be text")
	}
	for _, table := range []string{"series", "metadata_provider_snapshots", "tmdb_migration_failures", "hongguo_groups", "hongguo_group_members"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("retired table created: %s", table)
		}
	}
	if db.Migrator().HasColumn("api_configs", "last_tested_at") || db.Migrator().HasColumn("api_configs", "test_result") {
		t.Fatal("retired configuration fields created")
	}
	first := model.TelegramBinding{TelegramUserID: 1, UserID: "user-1"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := model.TelegramBinding{TelegramUserID: 2, UserID: first.UserID}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate active user binding accepted")
	}
	if err := db.Delete(&first).Error; err != nil {
		t.Fatal(err)
	}
	duplicate.ID = ""
	if err := db.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	sourceID := "source-1"
	if err := db.Create(&model.HongGuoArtwork{SourceID: &sourceID, SourceURL: "https://example.test/poster"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HongGuoArtwork{SourceURL: "https://example.test/orphan"}).Error; err == nil {
		t.Fatal("unowned artwork accepted")
	}
}
