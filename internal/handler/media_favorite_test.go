package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestFavoriteHandlersRejectEpisodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.PlayProfile{}, &model.Library{}, &model.Media{}, &model.Favorite{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "favorite-http-user", Role: "admin", IsActive: true}
	library := model.Library{Name: "Shows", Path: "/shows", Type: "tv", Enabled: true}
	for _, row := range []any{&user, &library} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "test"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season", Source: "test"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "test"}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, MetadataID: episode.ID, Title: episode.Title, Path: "/shows/series/s01e01.mkv", SeasonNum: 1, EpisodeNum: 1}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := &service.Container{Repo: repos, Playback: service.NewPlaybackService(zap.NewNop(), repos), Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos), Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos)}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, user.ID); c.Next() })
	router.POST("/favourites/:id", toggleFavouriteHandler(svc))
	router.POST("/media/:id/favorite", addMediaFavoriteHandler(svc))
	router.DELETE("/media/:id/favorite", removeMediaFavoriteHandler(svc))
	router.PUT("/media/:id/series/favorite", setMediaSeriesFavoriteHandler(svc))
	registerEmbyAuthenticatedUserDataRoutes(router.Group(""), svc)
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/favourites/" + media.ID},
		{http.MethodPost, "/media/" + media.ID + "/favorite"},
		{http.MethodDelete, "/media/" + media.ID + "/favorite"},
		{http.MethodPost, "/Users/" + user.ID + "/FavoriteItems/" + media.MetadataID},
		{http.MethodDelete, "/Users/" + user.ID + "/FavoriteItems/" + media.MetadataID},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: status=%d, body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	rows, err := repos.Favorite.ListByUser(t.Context(), user.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rejected requests wrote favorites: %#v, %v", rows, err)
	}
	// 整剧入口可用分集文件定位父剧，但保存的收藏身份必须是整剧。
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/media/"+media.ID+"/series/favorite", strings.NewReader(`{"favourite":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("series favorite: status=%d, body=%s", response.Code, response.Body.String())
	}
	view, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view == nil {
		t.Fatalf("episode view = %#v, %v", view, err)
	}
	rows, err = repos.Favorite.ListByUser(t.Context(), user.ID)
	if err != nil || len(rows) != 1 || rows[0].MetadataID != view.SeriesID {
		t.Fatalf("explicit series favorite = %#v, %v", rows, err)
	}
}
