package database

import (
	"testing"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestAutoMigrateRetiresScrapeSettingExactlyAndIdempotently(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"scrape.auto_on_scan", "scrape.auto_on_scan.enabled", "scrape.providers", "metadata.artwork_backfill_enabled", "metadata.artwork_backfill_interval_seconds", "internal.metadata_artwork_integrity_cursor", "internal.tmdb_artwork_local_repair_cursor", "internal.tmdb_artwork_missing_recheck_cursor"} {
		if err := db.Create(&model.Setting{Key: key, Value: "legacy"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated retirement: %v", err)
	}
	assertUnscopedCount(t, db, &model.Setting{}, "key = ?", "scrape.auto_on_scan", 0)
	assertUnscopedCount(t, db, &model.Setting{}, "key IN ?", []string{"metadata.artwork_backfill_enabled", "metadata.artwork_backfill_interval_seconds", "internal.metadata_artwork_integrity_cursor", "internal.tmdb_artwork_local_repair_cursor", "internal.tmdb_artwork_missing_recheck_cursor"}, 0)
	assertUnscopedCount(t, db, &model.Setting{}, "key = ?", "scrape.auto_on_scan.enabled", 1)
	assertUnscopedCount(t, db, &model.Setting{}, "key = ?", "scrape.providers", 1)
}
