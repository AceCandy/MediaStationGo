package handler

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestEmbyHongGuoImageServesLocalArtwork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	proxy := service.NewImageProxy(cfg, zap.NewNop())
	key := "test/poster.jpg"
	path := filepath.Join(cfg.App.DataDir, "catalogs", "hongguo", "artwork", key)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 80, 120)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	work := model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "image-work"}, SourceID: "image-source", Kind: "series", Title: "海报测试"}
	for _, row := range []any{
		&work,
		&model.HongGuoArtwork{PermanentBase: model.PermanentBase{ID: "image-art"}, WorkID: &work.ID, LocalKey: key},
		&model.Media{PermanentBase: model.PermanentBase{ID: "image-file"}, Path: "/test/artwork.strm", CatalogSource: "hongguo"},
		&model.HongGuoMediaBinding{MediaID: "image-file", WorkID: work.ID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo: repos, Emby: service.NewEmbyService(cfg, zap.NewNop(), repos), ImageProxy: proxy,
		HongGuo: service.NewHongGuoService(repos, nil, proxy, cfg.App.DataDir),
	})
	for _, prefix := range []string{"", "/emby"} {
		for _, route := range []string{"/Items/hg-work-image-work/Images/Primary", "/items/hg-season-image-work/images/primary"} {
			for _, query := range []string{"", "?maxWidth=40&format=jpeg"} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					w := httptest.NewRecorder()
					router.ServeHTTP(w, httptest.NewRequest(method, prefix+route+query, nil))
					if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || !strings.Contains(w.Header().Get("Cache-Control"), "max-age=2592000") {
						t.Fatalf("artwork response: status=%d type=%q cache=%q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
					}
					if method == http.MethodHead {
						if w.Body.Len() != 0 {
							t.Fatal("HEAD has body")
						}
						continue
					}
					if query == "" && !bytes.Equal(w.Body.Bytes(), encoded.Bytes()) {
						t.Fatal("original artwork bytes changed")
					}
					img, _, err := image.DecodeConfig(w.Body)
					width, height := 80, 120
					if query != "" {
						width, height = 40, 60
					}
					if err != nil || img.Width != width || img.Height != height {
						t.Fatalf("artwork dimensions=%dx%d err=%v", img.Width, img.Height, err)
					}
				}
			}
		}
	}
}

func TestEmbyPersonImageServesPersistedLocalFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Person{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	proxy := service.NewImageProxy(cfg, zap.NewNop())
	people := service.NewPeopleImageStore(cfg, repos.Person, proxy)
	want := embyPlaceholderPNG
	key := "sha256/aa/bb/profile.jpg"
	path := filepath.Join(cfg.App.DataDir, "people", filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	person := model.Person{Base: model.Base{ID: "person-local"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor-local", ProfileURL: "https://images.example/profile.jpg", ProfileImageKey: key, Source: "tmdb"}
	if err := repos.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo:         repos,
		Emby:         service.NewEmbyService(cfg, zap.NewNop(), repos),
		PeopleImages: people,
		ImageProxy:   proxy,
	})
	for _, route := range []string{
		"/Items/person-local/Images/Primary",
		"/items/person-local/images/primary",
		"/emby/Items/person-local/Images/Primary",
		"/emby/items/person-local/images/primary",
	} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Fatalf("route %s status=%d body=%d, want local image", route, rec.Code, rec.Body.Len())
		}
		if strings.Contains(rec.Header().Get("Location"), "images.example") {
			t.Fatalf("route %s redirected to source URL", route)
		}
	}
	for _, route := range []string{
		"/Items/person-local/Images/Primary",
		"/items/person-local/images/primary",
		"/emby/Items/person-local/Images/Primary",
		"/emby/items/person-local/images/primary",
	} {
		req := httptest.NewRequest(http.MethodHead, route, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("HEAD route %s status=%d body=%d content-type=%q", route, rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
		}
	}
}
