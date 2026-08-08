package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestAuthenticatedRouteSurfacesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	Register(router, &config.Config{
		Secrets: config.SecretsConfig{JWTSecret: "test-secret"},
	}, zap.NewNop(), &service.Container{Log: zap.NewNop()})

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, want := range []string{
		"GET /api/me",
		"GET /api/auth/permissions",
		"GET /api/libraries",
		"POST /api/libraries/:id/probe",
		"GET /api/media",
		"GET /api/stream/:id",
		"GET /api/storage",
		"GET /api/sites/search",
		"GET /api/watch-history",
		"GET /api/discover/feed",
		"GET /api/playback/:id/info",
		"GET /api/admin/assistant/history",
	} {
		if !routes[want] {
			t.Fatalf("%s route is not registered", want)
		}
	}
	for _, forbidden := range []string{
		"GET /api/downloads",
		"POST /api/downloads",
		"GET /api/subscriptions",
		"POST /api/subscriptions",
		"GET /api/download/tasks",
	} {
		if routes[forbidden] {
			t.Fatalf("retired route is still registered: %s", forbidden)
		}
	}
}
