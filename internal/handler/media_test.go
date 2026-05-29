package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestListLibrariesHidesAdultDirectoriesUnlessAdminRequestsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	viewer := &model.User{Username: "viewer", PasswordHash: "hash", Role: "admin", HideAdult: true}
	if err := repos.User.Create(t.Context(), viewer); err != nil {
		t.Fatal(err)
	}
	safe := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	adult := model.Library{Name: "9KG", Path: "/media/9KG", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &safe); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &adult); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), service.AdultLibraryIDsSettingKey, `["`+adult.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	visible := requestLibraries(t, svc, viewer.ID, "admin", "/api/libraries")
	if len(visible) != 1 || visible[0].ID != safe.ID {
		t.Fatalf("watching library list should hide adult directories, got %#v", visible)
	}

	all := requestLibraries(t, svc, viewer.ID, "admin", "/api/libraries?include_hidden=1")
	if len(all) != 2 {
		t.Fatalf("admin include_hidden list should keep management access, got %#v", all)
	}
}

func requestLibraries(t *testing.T, svc *service.Container, userID, role, path string) []model.Library {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, userID)
	c.Set(middleware.CtxUserRole, role)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	listLibrariesHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, w.Code, w.Body.String())
	}
	var libs []model.Library
	if err := json.Unmarshal(w.Body.Bytes(), &libs); err != nil {
		t.Fatalf("decode libraries: %v", err)
	}
	return libs
}
