package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func createEmbyArtworkFixture(t *testing.T, db *gorm.DB, cfg *config.Config, mediaID string, data []byte, mimeType, extension string) {
	t.Helper()
	metadataID := "metadata-" + mediaID
	assetID := "asset-" + mediaID
	storageKey := "test/" + mediaID + "/poster." + extension
	path := filepath.Join(cfg.App.DataDir, "artwork", filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create artwork dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write artwork: %v", err)
	}
	if err := db.Create(&model.MetadataItem{
		Base: model.Base{ID: metadataID}, Kind: model.MetadataKindMovie, Title: "Artwork Test", Source: "test",
	}).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	if err := db.Create(&model.ArtworkAsset{
		Base: model.Base{ID: assetID}, SHA256: strings.Repeat("a", 64), StorageKey: storageKey,
		MimeType: mimeType, SizeBytes: int64(len(data)),
	}).Error; err != nil {
		t.Fatalf("create artwork asset: %v", err)
	}
	if err := db.Create(&model.MetadataArtwork{
		MetadataID: metadataID, AssetID: assetID, ArtworkType: model.ArtworkTypePoster,
	}).Error; err != nil {
		t.Fatalf("create metadata artwork: %v", err)
	}
	if err := db.Create(&model.Media{
		Base: model.Base{ID: mediaID}, MetadataID: metadataID, Title: "Artwork Test", Path: "/media/" + mediaID + ".mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
}

func TestEmbyItemImageServesWithoutAPIAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Media{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	posterData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}

	repos := repository.New(db)
	cfg := &config.Config{
		App:   config.AppConfig{DataDir: t.TempDir()},
		Cache: config.CacheConfig{CacheDir: t.TempDir()},
	}
	createEmbyArtworkFixture(t, db, cfg, "media-1", posterData, "image/png", "png")
	imageProxy := service.NewImageProxy(cfg, zap.NewNop())

	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo:       repos,
		Emby:       service.NewEmbyService(cfg, zap.NewNop(), repos),
		Artwork:    service.NewArtworkStore(cfg, repos.Artwork, imageProxy),
		ImageProxy: imageProxy,
	})

	req := httptest.NewRequest(http.MethodGet, "/Items/media-1/Images/Primary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "" {
		t.Fatalf("expected direct image response, got redirect to %q", location)
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "image/png") {
		t.Fatalf("expected png content type, got %q", contentType)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "max-age=2592000") {
		t.Fatalf("image Cache-Control = %q, want long browser cache", got)
	}
	if got := w.Header().Get("Pragma"); got != "" {
		t.Fatalf("image Pragma = %q, want empty", got)
	}
	if got := w.Header().Get("Expires"); got != "" {
		t.Fatalf("image Expires = %q, want empty", got)
	}
}

func TestEmbyItemImageServesPersistentArtworkWithoutResolve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Media{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	imageProxy := service.NewImageProxy(cfg, zap.NewNop())
	repos := repository.New(db)
	createEmbyArtworkFixture(t, db, cfg, "cloud-media-1", handlerTestJPEG, "image/jpeg", "jpg")

	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo:       repos,
		Emby:       service.NewEmbyService(cfg, zap.NewNop(), repos),
		Artwork:    service.NewArtworkStore(cfg, repos.Artwork, imageProxy),
		ImageProxy: imageProxy,
	})

	req := httptest.NewRequest(http.MethodGet, "/Items/cloud-media-1/Images/Primary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.Bytes(); !bytes.Equal(got, handlerTestJPEG) {
		t.Fatalf("body = %q, want persistent artwork", got)
	}
	if location := w.Header().Get("Location"); location != "" {
		t.Fatalf("expected direct cached image response, got redirect to %q", location)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "max-age=2592000") {
		t.Fatalf("image Cache-Control = %q, want long browser cache", got)
	}
}

func TestEmbyMissingItemImageReturnsTransparentPlaceholder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	cfg := &config.Config{Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo:       repos,
		Emby:       service.NewEmbyService(cfg, zap.NewNop(), repos),
		ImageProxy: service.NewImageProxy(cfg, zap.NewNop()),
	})

	req := httptest.NewRequest(http.MethodHead, "/Items/missing/Images/Primary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected placeholder status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "image/png") {
		t.Fatalf("expected png content type, got %q", contentType)
	}
	if length := w.Header().Get("Content-Length"); length == "" || length == "0" {
		t.Fatalf("expected placeholder content length, got %q", length)
	}
	if got := w.Header().Get("Pragma"); got != "" {
		t.Fatalf("placeholder Pragma = %q, want empty", got)
	}
	if got := w.Header().Get("Expires"); got != "" {
		t.Fatalf("placeholder Expires = %q, want empty", got)
	}
}

func TestEmbyUserItemByIDRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "剧集", Path: "D:\\media\\tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:       model.Base{ID: "episode-1"},
		LibraryID:  lib.ID,
		Title:      "Test Show",
		Path:       "D:\\media\\tv\\Test Show\\Season 01\\Test Show - S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
		Container:  "mkv",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/Users/user-1/Items/episode-1", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	req.Header.Set("If-None-Match", `"stale-client-cache"`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q body=%s", contentType, w.Body.String())
	}
	var item map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item["Id"] != media.MetadataID || item["Type"] != "Episode" {
		t.Fatalf("unexpected item payload: %#v", item)
	}
}

func TestEmbyUserItemByIDRouteReturnsLibraryView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Base: model.Base{ID: "lib-tv"}, Name: "剧集", Path: "D:\\media\\tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/Users/user-1/Items/lib-tv", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var item map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item["Id"] != "lib-tv" || item["Type"] != "CollectionFolder" || item["CollectionType"] != "tvshows" {
		t.Fatalf("unexpected library payload: %#v", item)
	}
}
