package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
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
		PermanentBase: model.PermanentBase{ID: metadataID}, Kind: model.MetadataKindMovie, Title: "Artwork Test", Source: "test",
	}).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	if err := db.Create(&model.ArtworkAsset{
		PermanentBase: model.PermanentBase{ID: assetID}, SHA256: strings.Repeat("a", 64), StorageKey: storageKey,
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
		PermanentBase: model.PermanentBase{ID: mediaID}, MetadataID: metadataID, Title: "Artwork Test", Path: "/media/" + mediaID + ".mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
}

func TestEmbyItemImageServesWithoutAPIAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Media{}); err != nil {
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

	req := httptest.NewRequest(http.MethodGet, "/Items/metadata-media-1/Images/Primary", nil)
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
	if !bytes.Equal(w.Body.Bytes(), posterData) {
		t.Fatal("expected original poster bytes")
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
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Media{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	imageProxy := service.NewImageProxy(cfg, zap.NewNop())
	repos := repository.New(db)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	embyTestJPEG := encoded.Bytes()
	createEmbyArtworkFixture(t, db, cfg, "media-artwork-1", embyTestJPEG, "image/jpeg", "jpg")

	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo:       repos,
		Emby:       service.NewEmbyService(cfg, zap.NewNop(), repos),
		Artwork:    service.NewArtworkStore(cfg, repos.Artwork, imageProxy),
		ImageProxy: imageProxy,
	})

	req := httptest.NewRequest(http.MethodGet, "/Items/metadata-media-artwork-1/Images/Primary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.Bytes(); !bytes.Equal(got, embyTestJPEG) {
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
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
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
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("placeholder Cache-Control = %q, want no-store", got)
	}
	if w.Body.Len() != 0 {
		t.Fatal("HEAD placeholder has a body")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Items/missing/Images/Primary", nil))
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), embyPlaceholderPNG) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("GET placeholder must preserve bytes without caching")
	}
}

func TestEmbyUserItemByIDRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
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
		PermanentBase: model.PermanentBase{
			ID:        "episode-1",
			CreatedAt: time.Date(2026, time.August, 6, 20, 9, 14, 746_854_000, time.FixedZone("UTC+8", 8*60*60)),
		},
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
	if item["DateCreated"] != "2026-08-06T12:09:14.7468540Z" {
		t.Fatalf("item date created = %#v", item["DateCreated"])
	}
}

func TestEmbyUserItemByIDRouteReturnsLibraryView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
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

func TestEmbyHongGuoDetailClickRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// 红果文件不经过普通资料 fixture，避免生成 metadata_id。
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "admin", Tier: "plus", IsActive: true},
		&model.Library{Base: model.Base{ID: "library"}, Name: "红果", Path: "/test/hg", Type: model.LibraryTypeHongGuo},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work-1"}, SourceID: "1001", Kind: "series", Title: "第一季", RelatedAlbumID: "album", SeasonIndex: 1},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work-2"}, SourceID: "1002", Kind: "series", Title: "第二季", RelatedAlbumID: "album", SeasonIndex: 2},
		&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "episode-1"}, WorkID: "work-1", Number: 1},
		&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "episode-2"}, WorkID: "work-2", Number: 1},
		&model.Media{PermanentBase: model.PermanentBase{ID: "file-1"}, LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "1001", SeasonNum: 1, EpisodeNum: 1, Path: "/test/hg/1.strm"},
		&model.Media{PermanentBase: model.PermanentBase{ID: "file-2"}, LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "1002", SeasonNum: 1, EpisodeNum: 1, Path: "/test/hg/2.strm"},
		&model.HongGuoUserState{UserID: "user-1", SourceID: "1001", EpisodeNumber: 1, Completed: true},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"1", "2"} {
		episodeID := "episode-" + n
		if err := db.Create(&model.HongGuoMediaBinding{MediaID: "file-" + n, WorkID: "work-" + n, EpisodeID: &episodeID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repos := repository.New(db)
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{Repo: repos, Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos)})
	token := signedTestToken(t, "test-secret")
	for _, tc := range []struct {
		path, id, kind, parent string
		total                  int
		played                 bool
	}{
		{"/Users/user-1/Items/hg-group-album", "hg-group-album", "Series", "", 0, false},
		{"/users/user-1/items/hg-season-work-1", "hg-season-work-1", "Season", "hg-group-album", 0, true},
		{"/Users/user-1/Items/hg-episode-episode-1", "hg-episode-episode-1", "Episode", "hg-season-work-1", 0, true},
		{"/Shows/hg-group-album/Seasons", "hg-season-work-1", "Season", "hg-group-album", 2, true},
		{"/Users/user-1/Shows/hg-group-album/Episodes", "hg-episode-episode-1", "Episode", "hg-season-work-1", 2, true},
		{"/shows/hg-group-album/episodes?seasonId=hg-season-work-2", "hg-episode-episode-2", "Episode", "hg-season-work-2", 1, false},
		{"/Users/user-1/Items?ParentId=hg-group-album&IncludeItemTypes=Episode&Recursive=true&Limit=1&StartIndex=1&EnableTotalRecordCount=true", "hg-episode-episode-2", "Episode", "hg-season-work-2", 2, false},
	} {
		for _, prefix := range []string{"", "/emby"} {
			request := httptest.NewRequest(http.MethodGet, prefix+tc.path, nil)
			request.Header.Set("X-Emby-Token", token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK {
				t.Fatalf("%s: status=%d body=%s err=%v", tc.path, response.Code, response.Body.String(), err)
			}
			item := result
			if tc.total > 0 {
				items, _ := result["Items"].([]any)
				if result["TotalRecordCount"] != float64(tc.total) || len(items) == 0 {
					t.Fatalf("%s: invalid list %v", tc.path, result)
				}
				item = items[0].(map[string]any)
			}
			if item["Id"] != tc.id || item["Type"] != tc.kind || item["ParentId"] != tc.parent || item["UserData"].(map[string]any)["Played"] != tc.played {
				t.Fatalf("%s: invalid item %v", tc.path, item)
			}
			if tc.kind == "Series" || tc.kind == "Season" {
				want := float64(1)
				if tc.played {
					want = 0
				}
				if item["UserData"].(map[string]any)["UnplayedItemCount"] != want {
					t.Fatalf("%s: invalid unread count %v", tc.path, item["UserData"])
				}
			}
		}
	}
}
