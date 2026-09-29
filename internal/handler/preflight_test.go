package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestAuthenticatedVisibilityReusesUserRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	user := model.User{Username: "viewer", PasswordHash: "unused"}
	if err := repo.User.Create(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	reads := 0
	if err := db.Callback().Query().Before("gorm:query").Register("test:count-user-read", func(q *gorm.DB) {
		if q.Statement.Table == "users" {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repo}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserID, user.ID)
		c.Set(middleware.CtxTokenVersion, user.TokenVersion)
	})
	visibility := func(c *gin.Context) {
		if !service.UserHidesAdult(c.Request.Context(), repo, user.ID) || mediaVisibilityForRequest(c, svc).IncludeNSFW {
			c.Status(http.StatusForbidden)
			return
		}
		c.Status(http.StatusOK)
	}
	router.GET("/web", activeUserRequired(svc), visibility)
	router.GET("/emby", activeEmbyUserRequired(svc), visibility)
	for _, path := range []string{"/web", "/emby"} {
		reads = 0
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || reads != 1 {
			t.Fatalf("%s: status=%d user reads=%d", path, response.Code, reads)
		}
	}
}
