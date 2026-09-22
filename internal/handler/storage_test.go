package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestStorageRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	Register(router, &config.Config{
		Secrets: config.SecretsConfig{JWTSecret: "test-secret"},
	}, zap.NewNop(), &service.Container{Log: zap.NewNop()})

	for _, route := range router.Routes() {
		if route.Method == "GET" && route.Path == "/api/storage" {
			return
		}
	}
	t.Fatal("GET /api/storage route is not registered")
}

func TestSTRMDeleteRoutesAreAdminOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "user"}).Error; err != nil {
		t.Fatal(err)
	}
	const secret = "strm-delete-secret"
	router := gin.New()
	Register(router, &config.Config{Secrets: config.SecretsConfig{JWTSecret: secret}}, zap.NewNop(), &service.Container{Log: zap.NewNop(), Repo: repository.New(db)})

	found := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Path == "/api/admin/media/:id/strm-delete-target" {
			found[route.Method] = true
		}
	}
	if !found[http.MethodGet] || !found[http.MethodDelete] {
		t.Fatalf("STRM delete routes = %#v", found)
	}

	for _, tt := range []struct {
		name   string
		method string
		token  string
		want   int
	}{
		{name: "authentication required", method: http.MethodGet, want: http.StatusUnauthorized},
		{name: "admin required", method: http.MethodDelete, token: signedProbeRoleToken(t, secret, "user"), want: http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/admin/media/media-1/strm-delete-target", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d body=%s, want %d", w.Code, w.Body.String(), tt.want)
			}
		})
	}
}
