package database

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoRetireManualGroups(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	w := model.HongGuoWork{SourceID: "91001", Kind: "series", Title: "保留作品", Tags: "[]", RefreshedAt: time.Now()}
	if err = db.Create(&w).Error; err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"CREATE TABLE hongguo_groups (id text PRIMARY KEY)",
		"CREATE TABLE hongguo_group_members (group_id text REFERENCES hongguo_groups(id), work_id text)",
		"INSERT INTO hongguo_groups VALUES ('old-group')",
	} {
		if err = db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Exec("INSERT INTO hongguo_group_members VALUES ('old-group', ?)", w.ID).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	if db.Migrator().HasTable("hongguo_groups") || db.Migrator().HasTable("hongguo_group_members") {
		t.Fatal("retired tables remain")
	}
	var saved model.HongGuoWork
	if err = db.First(&saved, "id = ?", w.ID).Error; err != nil || saved.SourceID != w.SourceID || saved.AlbumCheckedAt != nil {
		t.Fatalf("work changed: %+v %v", saved, err)
	}
}
