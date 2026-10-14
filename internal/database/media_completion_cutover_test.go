package database

import (
	"os"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestMediaCompletionCutoverPreservesIdentity(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: model.MetadataKindMovie, Source: "tmdb"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	columns := []string{"catalog_metadata_hydrated_at", "people_hydrated_at", "catalog_artwork_hydrated_at", "catalog_artwork_due_at", "catalog_artwork_attempts", "catalog_hydrated_at"}
	for _, column := range columns {
		if err := db.Exec("ALTER TABLE metadata_items ADD COLUMN " + column + " integer").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("CREATE TABLE catalog_hydration_jobs(id text)").Error; err != nil {
		t.Fatal(err)
	}
	// 模拟旧视图确实依赖将删除的列，而非仅在空白 schema 上验证脚本。
	if err := db.Exec("DROP VIEW metadata_artwork_presentations, metadata_presentations").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE VIEW metadata_presentations AS SELECT * FROM metadata_items").Error; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../scripts/migrations/20261014_retire_media_completion.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var got model.MetadataItem
	if err := db.Table("metadata_presentations").First(&got, "id=?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Source != "tmdb" || got.Kind != item.Kind {
		t.Fatalf("identity changed: %+v", got)
	}
	for _, column := range columns {
		if db.Migrator().HasColumn(&model.MetadataItem{}, column) {
			t.Fatalf("retired column remains: %s", column)
		}
	}
	if db.Migrator().HasTable("catalog_hydration_jobs") {
		t.Fatal("retired queue remains")
	}
}
