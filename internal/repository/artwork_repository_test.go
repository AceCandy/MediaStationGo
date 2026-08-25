package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestSaveCatalogSelectionPreservesExistingSelection(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.MetadataArtworkCandidate{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Artwork
	manual := &model.ArtworkAsset{SHA256: "manual", StorageKey: "manual.jpg", MimeType: "image/jpeg"}
	if _, err := repo.SaveSelection(t.Context(), metadata.ID, model.ArtworkTypePoster, "manual", "manual.jpg", manual); err != nil {
		t.Fatal(err)
	}
	catalog := &model.ArtworkAsset{SHA256: "catalog", StorageKey: "catalog.jpg", MimeType: "image/jpeg"}
	if _, selected, err := repo.SaveCatalogSelection(t.Context(), metadata.ID, model.ArtworkTypePoster, "tmdb", "catalog.jpg", catalog); err != nil || selected {
		t.Fatal(err)
	}
	selected, err := repo.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || selected.ID != manual.ID {
		t.Fatalf("catalog replaced manual selection: %#v", selected)
	}
	douban := &model.ArtworkAsset{SHA256: "douban", StorageKey: "douban.jpg", MimeType: "image/jpeg"}
	if _, promoted, err := repo.SaveCandidate(t.Context(), metadata.ID, model.ArtworkTypePoster, "douban", "douban.jpg", douban); err != nil || promoted {
		t.Fatalf("save douban candidate: promoted=%v err=%v", promoted, err)
	}
	selected, err = repo.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil || selected == nil || selected.ID != manual.ID {
		t.Fatalf("candidate replaced current selection: %#v, %v", selected, err)
	}
	if exists, err := repo.HasCandidate(t.Context(), metadata.ID, model.ArtworkTypePoster, "douban"); err != nil || !exists {
		t.Fatalf("douban candidate exists=%v err=%v", exists, err)
	}
}

func TestInvalidateSelectionsForMissingAssetPreservesSharedAsset(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.MetadataArtworkCandidate{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	items := []model.MetadataItem{
		{Kind: model.MetadataKindMovie, Title: "One", Source: "tmdb", CatalogArtworkHydratedAt: &now},
		{Kind: model.MetadataKindMovie, Title: "Two", Source: "tmdb", CatalogArtworkHydratedAt: &now},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "shared", StorageKey: "shared.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	selections := []model.MetadataArtwork{
		{MetadataID: items[0].ID, ArtworkType: model.ArtworkTypePoster, AssetID: asset.ID},
		{MetadataID: items[1].ID, ArtworkType: model.ArtworkTypePoster, AssetID: asset.ID},
	}
	if err := db.Create(&selections).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Artwork
	assets, err := repo.ListSelectedAssetsAfter(t.Context(), "", 20)
	if err != nil || len(assets) != 1 || assets[0].ID != asset.ID {
		t.Fatalf("selected assets = %#v, %v", assets, err)
	}
	invalidated, err := repo.InvalidateSelectionsForMissingAsset(t.Context(), asset.ID)
	if err != nil || invalidated != 2 {
		t.Fatalf("invalidated selections = %d, %v", invalidated, err)
	}
	var assetCount, selectionCount int64
	if err := db.Model(&model.ArtworkAsset{}).Where("id = ?", asset.ID).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataArtwork{}).Where("asset_id = ?", asset.ID).Count(&selectionCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 1 || selectionCount != 0 {
		t.Fatalf("asset/selection counts = %d/%d, want 1/0", assetCount, selectionCount)
	}
	for _, item := range items {
		var updated model.MetadataItem
		if err := db.First(&updated, "id = ?", item.ID).Error; err != nil || updated.CatalogArtworkHydratedAt != nil {
			t.Fatalf("metadata checkpoint after invalidation = %#v, %v", updated, err)
		}
	}
}
