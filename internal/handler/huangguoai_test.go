package handler

import (
	"encoding/json"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http/httptest"
	"testing"
)

func TestHuangGuoAIRouteRegistrationAndAdminBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserRole, c.GetHeader("Test-Role")); c.Next() })
	registerHuangGuoAIRoutes(r.Group("/api"), &service.Container{})
	for _, tt := range []struct {
		method, path, role string
		code               int
	}{
		{"GET", "/api/catalogs/huangguoai/works", "user", 503},
		{"GET", "/api/catalogs/huangguoai/downloads/works", "user", 403},
		{"GET", "/api/catalogs/huangguoai/downloads/works", "admin", 503},
		{"GET", "/api/catalogs/huangguoai/downloads/works/71/episodes", "admin", 503},
		{"POST", "/api/catalogs/huangguoai/downloads/works/71/cancel", "admin", 503},
		{"POST", "/api/catalogs/huangguoai/downloads/00000000-0000-0000-0000-000000000001/retry", "admin", 503},
	} {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.Header.Set("Test-Role", tt.role)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != tt.code {
			t.Fatalf("%s %s: got %d want %d", tt.method, tt.path, res.Code, tt.code)
		}
	}
}

func TestHuangGuoAIHTTPAdultAndProfileBoundary(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	user := model.User{Username: "synthetic-admin", Role: "admin"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&user).Update("hide_adult", false).Error; err != nil {
		t.Fatal(err)
	}
	profile := model.PlayProfile{UserID: user.ID, Name: "Synthetic", AllowAdult: false}
	if err = db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	catalog := service.NewHuangGuoAIService(repos, service.NewTaskTrackerService(nil, nil), nil, t.TempDir())
	defer catalog.Wait()
	svc := &service.Container{Repo: repos, HuangGuoAI: catalog, HuangGuoAIDownloads: service.NewHuangGuoAIDownloadService(repos, catalog, nil)}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserID, user.ID)
		c.Set(middleware.CtxUserRole, "admin")
		c.Next()
	})
	registerHuangGuoAIRoutes(r.Group("/api"), svc)
	request := func(method, path, profileID string, want int) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/catalogs/huangguoai"+path, nil)
		req.Header.Set("X-Play-Profile-ID", profileID)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s profile=%t: got %d want %d", method, path, profileID != "", res.Code, want)
		}
	}
	request("GET", "/works", "", 200)
	request("GET", "/downloads/works", "", 200)
	for _, path := range []string{"/works/71/media", "/works/71/media?page=2"} {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", "/api/catalogs/huangguoai"+path, nil))
		var result struct {
			Items json.RawMessage `json:"items"`
			Total int64           `json:"total"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != 200 || string(result.Items) != "[]" || result.Total != 0 {
			t.Fatalf("empty media page %s: status=%d body=%s err=%v", path, res.Code, res.Body.String(), err)
		}
	}
	if err = db.Create(&[]model.HuangGuoAIDownloadWork{{SourceID: "71", Title: "Special 100%_"}, {SourceID: "72", Title: "Other"}}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&[]model.HuangGuoAIDownload{{SourceID: "71", Episode: 1, Status: "failed"}, {SourceID: "71", Episode: 2, Status: "completed"}, {SourceID: "72", Episode: 1, Status: "cancelled"}}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		query string
		total int64
	}{{"keyword=special&status=failed", 1}, {"keyword=special&status=cancelled", 0}, {"keyword=%25", 1}, {"keyword=missing", 0}, {"keyword=72", 1}} {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", "/api/catalogs/huangguoai/downloads/works?"+tt.query, nil))
		var result struct {
			Items []service.HuangGuoAIDownloadWorkSummary `json:"items"`
			Total int64                                   `json:"total"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != 200 || result.Total != tt.total || int64(len(result.Items)) != tt.total {
			t.Fatalf("%s: status=%d body=%s err=%v", tt.query, res.Code, res.Body.String(), err)
		}
	}
	for _, path := range []string{"/works", "/works/71", "/works/71/media", "/works/71/state", "/artwork/synthetic", "/downloads/works", "/downloads/config", "/status"} {
		request("GET", path, profile.ID, 404)
	}
	request("POST", "/works/71/refresh", profile.ID, 404)
	request("POST", "/downloads/works/71", profile.ID, 404)
	if err = repos.Setting.Set(t.Context(), "adult.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	request("GET", "/works", "", 404)
	request("GET", "/downloads/works", "", 404)
}
