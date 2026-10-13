package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestDiscoverCatalogReadPermissionsAndProjection(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	user := model.User{Username: "catalog-viewer", Role: "user"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	permission := model.UserPermission{UserID: user.ID, CanViewDiscover: false}
	if err = db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}, Secrets: config.SecretsConfig{JWTSecret: "catalog-test-secret"}}
	svc := service.New(cfg, zap.NewNop(), repos)
	t.Cleanup(svc.Close)
	now := time.Now().UTC()
	poster := "https://img9.doubanio.com/view/photo/l/public/catalog-test.jpg"
	raw := `{"id":"100","title":"已保存豆瓣作品","rating":8,"pic":{"large":"` + poster + `"},"unknown_source_field":"admin-only"}`
	if err = repos.Douban.Save(t.Context(), "100", "movie", []byte(raw), model.DoubanQualityComplete, now); err != nil {
		t.Fatal(err)
	}
	douban, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil {
		t.Fatal(err)
	}
	key := "catalogs/douban/artwork/aa/bb/cc/aabbcc" + strings.Repeat("0", 58) + ".png"
	var pixels bytes.Buffer
	if err = png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.App.DataDir, filepath.FromSlash(key))
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, pixels.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "aabbcc" + strings.Repeat("0", 58), StorageKey: key, MimeType: "image/png", Width: 2, Height: 3, SizeBytes: int64(pixels.Len())}
	if err = db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(douban).Updates(map[string]any{"poster_asset_id": asset.ID, "poster_source_url": poster, "last_attempt_at": now, "next_retry_at": now.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err = repos.TMDb.SaveResponse(t.Context(), repository.TMDbSourceRequest{Kind: "movie", TMDbID: 8, Component: "detail", Language: "zh-CN"}, []byte(`{"id":8,"title":"已保存 TMDb 作品","unknown_source_field":"admin-only","credits":{"cast":[],"crew":[]}}`), now, ""); err != nil {
		t.Fatal(err)
	}
	tmdb, err := repos.TMDb.SubjectIdentity(t.Context(), "movie", 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"anonymous", "denied", "viewer", "admin"} {
		if mode == "viewer" {
			if err = db.Model(&permission).Update("can_view_discover", true).Error; err != nil {
				t.Fatal(err)
			}
		}
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if mode != "anonymous" {
				c.Set(middleware.CtxUserID, user.ID)
				role := "user"
				if mode == "admin" {
					role = "admin"
				}
				c.Set(middleware.CtxUserRole, role)
			}
			c.Next()
		})
		registerAuthedUISurfaceRoutes(router.Group("/api"), svc)
		admin := router.Group("/api/admin", middleware.AdminRequired())
		registerTMDbCenterRoutes(admin.Group("/tmdb"), svc, true)
		registerDoubanCenterRoutes(admin.Group("/douban"), svc, true)
		for _, request := range []struct {
			method, path string
			status       int
		}{
			{"GET", "/api/catalogs/douban/subjects", 200},
			{"GET", "/api/catalogs/douban/categories?type=movie", 200},
			{"GET", "/api/catalogs/douban/categories?type=season", 400},
			{"GET", "/api/catalogs/douban/subjects/" + douban.ID, 200},
			{"GET", "/api/catalogs/douban/subjects/" + douban.ID + "/poster", 200},
			{"HEAD", "/api/catalogs/douban/subjects/" + douban.ID + "/poster", 200},
			{"GET", "/api/catalogs/douban/subjects/" + douban.ID + "/credits/0/image", 404},
			{"HEAD", "/api/catalogs/douban/subjects/" + douban.ID + "/credits/0/image", 404},
			{"GET", "/api/catalogs/tmdb/subjects?kind=movie", 200},
			{"GET", "/api/catalogs/tmdb/subjects/" + tmdb.ID, 200},
			{"GET", "/api/catalogs/tmdb/people", 200},
			{"GET", "/api/catalogs/tmdb/categories?kind=person", 200},
			{"GET", "/api/catalogs/tmdb/categories?kind=episode", 400},
			{"GET", "/api/catalogs/tmdb/subjects?genre=invalid", 400},
		} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
			want := request.status
			if mode == "anonymous" {
				want = 401
			}
			if mode == "denied" {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("%s %s %s: %d want %d: %s", mode, request.method, request.path, w.Code, want, w.Body.String())
			}
			if want == 200 && request.method == "GET" && !strings.HasSuffix(request.path, "/poster") {
				if w.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal("unsafe read cache")
				}
				for _, private := range []string{"unknown_source_field", "raw_json", "storage_key", "image_key", "catalogs/douban/artwork"} {
					if strings.Contains(w.Body.String(), private) {
						t.Fatalf("public projection leaked %s", private)
					}
				}
				if strings.Contains(request.path, "/catalogs/douban/subjects") && (!strings.Contains(w.Body.String(), `"last_attempt_at":null`) || !strings.Contains(w.Body.String(), `"next_retry_at":null`)) {
					t.Fatal("public projection leaked refresh diagnostics")
				}
			}
		}
		if mode == "viewer" || mode == "admin" {
			for _, path := range []string{"/api/admin/douban/subjects/" + douban.ID, "/api/admin/tmdb/subjects/" + tmdb.ID} {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if mode == "viewer" && w.Code != 403 {
					t.Fatalf("viewer read admin diagnostics: %d", w.Code)
				}
				if mode == "admin" && (w.Code != 200 || !strings.Contains(w.Body.String(), "unknown_source_field")) {
					t.Fatal("admin raw projection missing")
				}
			}
		}
		if mode == "viewer" {
			for _, path := range []string{"/api/admin/douban/subjects/" + douban.ID + "/refresh", "/api/admin/tmdb/subjects/" + tmdb.ID + "/refresh", "/api/admin/tmdb/import"} {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
				if w.Code != 403 {
					t.Fatalf("viewer write allowed: %s %d", path, w.Code)
				}
			}
		}
	}
	var jobs int64
	if err = db.Model(&model.TMDbRefreshJob{}).Count(&jobs).Error; err != nil || jobs != 0 {
		t.Fatalf("reads enqueued work: %d %v", jobs, err)
	}
	stored, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || stored.Payload == nil || !json.Valid([]byte(*stored.Payload)) || !strings.Contains(*stored.Payload, "unknown_source_field") || stored.LastAttemptAt == nil || stored.NextRetryAt == nil {
		t.Fatal("read projection changed stored payload")
	}
}
