package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func TestTaskStartupGuardsAndLightweightStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 无数据库与执行器，状态查询仍能响应，写请求必须在解析/执行前被拦截。
	svc := &service.Container{Startup: service.NewStartupState()}
	router := gin.New()
	router.GET("/tasks/startup", taskStartupHandler(svc))
	router.POST("/tasks/definitions/:key/run", taskDefinitionRunHandler(svc))
	router.PUT("/tasks/definitions/:key/schedule", taskDefinitionScheduleHandler(svc))
	router.POST("/scheduler/:name/run", schedulerRunHandler(svc))
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/tasks/startup", 200},
		{"POST", "/tasks/definitions/" + service.TaskDefinitionOrganize + "/run", 503},
		{"POST", "/tasks/definitions/library_scan/run", 503},
		{"POST", "/tasks/definitions/media_scrape/run", 503},
		{"POST", "/tasks/definitions/probe_backfill/run", 503},
		{"POST", "/tasks/definitions/tmdb_snapshot_backfill/run", 503},
		{"POST", "/tasks/definitions/hongguo_download_supplement/run", 503},
		{"POST", "/scheduler/organize_source/run", 503},
		{"PUT", "/tasks/definitions/" + service.TaskDefinitionOrganize + "/schedule", 503},
		{"POST", "/tasks/definitions/unknown/run", 404},
		{"PUT", "/tasks/definitions/unknown/schedule", 404},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			r := httptest.NewRecorder()
			router.ServeHTTP(r, httptest.NewRequest(route.method, route.path, strings.NewReader("{}")))
			if r.Code != route.status {
				t.Fatalf("status %d: %s", r.Code, r.Body.String())
			}
			if route.status == 503 && !strings.Contains(r.Body.String(), "startup_not_ready") {
				t.Fatal(r.Body.String())
			}
			if route.method == http.MethodGet {
				var state service.StartupStatus
				if err := json.Unmarshal(r.Body.Bytes(), &state); err != nil || state.State != "starting" || r.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status: %+v %v", state, err)
				}
			}
		})
	}
}
