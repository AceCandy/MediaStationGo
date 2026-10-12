package database

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestArtworkStorageKeysIsolateSources(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ArtworkAsset{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		if db.Migrator().HasIndex(&model.ArtworkAsset{}, "idx_artwork_assets_sha256") {
			t.Fatal("global SHA index exists")
		}
	}
	for _, source := range []string{"tmdb", "douban"} {
		asset := model.ArtworkAsset{SHA256: "same-content", StorageKey: "catalogs/" + source + "/artwork/aa/bb/cc/image.png", MimeType: "image/png"}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatalf("source isolation: %v", err)
		}
	}
	duplicate := model.ArtworkAsset{SHA256: "same-content", StorageKey: "catalogs/douban/artwork/aa/bb/cc/image.png", MimeType: "image/png"}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate storage key accepted")
	}

}

func TestArtworkStorageSchemaHasNFOReferenceIndexes(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	tables := []string{"nfo_items", "nfo_media_bindings"}
	columns := []string{"poster_asset_id", "backdrop_asset_id"}
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
