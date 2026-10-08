package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		{"GET", "/api/catalogs/huangguoai/downloads/00000000-0000-0000-0000-000000000001/preview", "user", 403},
		{"POST", "/api/catalogs/huangguoai/downloads/00000000-0000-0000-0000-000000000001/confirm", "user", 403},
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
	if err = db.Create(&[]model.HuangGuoAIWork{{SourceID: "71", Kind: "series", SourceCategory: "ai-duanju", Title: "Series"}, {SourceID: "72", Kind: "movie", SourceCategory: "ai-mogai", Title: "Movie"}}).Error; err != nil {
		t.Fatal(err)
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
		for _, item := range result.Items {
			want := "series"
			if item.SourceID == "72" {
				want = "movie"
			}
			if item.Kind != want {
				t.Fatalf("wrong JSON kind: %+v", item)
			}
		}
	}
	root := t.TempDir()
	content := []byte("synthetic-review-content")
	digest := sha256.Sum256(content)
	review := model.HuangGuoAIDownload{SourceID: "73", Episode: 1, Root: root, Status: "pending_review", RawSize: int64(len(content)), VerifiedSize: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), ReviewToken: uuid.NewString(), Warning: "Synthetic warning"}
	if err := db.Create(&review).Error; err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join("downloading", review.ID+"-"+uuid.NewString(), "ready.mp4")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, stage)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, stage), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&review).Update("staging_path", stage).Error; err != nil {
		t.Fatal(err)
	}
	previewPath := "/downloads/" + review.ID + "/preview?review_token=" + review.ReviewToken
	req := httptest.NewRequest("GET", "/api/catalogs/huangguoai"+previewPath, nil)
	req.Header.Set("Range", "bytes=0-8")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != 206 || res.Body.String() != string(content[:9]) || res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("candidate Range: status=%d", res.Code)
	}
	request("GET", previewPath, profile.ID, 404)
	locked := model.PlayProfile{UserID: user.ID, Name: "Locked", AllowAdult: true, RequirePIN: true}
	if err := db.Create(&locked).Error; err != nil {
		t.Fatal(err)
	}
	request("GET", previewPath, locked.ID, 404)
	request("POST", "/downloads/"+review.ID+"/confirm", locked.ID, 404)
	request("POST", "/downloads/"+review.ID+"/confirm", profile.ID, 404)
	request("GET", "/downloads/"+review.ID+"/preview?review_token=stale", "", 404)
	confirmReq := httptest.NewRequest("POST", "/api/catalogs/huangguoai/downloads/"+review.ID+"/confirm", strings.NewReader(`{"review_token":"`+review.ReviewToken+`"}`))
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmRes := httptest.NewRecorder()
	r.ServeHTTP(confirmRes, confirmReq)
	if confirmRes.Code != 204 {
		t.Fatalf("confirm endpoint status=%d", confirmRes.Code)
	}
	if err := db.First(&review, "id=?", review.ID).Error; err != nil || review.Status != "waiting_verify" || review.ConfirmedBy != user.ID || review.Warning == "" {
		t.Fatal("HTTP confirmation audit", err)
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
	request("GET", previewPath, "", 404)
	request("POST", "/downloads/"+review.ID+"/confirm", "", 404)
}
