package service

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbArtworkLocalRepairRestoresOldURLWithoutCatalogOrTMDb(t *testing.T) {
	db := newServiceTestDB(t, &model.CatalogHydrationJob{}, &model.MetadataArtworkRecheck{})
	repos := repository.New(db)
	now := time.Now().UTC()
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb", CatalogArtworkHydratedAt: &now}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	oldAsset := model.ArtworkAsset{SHA256: "missing", StorageKey: "sha256/mi/ss/missing.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&oldAsset).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: metadata.ID, ArtworkType: model.ArtworkTypePoster, AssetID: oldAsset.ID, SourceProvider: "tmdb", SourceURL: "https://old.test/poster.jpg"}
	if err := db.Create(&selection).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(root, "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(testJPEG)), Request: req}, nil
	})}
	store := NewArtworkStore(&config.Config{App: config.AppConfig{DataDir: root}}, repos.Artwork, proxy)
	svc := &ScraperService{repo: repos, artwork: store}
	if err := svc.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	selected, err := repos.Artwork.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil || selected == nil || selected.ID == oldAsset.ID {
		t.Fatalf("repaired selection = %#v, %v", selected, err)
	}
	path, err := store.pathForStorageKey(selected.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if available, err := localArtworkFileAvailable(path); err != nil || !available {
		t.Fatalf("repaired local file available=%v err=%v", available, err)
	}
	var jobs int64
	if err := db.Model(&model.CatalogHydrationJob{}).Count(&jobs).Error; err != nil || jobs != 0 {
		t.Fatalf("catalog jobs = %d, %v", jobs, err)
	}
	updated, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil || updated == nil || updated.CatalogArtworkHydratedAt == nil {
		t.Fatalf("artwork checkpoint changed = %#v, %v", updated, err)
	}
}

func TestDoubanArtworkLocalRepairUpgradesCandidateWithoutReplacingManualSelection(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	manual := &model.ArtworkAsset{SHA256: "manual-douban-repair", StorageKey: "manual-douban-repair.jpg", MimeType: "image/jpeg"}
	if _, err := repos.Artwork.SaveSelection(t.Context(), metadata.ID, model.ArtworkTypePoster, "manual", "manual.jpg", manual); err != nil {
		t.Fatal(err)
	}
	old := &model.ArtworkAsset{SHA256: "douban-small", StorageKey: "sha256/do/ub/douban-small.jpg", MimeType: "image/jpeg", Width: 270, Height: 400}
	oldURL := "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p123.jpg"
	if _, _, err := repos.Artwork.SaveCandidate(t.Context(), metadata.ID, model.ArtworkTypePoster, "douban", oldURL, old); err != nil {
		t.Fatal(err)
	}
	payload := `{"cover":{"image":{"large":{"url":"https://img9.doubanio.com/view/photo/l/public/p123.jpg"}}}}`
	if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: metadata.ID, Provider: "douban", Payload: payload, FetchedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	cfg := &config.Config{App: config.AppConfig{DataDir: root}, Cache: config.CacheConfig{CacheDir: filepath.Join(root, "cache")}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	requestedURL := ""
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(testJPEG)), Request: req}, nil
	})}
	store := NewArtworkStore(cfg, repos.Artwork, proxy)
	oldPath, err := store.pathForStorageKey(old.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("old-small"), 0o600); err != nil {
		t.Fatal(err)
	}

	apiConfig := NewAPIConfigService(zap.NewNop(), repos, NewCryptoService("test-secret", zap.NewNop()))
	origin := "http://db-pic1.acecandy.cn/"
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{BaseURL: &origin}); err != nil {
		t.Fatal(err)
	}
	svc := &ScraperService{repo: repos, artwork: store, douban: NewDoubanProvider(apiConfig)}
	if err := svc.runDoubanArtworkLocalRepair(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if requestedURL != "http://db-pic1.acecandy.cn/view/photo/l/public/p123.webp" {
		t.Fatalf("Douban artwork URL = %q", requestedURL)
	}
	selected, err := repos.Artwork.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil || selected == nil || selected.ID != manual.ID {
		t.Fatalf("manual selection changed: %#v, %v", selected, err)
	}
	var candidate model.MetadataArtworkCandidate
	if err := db.First(&candidate, "metadata_id = ? AND artwork_type = ? AND source_provider = 'douban'", metadata.ID, model.ArtworkTypePoster).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.AssetID == old.ID || candidate.SourceURL != "http://db-pic1.acecandy.cn/view/photo/l/public/p123.webp" {
		t.Fatalf("repaired candidate = %#v", candidate)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old small artwork was removed: %v", err)
	}
}

func TestDoubanRepairPosterURLDerivesKnownPhotoVariant(t *testing.T) {
	payload := `{"pic":{"large":"https://img9.doubanio.com/view/photo/m_ratio_poster/public/p123.jpg?x=1"}}`
	if got := doubanRepairPosterURL(payload, ""); got != "https://img9.doubanio.com/view/photo/l/public/p123.jpg?x=1" {
		t.Fatalf("derived Douban poster URL = %q", got)
	}
}

func TestTMDbArtworkLocalRepairOmitsHealthyFileDetail(t *testing.T) {
	store := NewArtworkStore(&config.Config{App: config.AppConfig{DataDir: t.TempDir()}}, nil, nil)
	item := repository.TMDbArtworkSelection{StorageKey: "sha256/aa/bb/healthy.jpg"}
	path, err := store.pathForStorageKey(item.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("healthy"), 0o600); err != nil {
		t.Fatal(err)
	}
	detail, failed := (&ScraperService{artwork: store}).repairTMDbArtworkSelection(t.Context(), item, map[string]int64{})
	if failed || detail != "" {
		t.Fatalf("healthy repair detail=%q failed=%v", detail, failed)
	}
}

func TestTMDbArtworkLocalRepairRefreshesOnlyAfter404(t *testing.T) {
	db := newServiceTestDB(t, &model.MetadataArtworkRecheck{})
	repos := repository.New(db)
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	oldAsset := model.ArtworkAsset{SHA256: "missing-404", StorageKey: "sha256/mi/ss/missing-404.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&oldAsset).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: metadata.ID, ArtworkType: model.ArtworkTypePoster, AssetID: oldAsset.ID, SourceProvider: "tmdb", SourceURL: "https://old.test/missing.jpg"}
	if err := db.Create(&selection).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{
		App: config.AppConfig{DataDir: root}, Cache: config.CacheConfig{CacheDir: filepath.Join(root, "cache")},
		Secrets: config.SecretsConfig{TMDbAPIKey: "test", TMDbAPIProxy: "https://api.test", TMDbImageProxy: "https://img.test"},
	}
	proxy := NewImageProxy(cfg, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "old.test" {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("missing")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(testJPEG)), Request: req}, nil
	})}
	var tmdbCalls atomic.Int32
	tmdb := NewTMDbProvider(cfg, zap.NewNop(), nil)
	tmdb.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		tmdbCalls.Add(1)
		body := `{"id":42,"title":"Movie","poster_path":"/new.jpg","backdrop_path":""}`
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	store := NewArtworkStore(cfg, repos.Artwork, proxy)
	svc := &ScraperService{repo: repos, artwork: store, tmdb: tmdb}
	if err := svc.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if tmdbCalls.Load() != 1 {
		t.Fatalf("TMDb calls after 404 = %d, want 1", tmdbCalls.Load())
	}
	var updated model.MetadataArtwork
	if err := db.First(&updated, "id = ?", selection.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.AssetID == oldAsset.ID || updated.SourceURL != "https://img.test/original/new.jpg" {
		t.Fatalf("selection after 404 refresh = %#v", updated)
	}
}

func TestTMDbArtworkMissingRecheckHonorsTaskHandoffCooldown(t *testing.T) {
	db := newServiceTestDB(t, &model.MetadataArtworkRecheck{})
	repos := repository.New(db)
	now := time.Now().UTC()
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Artwork.UpsertArtworkRecheck(t.Context(), metadata.ID, model.ArtworkTypePoster, now); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}, Secrets: config.SecretsConfig{TMDbAPIKey: "test"}}
	tmdb := NewTMDbProvider(cfg, zap.NewNop(), nil)
	tmdb.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
	})}
	svc := &ScraperService{repo: repos, tmdb: tmdb, artwork: NewArtworkStore(cfg, repos.Artwork, NewImageProxy(cfg, zap.NewNop()))}
	if err := svc.runTMDbArtworkMissingRecheck(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("TMDb calls during cooldown = %d, want 0", calls.Load())
	}
}

func TestTMDbArtworkMissingRecheckOmitsHealthySelectionDetail(t *testing.T) {
	db := newServiceTestDB(t, &model.Setting{}, &model.MetadataArtworkRecheck{})
	repos := repository.New(db)
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "healthy", StorageKey: "sha256/aa/bb/healthy.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: metadata.ID, ArtworkType: model.ArtworkTypePoster, AssetID: asset.ID, SourceProvider: "tmdb"}
	if err := db.Create(&selection).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repos.Artwork.UpsertArtworkRecheck(t.Context(), metadata.ID, model.ArtworkTypePoster, now); err != nil {
		t.Fatal(err)
	}
	store := NewArtworkStore(&config.Config{App: config.AppConfig{DataDir: t.TempDir()}}, repos.Artwork, nil)
	path, err := store.pathForStorageKey(asset.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("healthy"), 0o600); err != nil {
		t.Fatal(err)
	}
	details, requested, failures := (&ScraperService{repo: repos, artwork: store}).recheckTMDbArtwork(t.Context(), repository.TMDbArtworkRecheckCandidate{
		MetadataID: metadata.ID,
		Title:      metadata.Title,
		Kind:       metadata.Kind,
		Types: []repository.TMDbArtworkRecheckType{{
			ArtworkType:   model.ArtworkTypePoster,
			SelectionID:   selection.ID,
			StorageKey:    asset.StorageKey,
			LastNoImageAt: &now,
		}},
	}, now, map[string]int64{})
	if requested || failures != 0 || len(details) != 0 {
		t.Fatalf("healthy recheck details=%q requested=%v failures=%d", details, requested, failures)
	}
}
