package database

import (
	"testing"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestAutoMigrateRetiresPTSiteSchema(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE user_permissions ADD COLUMN can_manage_sites boolean NOT NULL DEFAULT false`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE sites (id varchar(36) PRIMARY KEY, cookie text, api_key text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO sites (id, cookie, api_key) VALUES ('legacy-site', 'secret-cookie', 'secret-key')`).Error; err != nil {
		t.Fatal(err)
	}

	permission := model.NewDefaultPermission("user-1")
	permission.CanPlayMedia = true
	if err := db.Create(permission).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated retirement: %v", err)
	}

	if db.Migrator().HasTable("sites") {
		t.Fatal("retired sites table still exists")
	}
	var retiredColumnCount int64
	if err := db.Raw(`
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'user_permissions'
  AND column_name = 'can_manage_sites'`).Scan(&retiredColumnCount).Error; err != nil {
		t.Fatal(err)
	}
	if retiredColumnCount != 0 {
		t.Fatal("retired can_manage_sites column still exists")
	}

	var persisted model.UserPermission
	if err := db.First(&persisted, "id = ?", permission.ID).Error; err != nil {
		t.Fatalf("unrelated permission row was removed: %v", err)
	}
	if !persisted.CanPlayMedia {
		t.Fatal("unrelated permission value changed")
	}

	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("sites") {
		t.Fatal("fresh schema migration recreated the retired sites table")
	}
}
