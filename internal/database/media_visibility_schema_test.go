package database

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestAutoMigrateRemovesMediaNSFW(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"metadata_items", "nfo_items", "nfo_media_bindings"} {
		if err := db.Exec("ALTER TABLE " + table + " ADD COLUMN nsfw boolean").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE INDEX test_" + table + "_nsfw ON " + table + "(nsfw)").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO settings(key,value) VALUES ('adult.library_ids','[\"adult-library\"]')").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"metadata_items", "nfo_items", "nfo_media_bindings"} {
			if db.Migrator().HasColumn(table, "nsfw") || db.Migrator().HasIndex(table, "test_"+table+"_nsfw") {
				t.Fatalf("retired column/index remains on %s", table)
			}
		}
		var value string
		if err := db.Table("settings").Select("value").Where("key = 'adult.library_ids'").Scan(&value).Error; err != nil || value != `["adult-library"]` {
			t.Fatalf("library policy changed: %q %v", value, err)
		}
	}
}
