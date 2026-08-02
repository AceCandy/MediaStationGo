package service

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestScrapeDelayUsesSettings(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}

	if got := scraper.scrapeDelay(t.Context()); got < 250*time.Millisecond || got > 500*time.Millisecond {
		t.Fatalf("default scrapeDelay = %s, want 250-500ms", got)
	}

	if err := repos.Setting.Set(t.Context(), "scrape.delay_min_ms", "0"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "scrape.delay_max_ms", "0"); err != nil {
		t.Fatal(err)
	}
	if got := scraper.scrapeDelay(t.Context()); got != 0 {
		t.Fatalf("disabled scrapeDelay = %s, want 0", got)
	}

	if err := repos.Setting.Set(t.Context(), "scrape.delay_min_ms", "800"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "scrape.delay_max_ms", "200"); err != nil {
		t.Fatal(err)
	}
	if got := scraper.scrapeDelay(t.Context()); got != 800*time.Millisecond {
		t.Fatalf("normalized scrapeDelay = %s, want 800ms", got)
	}
}

func TestApplyProviderMatchInvalidatesMediaCache(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	cache := NewRuntimeCacheService(&config.Config{}, zap.NewNop())
	cache.SetJSON(t.Context(), "media:list:stale", map[string]string{"poster": ""}, time.Minute)
	cache.SetJSON(t.Context(), "stats:snapshot:base", map[string]int{"media": 1}, time.Minute)
	scraper.SetRuntimeCache(cache)

	libPath := t.TempDir()
	lib := model.Library{Name: "Movies", Path: libPath, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "Raw", Path: "/media/movies/raw.mkv", ScrapeStatus: "pending"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	match := &Match{Title: "Matched", PosterURL: "https://image.tmdb.org/t/p/w500/poster.jpg", BackdropURL: "https://image.tmdb.org/t/p/w1280/backdrop.jpg"}
	if err := scraper.applyProviderMatch(t.Context(), &media, &lib, match); err != nil {
		t.Fatal(err)
	}

	var stale map[string]string
	if cache.GetJSON(t.Context(), "media:list:stale", &stale) {
		t.Fatal("scraper should invalidate media list cache after applying artwork")
	}
	var stats map[string]int
	if cache.GetJSON(t.Context(), "stats:snapshot:base", &stats) {
		t.Fatal("scraper should invalidate stats cache after applying artwork")
	}
	view := serviceTestMediaView(t, repos, media.ID)
	if !strings.HasPrefix(view.PosterURL, "/api/artwork/") || !strings.HasPrefix(view.BackdropURL, "/api/artwork/") || view.ScrapeStatus != "matched" {
		t.Fatalf("match not saved: poster=%q backdrop=%q status=%q", view.PosterURL, view.BackdropURL, view.ScrapeStatus)
	}
}

func TestApplyProviderMatchKeepsExistingArtworkWhenImportFails(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	images := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	images.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("bad gateway")),
			Request:    req,
		}, nil
	})}
	scraper.SetImageProxy(images)
	scraper.SetArtworkStore(NewArtworkStore(scraper.cfg, repos.Artwork, images))

	libPath := t.TempDir()
	lib := model.Library{Name: "Movies", Path: libPath, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, repos.DB, model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "Raw", Source: "tmdb",
	}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "77"})
	oldAssetID := "old-poster-asset"
	createServiceTestArtwork(t, repos.DB, metadata.ID, model.ArtworkTypePoster, oldAssetID)
	media := model.Media{
		LibraryID:    lib.ID,
		MetadataID:   metadata.ID,
		Title:        "Raw",
		Path:         filepath.Join(libPath, "raw.mkv"),
		ScrapeStatus: "matched",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	match := &Match{
		Title:       "Matched",
		TMDbID:      77,
		PosterURL:   "https://image.tmdb.org/t/p/w500/new-broken-poster.jpg",
		BackdropURL: "",
	}
	if err := scraper.applyProviderMatch(t.Context(), &media, &lib, match); err == nil {
		t.Fatal("broken artwork import should fail the match")
	}

	var stored model.Media
	if err := repos.DB.First(&stored, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ScrapeStatus != "error" {
		t.Fatalf("scrape status = %q, want error", stored.ScrapeStatus)
	}
	selection, err := repos.Artwork.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil {
		t.Fatal(err)
	}
	if selection == nil || selection.ID != oldAssetID {
		t.Fatalf("poster selection after failed import = %#v, want %q", selection, oldAssetID)
	}
}

func TestApplyProviderMatchReplacesSelectedArtwork(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	imageData := testArtworkPNG(t, 4, 3)

	images := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	images.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(imageData)),
			Request:    req,
		}, nil
	})}
	scraper.SetImageProxy(images)
	scraper.SetArtworkStore(NewArtworkStore(scraper.cfg, repos.Artwork, images))

	libPath := t.TempDir()
	lib := model.Library{Name: "Movies", Path: libPath, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	newPoster := "https://image.tmdb.org/t/p/w500/new-cache-poster.jpg"
	metadata := createServiceTestMetadata(t, repos.DB, model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "Raw", Source: "tmdb",
	}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "88"})
	oldAssetID := "old-poster-asset"
	createServiceTestArtwork(t, repos.DB, metadata.ID, model.ArtworkTypePoster, oldAssetID)
	media := model.Media{
		LibraryID:    lib.ID,
		MetadataID:   metadata.ID,
		Title:        "Raw",
		Path:         filepath.Join(libPath, "raw.mkv"),
		ScrapeStatus: "matched",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	match := &Match{Title: "Matched", TMDbID: 88, PosterURL: newPoster}
	if err := scraper.applyProviderMatch(t.Context(), &media, &lib, match); err != nil {
		t.Fatal(err)
	}

	view := serviceTestMediaView(t, repos, media.ID)
	selection, err := repos.Artwork.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil {
		t.Fatal(err)
	}
	if selection == nil || selection.ID == oldAssetID || view.PosterURL != ArtworkURL(selection.ID) {
		t.Fatalf("selected poster not replaced: selection=%#v view=%q", selection, view.PosterURL)
	}
	path := filepath.Join(scraper.cfg.App.DataDir, "artwork", filepath.FromSlash(selection.StorageKey))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("managed poster does not exist: %v", err)
	}
	var oldAssetCount int64
	if err := repos.DB.Model(&model.ArtworkAsset{}).Where("id = ?", oldAssetID).Count(&oldAssetCount).Error; err != nil {
		t.Fatal(err)
	}
	if oldAssetCount != 1 {
		t.Fatalf("old artwork asset count = %d, want 1 until artwork GC exists", oldAssetCount)
	}
}
