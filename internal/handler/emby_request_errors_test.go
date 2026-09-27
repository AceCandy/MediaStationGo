package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEmbyShowsRejectEmptyParent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/Shows//Seasons", "/Shows/%20/Seasons", "/Shows//Episodes", "/Shows/%20/Episodes?SeasonId=%20"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(gin.RecoveryWithWriter(io.Discard))
			// 无服务对象，确保缺少父级的请求在查询前被拒绝。
			router.GET("/Shows/:id/Seasons", embyShowSeasonsHandler(nil))
			router.GET("/Shows/:id/Episodes", embyShowEpisodesHandler(nil))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestEmbyBrowseRequestErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))}
	for _, tc := range []struct {
		path, url string
		handler   gin.HandlerFunc
	}{
		{"/Items", "/Items", embyItemsHandler(svc)},
		{"/Persons", "/Persons", embyPersonsHandler(svc)},
		{"/Search/Hints", "/Search/Hints?SearchTerm=movie", embySearchHintsHandler(svc)},
		{"/Items/Counts", "/Items/Counts", embyItemsCountsHandler(svc)},
		{"/Items/:id", "/Items/item", embyItemByIDHandler(svc)},
		{"/Items/Resume", "/Items/Resume", embyResumeItemsHandler(svc)},
		{"/Items/Latest", "/Items/Latest", embyLatestItemsHandler(svc)},
		{"/Shows/:id/Seasons", "/Shows/series/Seasons", embyShowSeasonsHandler(svc)},
		{"/Shows/NextUp", "/Shows/NextUp", embyNextUpHandler(svc)},
		{"/Shows/:id/Episodes", "/Shows/series/Episodes", embyShowEpisodesHandler(svc)},
		{"/Shows/:id/Episodes", "/Shows//Episodes?SeasonId=season", embyShowEpisodesHandler(svc)},
	} {
		for _, canceled := range []bool{false, true} {
			t.Run(tc.url+map[bool]string{false: "/database-error", true: "/canceled"}[canceled], func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, "viewer") })
				router.GET(tc.path, tc.handler)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				want := http.StatusInternalServerError
				if canceled {
					cancel()
					want = statusClientClosedRequest
				}
				w := httptest.NewRecorder()
				// 隔离 schema 未建业务表，用真实数据库错误验证 500 不被吞掉。
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil).WithContext(ctx))
				if w.Code != want || canceled && w.Body.Len() != 0 {
					t.Fatalf("status = %d, want %d, body = %s", w.Code, want, w.Body.String())
				}
			})
		}
	}
}

func TestEmbyPlaybackSelectionStringMinSegments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/Items/item/PlaybackInfo", strings.NewReader(`{"MediaSourceId":"source","DeviceProfile":{"TranscodingProfiles":[{"MinSegments":"1","MaxAudioChannels":"6"}]}}`))
	selection, _, err := embyPlaybackSelection(c)
	if err != nil || selection.MediaSourceID != "source" {
		t.Fatalf("selection = %+v, error = %v", selection, err)
	}
}
