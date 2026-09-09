package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func TestDoubanBindingRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{"", "user", "admin"} {
		for _, route := range []struct{ method, path string }{{"GET", "/api/metadata/current/douban/search"}, {"POST", "/api/metadata/current/douban/bind"}} {
			router := gin.New()
			group := router.Group("/api", func(c *gin.Context) { c.Set(middleware.CtxUserRole, role) })
			registerAuthedMediaRoutes(group, &service.Container{})
			response := httptest.NewRecorder()
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"douban_id":"1","media_type":"movie"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, req)
			want := http.StatusForbidden
			if role == "admin" {
				want = http.StatusBadRequest
			}
			if response.Code != want {
				t.Fatalf("%s %s status = %d want %d", role, route.path, response.Code, want)
			}
		}
	}
}
