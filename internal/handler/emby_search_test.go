package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestEmbySearchHintsReturnsSharedMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base: model.Base{ID: "user-1"}, Username: "tester", PasswordHash: "x", Role: "admin", Tier: "plus", IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	metadata := model.MetadataItem{
		Base: model.Base{ID: "metadata-search"}, Kind: model.MetadataKindMovie,
		Title: "可搜索电影", OriginalName: "Searchable Movie", Source: "tmdb",
	}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	if err := db.Create(&model.Media{
		Base: model.Base{ID: "media-search"}, LibraryID: lib.ID, MetadataID: metadata.ID,
		Title: "扫描提示", Path: "/media/searchable.mkv",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	emby := service.NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: emby,
	})
	for _, path := range []string{
		"/SearchHints?SearchTerm=可搜索",
		"/Users/user-1/SearchHints?SearchTerm=Searchable",
		"/search/hints?SearchTerm=可搜索",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var body struct {
			Hints []map[string]any `json:"SearchHints"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		if len(body.Hints) != 1 || body.Hints[0]["ItemId"] != metadata.ID || body.Hints[0]["Name"] != metadata.Title {
			t.Fatalf("%s hints=%#v", path, body.Hints)
		}
	}
}
