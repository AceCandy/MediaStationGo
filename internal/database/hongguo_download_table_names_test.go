package database

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoDownloadTableNamesFreshDatabase(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"hong_guo_download_works", "hongguo_download_works"}, {"hong_guo_downloads", "hongguo_downloads"}} {
		if db.Migrator().HasTable(pair[0]) || !db.Migrator().HasTable(pair[1]) {
			t.Fatalf("unexpected fresh table names: %v", pair)
		}
	}
}
