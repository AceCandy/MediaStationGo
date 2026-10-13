package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestTMDbCenterPublicProjectionAndReadOnlyDetail(t *testing.T) {
	db, e := testdb.OpenPostgres(t, &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(model.AllModels()...); e != nil {
		t.Fatal(e)
	}
	repo := repository.New(db)
	raw := []byte(`{"id":8,"title":"来源作品","unknown_source_field":"preserved","credits":{"cast":[],"crew":[]}}`)
	if e = repo.TMDb.SaveResponse(context.Background(), repository.TMDbSourceRequest{Kind: "movie", TMDbID: 8, Component: "detail", Language: "zh-CN"}, raw, time.Now(), ""); e != nil {
		t.Fatal(e)
	}
	sub, e := repo.TMDb.SubjectIdentity(context.Background(), "movie", 8)
	if e != nil {
		t.Fatal(e)
	}
	svc := &service.Container{Repo: repo, TMDbCenter: service.NewTMDbDataCenter(repo, service.NewTMDbProvider(&config.Config{}, zap.NewNop(), nil))}
	job := model.TMDbRefreshJob{Key: "public-projection-test", SubjectID: &sub.ID, Status: "pending", Scope: "detail", Attempts: 7, LastError: "internal diagnostic"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/public/:id", tmdbCenterDetail(svc, false, false))
	router.GET("/admin/:id", tmdbCenterDetail(svc, false, true))
	for _, prefix := range []string{"public", "admin"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/"+prefix+"/"+sub.ID, nil))
		if w.Code != 200 {
			t.Fatalf("%s status %d: %s", prefix, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "unknown_source_field") != (prefix == "admin") {
			t.Fatalf("raw JSON boundary %s", prefix)
		}
		if prefix == "public" {
			for _, field := range []string{`"jobs"`, `"attempts"`, "internal diagnostic", `"next_refresh_at"`, `"next_attempt_at"`} {
				if strings.Contains(w.Body.String(), field) {
					t.Fatalf("public leaked %s", field)
				}
			}
			if !strings.Contains(w.Body.String(), `"refresh_pending":true`) {
				t.Fatal("pending projection missing")
			}
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("unsafe source cache headers")
		}
	}
	var n int64
	if e = db.Model(&model.TMDbRefreshJob{}).Count(&n).Error; e != nil || n != 1 {
		t.Fatalf("GET enqueued work %d %v", n, e)
	}
}
