package service

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestArtworkStorePersistsAndDeduplicatesLocalImages(t *testing.T) {
	dataDir := t.TempDir()
	libraryDir := t.TempDir()
	source := filepath.Join(libraryDir, "poster.png")
	data := testArtworkPNG(t, 3, 2)
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	items := []model.MetadataItem{
		{Kind: model.MetadataKindMovie, Title: "One", Source: "local_nfo"},
		{Kind: model.MetadataKindMovie, Title: "Two", Source: "local_nfo"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{App: config.AppConfig{DataDir: dataDir}, Cache: config.CacheConfig{CacheDir: filepath.Join(dataDir, "cache")}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	proxy.SetLibraryRootsProvider(func() []string { return []string{libraryDir} })
	store := NewArtworkStore(cfg, repos.Artwork, proxy)

	first, err := store.ImportLocal(t.Context(), items[0].ID, model.ArtworkTypePoster, source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ImportLocal(t.Context(), items[1].ID, model.ArtworkTypePoster, source)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("asset IDs differ for identical content: %s != %s", first.ID, second.ID)
	}
	if first.Width != 3 || first.Height != 2 || first.SizeBytes != int64(len(data)) {
		t.Fatalf("unexpected asset metadata: %#v", first)
	}
	wantPath := filepath.Join(dataDir, "artwork", filepath.FromSlash(first.StorageKey))
	stored, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatal("stored artwork bytes differ from source")
	}
	var assetCount, selectionCount int64
	if err := db.Model(&model.ArtworkAsset{}).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataArtwork{}).Count(&selectionCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 1 || selectionCount != 2 {
		t.Fatalf("asset/selection counts = %d/%d, want 1/2", assetCount, selectionCount)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, ArtworkURL(first.ID), nil)
	if err := store.Serve(t.Context(), rec, req, first.ID); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), data) {
		t.Fatalf("served artwork mismatch: status=%d type=%q bytes=%d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

func testArtworkPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
