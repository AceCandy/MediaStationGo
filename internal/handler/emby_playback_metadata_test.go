package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestEmbyVideoStreamResolvesMetadataIDToMediaID(t *testing.T) {
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
		Base: model.Base{ID: "metadata-playback"}, Kind: model.MetadataKindMovie,
		Title: "Metadata Playback", Source: "tmdb",
	}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	mediaPath := filepath.Join(t.TempDir(), "movie.mp4")
	content := []byte("playable media")
	if err := os.WriteFile(mediaPath, content, 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	if err := db.Create(&model.Media{
		Base: model.Base{ID: "media-playback"}, LibraryID: lib.ID, MetadataID: metadata.ID,
		Title: metadata.Title, Path: mediaPath, Container: "mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	cfg := &config.Config{}
	const secret = "test-secret"
	reposSvc := &service.Container{
		Repo:   repos,
		Emby:   service.NewEmbyService(cfg, zap.NewNop(), repos),
		Stream: service.NewStreamService(cfg, zap.NewNop(), repos, nil),
	}
	router := gin.New()
	registerEmbyRoutes(router, secret, reposSvc)
	req := httptest.NewRequest(http.MethodGet, "/Videos/"+metadata.ID+"/stream", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != string(content) {
		t.Fatalf("body=%q, want %q", w.Body.String(), content)
	}
}
