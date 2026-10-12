package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestDoubanManagementAdminAccessAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: "test-secret"}}
	auth := service.NewAuthService(cfg, zap.NewNop(), repos, nil, nil)
	tokens := map[string]string{}
	for _, role := range []string{"admin", "user"} {
		user := &model.User{Username: role, Role: role, PasswordHash: "unused"}
		if err := repos.User.Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
		tokens[role], err = auth.IssueEmbyToken(user)
		if err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	svc := &service.Container{Repo: repos, Log: zap.NewNop()}
	registerAdminRoutes(router.Group("/api"), cfg, svc)
	request := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/admin/douban/subjects"+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	id := uuid.NewString()
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, ""}, {http.MethodGet, "/" + id}, {http.MethodPost, "/" + id + "/refresh"}, {http.MethodPost, "/" + id + "/refresh?mode=base"},
		{http.MethodGet, "/" + id + "/poster"}, {http.MethodHead, "/" + id + "/poster"},
		{http.MethodGet, "/" + id + "/credits/0/image"}, {http.MethodHead, "/" + id + "/credits/0/image"},
	} {
		for _, access := range []struct {
			role string
			code int
		}{{"", http.StatusUnauthorized}, {"user", http.StatusForbidden}, {"admin", http.StatusServiceUnavailable}} {
			response := request(endpoint.method, endpoint.path, tokens[access.role])
			if response.Code != access.code {
				t.Fatalf("%s %s role=%q: %d %s", endpoint.method, endpoint.path, access.role, response.Code, response.Body.String())
			}
		}
	}
	for _, query := range []string{"page=0", "page=-1", "page=1000001", "page=x", "page_size=0", "page_size=101", "type=tv", "type=season", "type=episode", "keyword=" + url.QueryEscape(strings.Repeat("中", 201))} {
		response := request(http.MethodGet, "?"+query, tokens["admin"])
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid query %q: %d %s", query, response.Code, response.Body.String())
		}
	}
	for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/not-a-uuid"}, {http.MethodPost, "/not-a-uuid/refresh"}, {http.MethodGet, "/not-a-uuid/poster"}, {http.MethodHead, "/not-a-uuid/poster"}, {http.MethodGet, "/not-a-uuid/credits/0/image"}, {http.MethodGet, "/" + id + "/credits/-1/image"}} {
		response := request(endpoint.method, endpoint.path, tokens["admin"])
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid row id: %d %s", response.Code, response.Body.String())
		}
	}
	svc.Douban = service.NewDoubanProvider(nil)
	for _, query := range []string{"mode=invalid", "mode=", "mode=base&mode=full"} {
		response := request(http.MethodPost, "/"+id+"/refresh?"+query, tokens["admin"])
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid refresh mode %s: %d", query, response.Code)
		}
	}

}
