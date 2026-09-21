package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
)

func TestEmbyTargetUserRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		role       string
		target     string
		wantStatus int
	}{
		{name: "self", role: "user", target: "user-1", wantStatus: http.StatusNoContent},
		{name: "other user", role: "user", target: "user-2", wantStatus: http.StatusForbidden},
		{name: "admin target", role: "admin", target: "user-2", wantStatus: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(middleware.CtxUserID, "user-1")
				c.Set(middleware.CtxUserRole, tt.role)
				c.Next()
			}, embyTargetUserRequired())
			router.GET("/Users/:userId/Items", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			router.GET("/Users/:userId/Shows/NextUp", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			router.GET("/Shows/NextUp", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/Users/"+tt.target+"/Items", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			for _, path := range []string{"/Users/" + tt.target + "/Shows/NextUp", "/Shows/NextUp?UserId=" + tt.target} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != tt.wantStatus {
					t.Fatalf("%s status=%d want=%d", path, response.Code, tt.wantStatus)
				}
			}
		})
	}
}
