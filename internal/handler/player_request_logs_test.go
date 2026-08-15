package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestPlayerRequestLogsHandlerRejectsInvalidFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &service.Container{PlayerLogs: service.NewPlayerRequestLogService(nil, nil)}
	for _, path := range []string{
		"/logs?month=2026-13",
		"/logs?page=0",
		"/logs?page_size=101",
		"/logs?method=TRACE",
		"/logs?status=99",
	} {
		router := gin.New()
		router.GET("/logs", playerRequestLogsHandler(svc))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
