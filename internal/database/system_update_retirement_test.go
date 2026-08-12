package database

import (
	"testing"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestRemoveSystemUpdateSettings(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	retiredKeys := []string{
		"system.update.image",
		"system.update.watchtower_image",
		"system.update.command",
		"system.update.compose_dir",
	}
	for _, key := range retiredKeys {
		if err := db.Create(&model.Setting{Key: key, Value: "legacy"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Setting{Key: "app.server_url", Value: "https://example.test"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated retirement: %v", err)
	}

	assertUnscopedCount(t, db, &model.Setting{}, "key IN ?", retiredKeys, 0)
	assertUnscopedCount(t, db, &model.Setting{}, "key = ?", "app.server_url", 1)
}
