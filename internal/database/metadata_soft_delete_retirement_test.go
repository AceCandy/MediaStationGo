package database

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestRetireMetadataSoftDeletesRejectsReferencesAndDropsColumns(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"media", "metadata_items", "metadata_identifiers", "metadata_provider_snapshots",
		"catalog_hydration_jobs", "metadata_artworks", "artwork_assets", "metadata_credits",
	} {
		if err := db.Exec("ALTER TABLE " + table + " ADD COLUMN deleted_at timestamptz").Error; err != nil {
			t.Fatal(err)
		}
	}

	active := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Active", Source: "tmdb"}
	retired := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Retired", Source: "tmdb"}
	if err := db.Create(&[]*model.MetadataItem{&active, &retired}).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "asset", StorageKey: "sha256/asset.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: active.ID, AssetID: asset.ID, ArtworkType: model.ArtworkTypePoster}
	if err := db.Create(&selection).Error; err != nil {
		t.Fatal(err)
	}
	person := model.Person{Name: "Actor", OriginalName: "Actor", NormalizedName: "actor", Source: "tmdb"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	credits := []model.MetadataCredit{
		{MetadataID: active.ID, PersonID: person.ID, Type: model.CreditTypeActor, OriginalRole: "Hero", Role: "英雄"},
		{MetadataID: active.ID, PersonID: person.ID, Type: model.CreditTypeActor, OriginalRole: "Retired", Role: "旧角色"},
	}
	if err := db.Create(&credits).Error; err != nil {
		t.Fatal(err)
	}
	job := model.CatalogHydrationJob{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "99", MetadataID: &retired.ID, Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for table, id := range map[string]string{
		"metadata_items":   retired.ID,
		"artwork_assets":   asset.ID,
		"metadata_credits": credits[1].ID,
	} {
		if err := db.Exec("UPDATE "+table+" SET deleted_at = ? WHERE id = ?", now, id).Error; err != nil {
			t.Fatal(err)
		}
	}
	media := model.Media{Title: "Referenced", Path: "/referenced.mkv", MetadataID: retired.ID}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := retireMetadataSoftDeletes(db); err == nil {
		t.Fatal("expected referenced retired metadata to reject migration")
	}
	if !db.Migrator().HasColumn(&model.MetadataItem{}, "deleted_at") {
		t.Fatal("failed migration did not roll back column removal")
	}
	if err := db.Unscoped().Delete(&media).Error; err != nil {
		t.Fatal(err)
	}
	if err := retireMetadataSoftDeletes(db); err != nil {
		t.Fatal(err)
	}
	if err := retireMediaAndCreditSoftDeletes(db); err != nil {
		t.Fatal(err)
	}
	if err := retireMetadataSoftDeletes(db); err != nil {
		t.Fatalf("repeated migration: %v", err)
	}
	if err := retireMediaAndCreditSoftDeletes(db); err != nil {
		t.Fatalf("repeated media/credit migration: %v", err)
	}
	for _, table := range []string{
		"media", "metadata_items", "metadata_identifiers", "metadata_provider_snapshots",
		"catalog_hydration_jobs", "metadata_artworks", "artwork_assets", "metadata_credits",
	} {
		if db.Migrator().HasColumn(table, "deleted_at") {
			t.Fatalf("%s.deleted_at still exists", table)
		}
	}
	assertUnscopedCount(t, db, &model.MetadataItem{}, "id = ?", active.ID, 1)
	assertUnscopedCount(t, db, &model.MetadataItem{}, "id = ?", retired.ID, 0)
	assertUnscopedCount(t, db, &model.ArtworkAsset{}, "id = ?", asset.ID, 1)
	assertUnscopedCount(t, db, &model.MetadataArtwork{}, "id = ?", selection.ID, 1)
	assertUnscopedCount(t, db, &model.MetadataCredit{}, "id = ?", credits[0].ID, 1)
	assertUnscopedCount(t, db, &model.MetadataCredit{}, "id = ?", credits[1].ID, 0)
	var retainedJob model.CatalogHydrationJob
	if err := db.First(&retainedJob, "id = ?", job.ID).Error; err != nil || retainedJob.MetadataID != nil {
		t.Fatalf("catalog job after retired metadata cleanup = %#v, %v", retainedJob, err)
	}
}
