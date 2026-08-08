package handler

import (
	"bytes"
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

func TestManualScrapeApplyOneReturnsUpdatedMediaViewAndPreservesFilePlacement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db,
		&model.Library{}, &model.Media{},
		&model.Favorite{}, &model.PlaybackHistory{}, &model.PlaylistItem{},
	); err != nil {
		t.Fatal(err)
	}

	repos := repository.New(db)
	log := zap.NewNop()
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "Old Scan Title", Path: "/media/movies/movie.mkv"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:    repos,
		Media:   service.NewMediaService(&config.Config{}, log, repos),
		Scraper: service.NewScraperService(&config.Config{}, log, repos, nil, nil, nil, nil, service.NewHub(log)),
	}

	body, err := json.Marshal(service.ManualScrapeRequest{Source: "manual", MediaType: "movie", Title: "New Metadata Title"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: media.ID}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/media/"+media.ID+"/scrape/apply", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	manualScrapeApplyOneHandler(svc)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var got model.MediaView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Metadata Title" {
		t.Fatalf("response title = %q, want updated metadata title", got.Title)
	}
	if got.Path != media.Path || got.LibraryID != lib.ID {
		t.Fatalf("response changed file placement: path=%q library_id=%q", got.Path, got.LibraryID)
	}

	var stored model.Media
	if err := repos.DB.First(&stored, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Path != media.Path || stored.LibraryID != lib.ID {
		t.Fatalf("manual scrape changed stored file placement: path=%q library_id=%q", stored.Path, stored.LibraryID)
	}
}
