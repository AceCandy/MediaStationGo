package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestRefreshMetadataTMDbRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{"user", "admin"} {
		t.Run(role, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api", func(c *gin.Context) { c.Set(middleware.CtxUserRole, role) })
			registerAuthedMediaRoutes(group, &service.Container{})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/metadata/current/tmdb/refresh", nil))
			want := http.StatusForbidden
			if role == "admin" {
				want = http.StatusBadGateway
			}
			if response.Code != want {
				t.Fatalf("status = %d, want %d", response.Code, want)
			}
		})
	}
}
