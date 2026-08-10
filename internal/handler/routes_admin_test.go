package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestAdminRouteSurfacesAreRegistered(t *testing.T) {
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
		"GET /api/admin/users",
		"GET /api/admin/users/:id/permissions",
		"POST /api/admin/system/scheduler/:name/trigger",
		"GET /api/admin/notify/channels",
		"GET /api/admin/telegram/webhook",
		"GET /api/admin/organize/sources",
		"GET /api/admin/api-configs",
		"POST /api/admin/scheduler/:name/run",
	} {
		if !routes[want] {
			t.Fatalf("%s route is not registered", want)
		}
	}
	for _, retired := range []string{
		"GET /api/admin/download/clients",
		"GET /api/admin/storage/status",
		"GET /api/admin/cloud/:type/list",
		"POST /api/admin/cloud/scan-all",
		"POST /api/admin/media/repair-rescrape",
		"GET /api/playback/transcode/:job_id/status",
		"GET /api/hls/:id/master.m3u8",
	} {
		if routes[retired] {
			t.Fatalf("retired route is still registered: %s", retired)
		}
	}
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range []string{
			"/Videos/:id/master.m3u8",
			"/Videos/:id/main.m3u8",
			"/videos/:id/master.m3u8",
			"/videos/:id/main.m3u8",
		} {
			for _, method := range []string{"GET", "HEAD"} {
				retired := method + " " + prefix + path
				if routes[retired] {
					t.Fatalf("retired route is still registered: %s", retired)
				}
			}
		}
		for _, path := range []string{"/Videos/:id/:seg", "/videos/:id/:seg"} {
			retired := "GET " + prefix + path
			if routes[retired] {
				t.Fatalf("retired route is still registered: %s", retired)
			}
		}
	}
}
