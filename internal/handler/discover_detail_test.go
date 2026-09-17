package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestDiscoverDetailHTTPPermissionAndValidation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserPermission{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "discover-viewer", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	permission := model.UserPermission{UserID: user.ID, CanViewDiscover: false}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := &service.Container{Repo: repos, Permissions: service.NewPermissionService(zap.NewNop(), repos)}
	for _, mode := range []string{"anonymous", "denied", "allowed"} {
		if mode == "allowed" {
			if err := db.Model(&permission).Update("can_view_discover", true).Error; err != nil {
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
		for _, request := range []struct{ method, path, body string }{
			{"POST", "/api/discover/search", `{"query":"test","kind":"person","page":1}`},
			{"GET", "/api/discover/tmdb/season/123", ""},
			{"GET", "/api/discover/tmdb/movie/invalid", ""},
			{"POST", "/api/discover/library-status", `{"items":[]}`},
			{"POST", "/api/discover/library-status", `{"items":[{"tmdb_id":123,"media_type":"episode"}]}`},
		} {
			req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			want := 400
			if mode == "anonymous" {
				want = 401
			}
			if mode == "denied" {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("%s %s: got %d want %d", mode, request.path, w.Code, want)
			}
		}
	}
}
