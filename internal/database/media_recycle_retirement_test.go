package database

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestAutoMigratePurgesRetiredMediaRecycleRows(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	active := model.Media{Title: "Active", Path: "/active.mkv"}
	deleted := model.Media{Title: "Deleted", Path: "/deleted.mkv"}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	probe := model.MediaProbeMetadata{
		MediaID: deleted.ID, ProbeJSON: `{}`, SchemaVersion: 1, ProbedAt: time.Now(),
	}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated migration: %v", err)
	}
	assertUnscopedCount(t, db, &model.Media{}, "id = ?", deleted.ID, 0)
	assertUnscopedCount(t, db, &model.Media{}, "id = ?", active.ID, 1)
	assertUnscopedCount(t, db, &model.MediaProbeMetadata{}, "media_id = ?", deleted.ID, 0)
	if !db.Migrator().HasColumn(&model.Media{}, "scan_file_size_bytes") || !db.Migrator().HasColumn(&model.Media{}, "scan_file_mtime_ns") {
		t.Fatal("scan fingerprint columns were not migrated")
	}
	if !db.Migrator().HasColumn(&model.Media{}, "deleted_at") {
		t.Fatal("media deleted_at compatibility column should be retained")
	}
}
