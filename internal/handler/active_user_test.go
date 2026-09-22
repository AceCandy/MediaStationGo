package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestRefreshDoesNotOverwriteBrowserCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.RefreshToken{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: "test-secret"}}
	tokens := service.NewTokenService(cfg, zap.NewNop(), repos)
	auth := service.NewAuthService(cfg, zap.NewNop(), repos, tokens, nil)
	user := &model.User{Username: "viewer", PasswordHash: "unused", Role: "user"}
	if err := repos.User.Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	pair, err := tokens.IssuePair(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/refresh", NewRefreshHandler(&service.Container{Auth: auth}, zap.NewNop()).RefreshToken)
	req := httptest.NewRequest(http.MethodPost, "/refresh", strings.NewReader(`{"refresh_token":"`+pair.RefreshToken+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("refresh replaced browser session cookie")
	}
}

func TestAuthenticatedRoutesUseCurrentAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, change := range []string{"disabled", "expired", "demoted", "deleted", "password"} {
		t.Run(change, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.User{}, &model.RefreshToken{}); err != nil {
				t.Fatal(err)
			}
			repos := repository.New(db)
			user := &model.User{Username: "admin", PasswordHash: "old", Role: "admin", Tier: "plus"}
			if err := repos.User.Create(t.Context(), user); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{}
			cfg.Secrets.JWTSecret = "test-secret"
			auth := service.NewAuthService(cfg, zap.NewNop(), repos, nil, nil)
			token, err := auth.IssueEmbyToken(user)
			if err != nil {
				t.Fatal(err)
			}
			svc := &service.Container{Log: zap.NewNop(), Repo: repos, Scheduler: service.NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)}
			router := gin.New()
			registerAdminRoutes(router.Group("/api"), cfg, svc)
			registerAPIConfigRoutes(router.Group("/api"), cfg, svc)
			identity := func(c *gin.Context) { c.String(http.StatusOK, middleware.GetUserRole(c)+":"+middleware.GetUserTier(c)) }
			router.GET("/web", middleware.AuthRequired(cfg.Secrets.JWTSecret), activeUserRequired(svc), identity)
			router.GET("/emby", middleware.EmbyAuthRequired(cfg.Secrets.JWTSecret), activeEmbyUserRequired(svc), identity)
			request := func(path, token string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			if w := request("/api/admin/scheduler", token); w.Code != http.StatusOK {
				t.Fatalf("initial access: %d", w.Code)
			}
			ctx := context.Background()
			switch change {
			case "disabled":
				err = repos.User.UpdateFields(ctx, user.ID, map[string]any{"is_active": false})
			case "expired":
				err = repos.User.UpdateFields(ctx, user.ID, map[string]any{"expired_at": time.Now().Add(-time.Hour)})
			case "demoted":
				err = repos.User.UpdateFields(ctx, user.ID, map[string]any{"role": "user", "tier": "free"})
			case "deleted":
				err = db.Delete(user).Error
			case "password":
				err = repos.User.UpdatePassword(ctx, user.ID, "new")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/api/admin/scheduler", "/api/api-config/providers/list", "/web", "/emby"} {
				w := request(path, token)
				if change == "demoted" && (path == "/web" || path == "/emby") {
					if w.Code != http.StatusOK || w.Body.String() != "user:free" {
						t.Fatalf("stale identity at %s: %d %s", path, w.Code, w.Body.String())
					}
				} else if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
					t.Fatalf("%s accepted at %s: %d", change, path, w.Code)
				}
			}
			if change == "password" {
				current, err := repos.User.FindByID(ctx, user.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := auth.IssueEmbyToken(current)
				if err != nil {
					t.Fatal(err)
				}
				if w := request("/emby", fresh); w.Code != http.StatusOK {
					t.Fatalf("new token: %d", w.Code)
				}
			}
		})
	}
}
