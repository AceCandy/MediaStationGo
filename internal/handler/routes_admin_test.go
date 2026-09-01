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
	paths := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
		paths[route.Path] = true
	}

	for _, want := range []string{
		"GET /api/admin/users",
		"GET /api/admin/users/:id/permissions",
		"POST /api/admin/system/scheduler/:name/trigger",
		"GET /api/admin/notify/channels",
		"GET /api/admin/telegram/webhook",
		"GET /api/admin/organize/sources",
		"GET /api/admin/api-configs",
		"GET /api/admin/api-proxy-pool",
		"PUT /api/admin/api-proxy-pool",
		"POST /api/admin/api-proxy-pool/check",
		"POST /api/admin/api-proxy-pool/cleanup",
		"POST /api/admin/scheduler/:name/run",
		"POST /api/media/:id/douban-enrichment",
		"GET /api/tasks/definitions/:key/executions",
		"POST /api/tasks/definitions/:key/run",
		"PUT /api/tasks/definitions/:key/schedule",
	} {
		if !routes[want] {
			t.Fatalf("%s route is not registered", want)
		}
	}
	for _, retired := range []string{
		"/api/admin/download/clients",
		"/api/admin/storage/status",
		"/api/admin/storage/:type",
		"/api/admin/storage/:type/test",
		"/api/admin/storage/:type/logout",
		"/api/admin/storage/:type/upload-local",
		"/api/admin/cloud/:type/list",
		"/api/admin/cloud/:type/mkdir",
		"/api/admin/cloud/:type/rename",
		"/api/admin/cloud/:type/import",
		"/api/admin/cloud/:type/mount",
		"/api/admin/cloud/:type/qr/start",
		"/api/admin/cloud/:type/qr/poll",
		"/api/admin/cloud/scan-all",
		"/api/admin/cloud/scan/cancel",
		"/api/admin/cloud/scan/status",
		"/api/sites",
		"/api/sites/types",
		"/api/sites/auth-types",
		"/api/sites/:id",
		"/api/sites/:id/test",
		"/api/sites/:id/resource",
		"/api/sites/:id/userdata",
		"/api/sites/search",
		"/api/search/sites",
		"/api/cloud/play/:type",
		"/api/img/cloud/:type",
		"/api/admin/media/repair-rescrape",
		"/api/playback/transcode/:job_id/status",
		"/api/hls/:id",
		"/api/hls/:id/index.m3u8",
		"/api/hls/:id/:seg",
		"/api/hls/:id/master.m3u8",
	} {
		if paths[retired] {
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
			retired := prefix + path
			if paths[retired] {
				t.Fatalf("retired route is still registered: %s", retired)
			}
		}
		for _, path := range []string{"/Videos/:id/:seg", "/videos/:id/:seg"} {
			retired := prefix + path
			if paths[retired] {
				t.Fatalf("retired route is still registered: %s", retired)
			}
		}
	}
}
