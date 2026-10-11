package database

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestArtworkStorageMigrationRemovesGlobalHashUniqueness(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ArtworkAsset{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_artwork_assets_sha256 ON artwork_assets (sha256)").Error; err != nil {
		t.Fatal(err)
	}
	old := model.ArtworkAsset{SHA256: "same-content", StorageKey: "sha256/old.png", MimeType: "image/png"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		if db.Migrator().HasIndex(&model.ArtworkAsset{}, "idx_artwork_assets_sha256") {
			t.Fatal("global SHA index remains")
		}
	}
	for _, source := range []string{"tmdb", "douban"} {
		asset := model.ArtworkAsset{SHA256: old.SHA256, StorageKey: "catalogs/" + source + "/artwork/aa/bb/cc/image.png", MimeType: "image/png"}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatalf("source isolation: %v", err)
		}
	}
	duplicate := model.ArtworkAsset{SHA256: old.SHA256, StorageKey: "catalogs/douban/artwork/aa/bb/cc/image.png", MimeType: "image/png"}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate storage key accepted")
	}
	var got model.ArtworkAsset
	if err := db.First(&got, "id = ?", old.ID).Error; err != nil || got.StorageKey != old.StorageKey {
		t.Fatal("schema migration changed legacy data")
	}
}

func TestArtworkStorageMigrationAddsNFOReferenceIndexes(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	// 模拟旧库：两张表已存在，但图片引用字段尚无索引。
	tables := []string{"nfo_items", "nfo_media_bindings"}
	columns := []string{"poster_asset_id", "backdrop_asset_id"}
	for _, table := range tables {
		for _, column := range columns {
			name := "idx_" + table + "_" + column
			if db.Migrator().HasIndex(table, name) {
				if err := db.Migrator().DropIndex(table, name); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for range 2 {
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		for _, table := range tables {
			for _, column := range columns {
				name := "idx_" + table + "_" + column
				if !db.Migrator().HasIndex(table, name) {
					t.Fatalf("missing NFO artwork reference index: %s", name)
				}
			}
		}
	}
}
