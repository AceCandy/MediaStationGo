package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestEmbyMarkPlayedRefreshesPlaybackDevice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "media-1"},
		LibraryID: lib.ID,
		Title:     "Watched Movie",
		Path:      `/media/movies/Watched Movie.mkv`,
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo:   repos,
		Emby:   service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
		Device: service.NewDeviceService(zap.NewNop(), repos),
	})

	token := signedTestToken(t, secret)
	req := httptest.NewRequest(http.MethodPost, "/emby/Users/user-1/PlayedItems/media-1", nil)
	req.Header.Set("X-MediaBrowser-Authorization", `MediaBrowser Client="Infuse", Device="iPhone", DeviceId="played-device", Token="`+token+`"`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	devices, err := repos.UserDevice.ListByUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 || devices[0].LastPlayAt == nil {
		t.Fatalf("mark played should refresh playback device, got %#v", devices)
	}
	if devices[0].DeviceID != "played-device" || devices[0].DeviceName != "iPhone" || devices[0].Client != "Infuse" {
		t.Fatalf("playback device info not parsed: %#v", devices[0])
	}
}

func TestEmbyCompatSessionAllowsSameClientRequestsWithoutToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.JWTSecret = "test-secret"
	log := zap.NewNop()
	permissions := service.NewPermissionService(log, repos)
	auth := service.NewAuthService(cfg, log, repos, service.NewTokenService(cfg, log, repos), permissions)
	user, _, err := auth.Register(context.Background(), "viewer", "secret-pass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.Library.Create(t.Context(), &model.Library{Name: "Movies", Path: "D:\\media", Type: "movie", Enabled: true}); err != nil {
		t.Fatalf("create library: %v", err)
	}

	embyCompatSessions.Lock()
	embyCompatSessions.items = map[string]embyCompatSession{}
	embyCompatSessions.Unlock()

	router := gin.New()
	registerEmbyRoutes(router, cfg.Secrets.JWTSecret, &service.Container{
		Repo:  repos,
		Auth:  auth,
		Emby:  service.NewEmbyService(cfg, log, repos),
		Audit: service.NewAuditService(log, repos),
	})

	req := httptest.NewRequest(http.MethodPost, "/emby/Users/authenticatebyname", strings.NewReader(`{"Username":"viewer","Pw":"secret-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Emby Theater")
	req.Header.Set("X-Emby-Device-Id", "pc-device")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login status: %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/emby/Users/"+user.ID+"/Views", nil)
	req.Header.Set("User-Agent", "Emby Theater")
	req.Header.Set("X-Emby-Device-Id", "pc-device")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("views status: %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode views: %v", err)
	}
	if _, ok := payload["Items"]; !ok {
		t.Fatalf("missing Items: %#v", payload)
	}
}

func TestEmbyUppercaseSessionCapabilitiesRouteNoContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{Repo: repos})

	req := httptest.NewRequest(http.MethodPost, "/Sessions/Capabilities/Full", strings.NewReader(`{}`))
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
}

func TestEmbySessionCapabilitiesRouteAllowsPreAuthProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{})

	for _, path := range []string{"/Sessions/Capabilities", "/Sessions/Capabilities/Full", "/emby/Sessions/Capabilities", "/emby/Sessions/Capabilities/Full"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
