package handler

import (
	"net/http"
	"net/http/httptest"
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

func TestDiscoverCatalogSupplementPermissionsAndMissingOnly(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	user := model.User{Username: "supplement-viewer", Role: "user"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	permission := model.UserPermission{UserID: user.ID, CanViewDiscover: false}
	if err = db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}, Secrets: config.SecretsConfig{JWTSecret: "test-secret", TMDbAPIKey: "test-key"}}
	svc := service.New(cfg, zap.NewNop(), repos)
	t.Cleanup(svc.Close)
	if err = repos.TMDb.SaveResponse(t.Context(), repository.TMDbSourceRequest{Kind: "movie", TMDbID: 9, Component: "summary", Language: "und", Origin: "identity"}, []byte(`{"id":9,"title":"摘要"}`), time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	row, err := repos.TMDb.SubjectIdentity(t.Context(), "movie", 9)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"anonymous", "denied", "viewer"} {
		if mode == "viewer" {
			if err = db.Model(&permission).Update("can_view_discover", true).Error; err != nil {
				t.Fatal(err)
			}
		}
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if mode != "anonymous" {
				c.Set(middleware.CtxUserID, user.ID)
				c.Set(middleware.CtxUserRole, "user")
			}
			c.Next()
		})
		registerAuthedUISurfaceRoutes(router.Group("/api"), svc)
		for _, request := range []struct{ path, body string }{{"/api/catalogs/douban/subjects/ensure", `{"douban_id":"100","kind":"movie","force":true,"title":"客户端假标题"}`}, {"/api/catalogs/tmdb/subjects/" + row.ID + "/ensure?force=1", `{}`}} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, request.path, strings.NewReader(request.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			want := 200
			if strings.Contains(request.path, "/tmdb/") {
				want = 202
			}
			if mode == "anonymous" {
				want = 401
			} else if mode == "denied" {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("%s %s status=%d body=%s", mode, request.path, w.Code, w.Body.String())
			}
			if mode == "viewer" {
				for _, private := range []string{`"jobs"`, `"attempts"`, `"requested_by"`, `"config_key"`, `"raw_json"`, "客户端假标题"} {
					if strings.Contains(w.Body.String(), private) {
						t.Fatalf("public ensure leaked %s", private)
					}
				}
			}
		}
		if mode == "viewer" {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/catalogs/douban/subjects/ensure", strings.NewReader(`{"douban_id":"100","kind":"episode"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			if w.Code != 400 {
				t.Fatalf("invalid identity accepted: %d", w.Code)
			}
			if err = repos.TMDb.SaveResponse(t.Context(), repository.TMDbSourceRequest{Kind: "movie", TMDbID: 9, Component: "detail", Language: "zh-CN"}, []byte(`{"id":9,"title":"完整资料","credits":{"cast":[],"crew":[]}}`), time.Now(), ""); err != nil {
				t.Fatal(err)
			}
			if err = db.Model(&model.TMDbRefreshJob{}).Where("subject_id = ?", row.ID).Updates(map[string]any{"status": "ready", "explicit_pending": false, "next_refresh_at": time.Now().Add(-time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/catalogs/tmdb/subjects/"+row.ID+"/ensure?force=1", nil))
			if w.Code != 200 {
				t.Fatalf("complete ensure %d", w.Code)
			}
			var pending int64
			if err = db.Model(&model.TMDbRefreshJob{}).Where("explicit_pending").Count(&pending).Error; err != nil || pending != 0 {
				t.Fatalf("viewer refreshed complete source %d %v", pending, err)
			}
		}
	}
	var local int64
	if err = db.Model(&model.MetadataItem{}).Count(&local).Error; err != nil || local != 0 {
		t.Fatalf("source ensure created local work %d %v", local, err)
	}
}
