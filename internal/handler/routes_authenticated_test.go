package handler

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestUpdateLibraryRootRequestTracksEmptyName(t *testing.T) {
	var req updateLibraryRootReq
	if err := json.Unmarshal([]byte(`{"name":"","enabled":true}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Name == nil || *req.Name != "" {
		t.Fatalf("name = %#v, want explicit empty value", req.Name)
	}
}

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
		"GET /api/libraries/:id/cover",
		"PUT /api/libraries/:id/cover",
		"DELETE /api/libraries/:id/cover",
		"POST /api/libraries/:id/probe",
		"POST /api/libraries/:id/people-backfill",
		"GET /api/media",
		"GET /api/media/:id/versions",
		"POST /api/media/:id/probe/ensure",
		"GET /api/stream/:id",
		"GET /api/storage",
		"GET /api/search",
		"GET /api/search/advanced",
		"GET /api/search/tmdb",
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
		"GET /api/public/ui-config",
		"GET /api/downloads",
		"POST /api/downloads",
		"GET /api/subscriptions",
		"POST /api/subscriptions",
		"GET /api/download/tasks",
		"GET /api/sites",
		"GET /api/sites/types",
		"GET /api/sites/auth-types",
		"POST /api/sites",
		"GET /api/sites/:id",
		"PUT /api/sites/:id",
		"DELETE /api/sites/:id",
		"POST /api/sites/:id/test",
		"GET /api/sites/search",
		"GET /api/sites/:id/resource",
		"GET /api/sites/:id/userdata",
		"GET /api/search/sites",
		"POST /api/libraries/:id/scrape",
	} {
		if routes[forbidden] {
			t.Fatalf("retired route is still registered: %s", forbidden)
		}
	}
}
