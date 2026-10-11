package database

import (
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"testing"
)

func TestTMDbCatalogSchemaHierarchy(t *testing.T) {
	db, e := testdb.OpenPostgres(t, &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = AutoMigrate(db); e != nil {
		t.Fatal(e)
	}
	root := model.TMDbSubject{Kind: "series", TMDbID: 10, Fields: "{}"}
	if e = db.Create(&root).Error; e != nil {
		t.Fatal(e)
	}
	invalid := model.TMDbSubject{Kind: "season", TMDbID: 11, ParentID: &root.ID, Fields: "{}"}
	if e = db.Create(&invalid).Error; e == nil {
		t.Fatal("season accepted null coordinate")
	}
	var count int64
	if e = db.Raw("SELECT count(*) FROM pg_trigger WHERE tgrelid='tmdb_payloads'::regclass AND tgname='tmdb_catalog_source_changed'").Scan(&count).Error; e != nil || count != 1 {
		t.Fatalf("source notification not installed %d %v", count, e)
	}
}
