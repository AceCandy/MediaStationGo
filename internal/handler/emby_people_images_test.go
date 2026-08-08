package handler

import (
	"bytes"
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
