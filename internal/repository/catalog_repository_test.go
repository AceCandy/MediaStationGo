package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestNextCatalogAttemptAtHandlesEmptyAggregate(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CatalogHydrationJob{}); err != nil {
		t.Fatalf("migrate catalog hydration jobs: %v", err)
	}
	repos := New(db)

	assertNext := func(want *time.Time) {
		t.Helper()
		got, err := repos.Metadata.NextCatalogAttemptAt(t.Context())
		if err != nil {
			t.Fatalf("next catalog attempt: %v", err)
		}
		if want == nil {
			if got != nil {
				t.Fatalf("next catalog attempt = %v, want nil", got)
			}
			return
		}
		if got == nil || !got.Equal(*want) {
			t.Fatalf("next catalog attempt = %v, want %v", got, want)
		}
	}

	assertNext(nil)

	jobs := []model.CatalogHydrationJob{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "null-pending", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot},
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "null-retry", Status: model.CatalogJobStatusRetry, Stage: model.CatalogJobStageRoot},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create unscheduled catalog jobs: %v", err)
	}
	assertNext(nil)

	earlier := time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)
	completed := earlier.Add(-time.Hour)
	jobs = []model.CatalogHydrationJob{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "later", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot, NextAttemptAt: &later},
		{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "earlier", Status: model.CatalogJobStatusRetry, Stage: model.CatalogJobStageRoot, NextAttemptAt: &earlier},
		{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "completed", Status: model.CatalogJobStatusCompleted, Stage: model.CatalogJobStageRoot, NextAttemptAt: &completed},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create scheduled catalog jobs: %v", err)
	}
	assertNext(&earlier)
}
