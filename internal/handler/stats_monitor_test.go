package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestStatsMonitorDoesNotRequireDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 不提供 Stats 或 Repo，防止监控接口重新依赖媒体统计。
	router.GET("/monitor", statsMonitorHandler(&service.Container{Cfg: &config.Config{}}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/monitor", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var hardware service.Hardware
	if err := json.Unmarshal(w.Body.Bytes(), &hardware); err != nil {
		t.Fatal(err)
	}
	if hardware.GoVersion == "" || hardware.Goroutines == 0 {
		t.Fatalf("hardware = %#v", hardware)
	}
}
