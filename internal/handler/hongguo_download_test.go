package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

func TestHongGuoDownloadConfigHTTP(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Library{}, &model.LibraryRoot{}); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	group := engine.Group("/api", func(c *gin.Context) { c.Set(middleware.CtxUserRole, "admin"); c.Next() })
	registerHongGuoDownloadRoutes(group, &service.Container{HongGuoDownloads: service.NewHongGuoDownloadService(repository.New(db), nil, nil)})
	root := t.TempDir()
	for _, tt := range []struct {
		fields string
		status int
	}{
		{`,"concurrency":2,"verification_concurrency":3,"hardware_verification":true,"priority":"app"`, 200},
		{`,"concurrency":2,"verification_concurrency":3,"hardware_verification":true,"priority":"fallback"`, 200},
		{"", 200},
		{`,"concurrency":0`, 400},
		{`,"concurrency":6`, 400},
		{`,"concurrency":1.5`, 400},
		{`,"verification_concurrency":0`, 400},
		{`,"verification_concurrency":6`, 400},
		{`,"verification_concurrency":1.5`, 400},
		{`,"hardware_verification":"true"`, 400},
		{`,"hardware_verification":1`, 400},
		{`,"priority":"arbitrary"`, 400},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/catalogs/hongguo/downloads/config", strings.NewReader(fmt.Sprintf(`{"root":%q%s}`, root, tt.fields)))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		if w.Code != tt.status {
			t.Fatalf("fields=%s status=%d body=%s", tt.fields, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var cfg service.HongGuoDownloadConfig
			priority := "fallback"
			if strings.Contains(tt.fields, `"app"`) {
				priority = "app"
			}
			if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil || cfg.Concurrency != 2 || cfg.VerificationConcurrency != 3 || !cfg.HardwareVerification || cfg.Priority != priority {
				t.Fatalf("round-trip: %+v %v", cfg, err)
			}
		}
	}
}

func TestHongGuoDownloadWorksFailedFilterHTTP(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.HongGuoDownload{}); err != nil {
		t.Fatal(err)
	}
	rows := []model.HongGuoDownload{{SourceID: "123", Episode: 1, Status: "failed"}, {SourceID: "123", Episode: 2, Status: "completed"}, {SourceID: "456", Episode: 1, Status: "cancelled"}}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	group := engine.Group("/api", func(c *gin.Context) { c.Set(middleware.CtxUserRole, "admin"); c.Next() })
	registerHongGuoDownloadRoutes(group, &service.Container{HongGuoDownloads: service.NewHongGuoDownloadService(repository.New(db), nil, nil)})
	for _, tt := range []struct {
		query  string
		status int
		total  int64
	}{{"", 200, 2}, {"?failed_only=false", 200, 2}, {"?failed_only=true", 200, 1}, {"?failed_only=invalid", 400, 0}} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/catalogs/hongguo/downloads/works"+tt.query, nil))
		if w.Code != tt.status {
			t.Fatalf("%s: %d %s", tt.query, w.Code, w.Body.String())
		}
		if w.Code != 200 {
			continue
		}
		var result struct {
			Items []service.HongGuoDownloadSummary `json:"items"`
			Total int64                            `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != tt.total || int64(len(result.Items)) != tt.total {
			t.Fatalf("%s: %+v", tt.query, result)
		}
		if tt.total == 1 && (result.Items[0].Total != 2 || result.Items[0].Completed != 1 || result.Items[0].Failed != 1) {
			t.Fatalf("incomplete summary: %+v", result)
		}
	}
}

func TestHongGuoDownloadHTTPAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api", middleware.AuthRequired("test-download-secret"))
	registerHongGuoDownloadRoutes(group, &service.Container{})
	for _, tt := range []struct {
		role   string
		status int
	}{{"", 401}, {"user", 403}, {"admin", 503}} {
		for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/config"}, {http.MethodPut, "/config"}, {http.MethodGet, "/works"}, {http.MethodGet, "/works/123/episodes"}, {http.MethodPost, "/works/123/retry"}, {http.MethodPost, "/supplement"}} {
			method := endpoint.method
			req := httptest.NewRequest(method, "/api/catalogs/hongguo/downloads"+endpoint.path, nil)
			if tt.role != "" {
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{UserID: "test-user", Role: tt.role, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
				value, err := token.SignedString([]byte("test-download-secret"))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+value)
			}
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("%s %s got %d", tt.role, method, w.Code)
			}
		}
	}
}

func TestHongGuoDownloadSupplementHTTPValidation(t *testing.T) {
	engine := gin.New()
	group := engine.Group("/api", func(c *gin.Context) { c.Set(middleware.CtxUserRole, "admin"); c.Next() })
	registerHongGuoDownloadRoutes(group, &service.Container{HongGuoDownloads: service.NewHongGuoDownloadService(nil, nil, nil)})
	for _, body := range []string{`{}`, `{"count":0}`, `{"count":101}`, `{"count":1.5}`, `{"count":"2"}`, `{"count":-1}`, strings.Repeat(" ", 4096) + `{"count":1}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/catalogs/hongguo/downloads/supplement", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
