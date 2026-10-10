package database

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDoubanMigrationPreservesIdentityQualityAndTime(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	newer := old.Add(time.Hour)
	for _, row := range []struct {
		local, actual, payload string
		at                     time.Time
		degraded               bool
	}{
		{"movie", "", `{"title":"完整详情","rating":{"value":8.5}}`, old, false},
		{"series", "movie", `{"title":"降级资料","rating":{"value":7}}`, newer, true},
	} {
		item := model.MetadataItem{Kind: row.local, Title: row.local}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MetadataIdentifier{MetadataID: item.ID, Provider: "douban", EntityKind: row.local, DoubanEntityKind: row.actual, ExternalID: "100"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: item.ID, Provider: "douban", Payload: row.payload, Degraded: row.degraded, FetchedAt: row.at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := migrateDoubanSubjects(db); err != nil {
			t.Fatal(err)
		}
	}
	var subjects []model.DoubanSubject
	if err := db.Find(&subjects).Error; err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 1 || subjects[0].EntityKind != "movie" || subjects[0].Quality != model.DoubanQualityComplete || subjects[0].Rating != 8.5 || !subjects[0].FetchedAt.Equal(old) {
		t.Fatalf("migration: %#v", subjects)
	}
	fresh := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Model(&subjects[0]).Updates(map[string]any{"rating": 9.2, "fetched_at": fresh}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateDoubanSubjects(db); err != nil {
		t.Fatal(err)
	}
	var current model.DoubanSubject
	if err := db.First(&current).Error; err != nil || current.Rating != 9.2 || !current.FetchedAt.Equal(fresh) {
		t.Fatalf("existing subject overwritten: %#v %v", current, err)
	}
	var count int64
	if err := db.Model(&model.MetadataProviderSnapshot{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("old data changed: %d %v", count, err)
	}
}
