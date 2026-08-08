package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

type cloudArtworkResolverFunc func(context.Context, string, string, string) (*cloud.DirectLink, error)

func (f cloudArtworkResolverFunc) CloudResolve(ctx context.Context, typ, ref, clientUA string) (*cloud.DirectLink, error) {
	return f(ctx, typ, ref, clientUA)
}

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

func TestPersistMetadataArtworkImportsCloudImagesWithoutCache(t *testing.T) {
	data := testArtworkPNG(t, 4, 3)
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Cloud-Auth") != "test-token" {
			t.Fatalf("missing cloud direct-link header")
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	}))
	defer upstream.Close()

	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}); err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Cloud", Source: "local_nfo"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cfg := &config.Config{App: config.AppConfig{DataDir: dataDir}, Cache: config.CacheConfig{CacheDir: cacheDir}}
	repos := repository.New(db)
	proxy := NewImageProxy(cfg, zap.NewNop())
	proxy.client = upstream.Client()
	store := NewArtworkStore(cfg, repos.Artwork, proxy).SetCloudResolver(cloudArtworkResolverFunc(
		func(_ context.Context, typ, ref, clientUA string) (*cloud.DirectLink, error) {
			if typ != "openlist" || clientUA != "" {
				t.Fatalf("unexpected cloud resolve arguments: type=%q ua=%q", typ, clientUA)
			}
			return &cloud.DirectLink{URL: upstream.URL + ref, Headers: map[string]string{"X-Cloud-Auth": "test-token"}}, nil
		},
	))
	scraper := &ScraperService{artwork: store}
	poster, backdrop, err := scraper.persistMetadataArtwork(
		t.Context(), item.ID, "local_nfo",
		"/api/img/cloud/openlist?ref=%2FMovies%2Fposter.png",
		"/api/img/cloud/openlist?ref=%2FMovies%2Fbackdrop.png",
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !strings.HasPrefix(poster, "/api/artwork/") || !strings.HasPrefix(backdrop, "/api/artwork/") {
		t.Fatalf("cloud artwork persistence = requests:%d poster:%q backdrop:%q", requests, poster, backdrop)
	}
	for _, artworkType := range []string{model.ArtworkTypePoster, model.ArtworkTypeBackdrop} {
		asset, err := repos.Artwork.FindSelection(t.Context(), item.ID, artworkType)
		if err != nil {
			t.Fatal(err)
		}
		if asset == nil {
			t.Fatalf("missing %s selection", artworkType)
		}
		stored, err := os.ReadFile(filepath.Join(dataDir, "artwork", filepath.FromSlash(asset.StorageKey)))
		if err != nil || !bytes.Equal(stored, data) {
			t.Fatalf("stored %s artwork mismatch: %v", artworkType, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "images")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cloud metadata import created image cache: %v", err)
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
