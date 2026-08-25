package service

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestArtworkBackfillInvalidatesMissingLocalSelectionBeforeQueueing(t *testing.T) {
	db := newServiceTestDB(t, &model.CatalogHydrationJob{}, &model.Setting{})
	repos := repository.New(db)
	now := time.Now().UTC()
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb", CatalogArtworkHydratedAt: &now}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	identifier := model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42"}
	if err := db.Create(&identifier).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "missing", StorageKey: "sha256/mi/ss/missing.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: metadata.ID, ArtworkType: model.ArtworkTypePoster, AssetID: asset.ID}
	if err := db.Create(&selection).Error; err != nil {
		t.Fatal(err)
	}
	store := NewArtworkStore(&config.Config{App: config.AppConfig{DataDir: t.TempDir()}}, repos.Artwork, nil)
	svc := &ScraperService{repo: repos, artwork: store}
	if err := svc.runMetadataArtworkBackfill(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if selected, err := repos.Artwork.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster); err != nil || selected != nil {
		t.Fatalf("selection after missing file scan = %#v, %v", selected, err)
	}
	updated, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil || updated == nil || updated.CatalogArtworkHydratedAt != nil {
		t.Fatalf("metadata after missing file scan = %#v, %v", updated, err)
	}
	var job model.CatalogHydrationJob
	if err := db.First(&job, "external_id = ?", "42").Error; err != nil || job.Status != model.CatalogJobStatusPending {
		t.Fatalf("catalog job after missing file scan = %#v, %v", job, err)
	}
}

func TestArtworkBackfillResumesLocalAssetScanFromCursor(t *testing.T) {
	db := newServiceTestDB(t, &model.Setting{})
	repos := repository.New(db)
	items := []model.MetadataItem{
		{Kind: model.MetadataKindMovie, Title: "Before cursor", Source: "manual"},
		{Kind: model.MetadataKindMovie, Title: "After cursor", Source: "manual"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	assets := []model.ArtworkAsset{
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000001"}, SHA256: "before-cursor", StorageKey: "sha256/be/fo/before.jpg", MimeType: "image/jpeg"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000002"}, SHA256: "after-cursor", StorageKey: "sha256/af/te/after.jpg", MimeType: "image/jpeg"},
	}
	if err := db.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	selections := []model.MetadataArtwork{
		{MetadataID: items[0].ID, ArtworkType: model.ArtworkTypePoster, AssetID: assets[0].ID},
		{MetadataID: items[1].ID, ArtworkType: model.ArtworkTypePoster, AssetID: assets[1].ID},
	}
	if err := db.Create(&selections).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), artworkIntegrityCursorKey, assets[0].ID); err != nil {
		t.Fatal(err)
	}

	svc := &ScraperService{
		repo:    repos,
		artwork: NewArtworkStore(&config.Config{App: config.AppConfig{DataDir: t.TempDir()}}, repos.Artwork, nil),
	}
	metrics := map[string]int64{}
	if err := svc.scanSelectedArtworkAssets(t.Context(), metrics); err != nil {
		t.Fatal(err)
	}
	if metrics["assets_scanned"] != 1 || metrics["assets_missing"] != 1 || metrics["selections_invalidated"] != 1 {
		t.Fatalf("asset scan metrics = %#v", metrics)
	}
	if selected, err := repos.Artwork.FindSelection(t.Context(), items[0].ID, model.ArtworkTypePoster); err != nil || selected == nil {
		t.Fatalf("selection before cursor = %#v, %v", selected, err)
	}
	if selected, err := repos.Artwork.FindSelection(t.Context(), items[1].ID, model.ArtworkTypePoster); err != nil || selected != nil {
		t.Fatalf("selection after cursor = %#v, %v", selected, err)
	}
	if cursor, err := repos.Setting.Get(t.Context(), artworkIntegrityCursorKey); err != nil || cursor != "" {
		t.Fatalf("cursor after reaching end = %q, %v", cursor, err)
	}
}
