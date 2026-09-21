package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNextUpRoutesAndWebContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	create(&model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "admin", Tier: "plus", IsActive: true})
	create(&model.Library{Base: model.Base{ID: "library"}, Name: "Show", Path: "/test", Type: "tv"})
	seriesID, seasonID := "series", "season"
	create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: "series", Title: "Show", Source: "test"})
	create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: "season", ParentID: &seriesID, SeasonNum: 1, Title: "Season", Source: "test"})
	for i, id := range []string{"first", "next"} {
		create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: "episode", ParentID: &seasonID, EpisodeNum: i + 1, Title: id, Source: "test"})
		create(&model.Media{PermanentBase: model.PermanentBase{ID: id + "-file"}, LibraryID: "library", MetadataID: id, Path: "/test/" + id})
	}
	create(&model.PlaybackHistory{UserID: "user-1", MetadataID: "first", MediaID: "first-file", Completed: true, WatchedAt: time.Now()})
	repos := repository.New(db)
	svc := &service.Container{Repo: repos, Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos), Playback: service.NewPlaybackService(zap.NewNop(), repos)}
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", svc)
	token := signedTestToken(t, "test-secret")
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range []string{"/Shows/NextUp", "/Users/user-1/Shows/NextUp", "/shows/nextup", "/users/user-1/shows/nextup", "/Items/Resume", "/Users/user-1/Items/Resume", "/items/resume", "/users/user-1/items/resume"} {
			request := httptest.NewRequest(http.MethodGet, prefix+path+"?SeriesId=series&Limit=1&Fields=MediaSources", nil)
			request.Header.Set("X-Emby-Token", token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var result struct {
				Items []struct {
					ID string `json:"Id"`
				}
				TotalRecordCount int
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].ID != "next" {
				t.Fatalf("%s%s: status=%d body=%s err=%v", prefix, path, response.Code, response.Body.String(), err)
			}
		}
	}
	web := gin.New()
	web.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserID, "user-1")
		c.Set(middleware.CtxUserRole, "admin")
		c.Next()
	})
	web.GET("/continue", historyContinueHandler(svc))
	response := httptest.NewRecorder()
	web.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/continue", nil))
	var result []struct {
		History struct {
			IsNext   bool  `json:"is_next"`
			Position int64 `json:"position_ms"`
		}
		Media struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || len(result) != 1 || !result[0].History.IsNext || result[0].History.Position != 0 || result[0].Media.ID != "next-file" {
		t.Fatalf("web contract: status=%d body=%s err=%v", response.Code, response.Body.String(), err)
	}
	// 电影断点和下一集须统一排序后分页，而不是各取一页再拼接。
	create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: "movie"}, Kind: "movie", Title: "Movie", Source: "test"})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "movie-file"}, LibraryID: "library", MetadataID: "movie", Path: "/test/movie"})
	create(&model.PlaybackHistory{UserID: "user-1", MetadataID: "movie", MediaID: "movie-file", PositionMs: 30000, DurationMs: 120000, WatchedAt: time.Now().Add(time.Minute)})
	for index, want := range []string{"movie", "next", ""} {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emby/Users/user-1/Items/Resume?Limit=1&StartIndex=%d", index), nil)
		request.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var page struct {
			Items []struct {
				ID string `json:"Id"`
			}
			TotalRecordCount, StartIndex int
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || response.Code != 200 || page.TotalRecordCount != 2 || page.StartIndex != index {
			t.Fatalf("mixed page %d: status=%d body=%s err=%v", index, response.Code, response.Body.String(), err)
		}
		if (want == "" && len(page.Items) != 0) || (want != "" && (len(page.Items) != 1 || page.Items[0].ID != want)) {
			t.Fatalf("mixed page %d: %+v want %s", index, page, want)
		}
	}
}
