package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestStatsSnapshotHidesAdultRecentlyAddedForUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	viewer := &model.User{Username: "viewer", PasswordHash: "hash", Role: "user", HideAdult: true}
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
	if err := db.Create(&model.Media{LibraryID: safe.ID, Title: "普通电影", Path: "/media/movie/a.mkv", SizeBytes: 100, DurationSec: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{LibraryID: adult.ID, Title: "成人影片", Path: "/media/9KG/a.mkv", SizeBytes: 200, DurationSec: 20}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repos}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, viewer.ID)
	c.Request = httptest.NewRequest("GET", "/api/stats", nil)

	snap := &service.Snapshot{}
	if err := applyStatsVisibility(c, svc, snap); err != nil {
		t.Fatalf("applyStatsVisibility: %v", err)
	}
	if snap.MediaCount != 1 || snap.TotalSizeBytes != 100 || snap.TotalSeconds != 10 {
		t.Fatalf("stats should only include visible media, got count=%d size=%d seconds=%d", snap.MediaCount, snap.TotalSizeBytes, snap.TotalSeconds)
	}
	if len(snap.RecentlyAdded) != 1 || snap.RecentlyAdded[0].LibraryID != safe.ID {
		t.Fatalf("recently added should hide adult library, got %#v", snap.RecentlyAdded)
	}
}

func TestStatsLibrariesCountsEachLocalLibrary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	primary := model.Library{Name: "国产电影", Path: "/media/国产电影", Type: "movie", Enabled: true}
	secondary := model.Library{Name: "国产电影 2", Path: "/media/国产电影-2", Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&primary, &secondary} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&[]model.Media{
		{LibraryID: primary.ID, Title: "电影一", Path: "/media/国产电影/one.mkv", SizeBytes: 100},
		{LibraryID: secondary.ID, Title: "电影二", Path: "/media/国产电影-2/two.mkv", SizeBytes: 200},
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repos}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/stats/libraries", nil)

	statsLibrariesHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Libraries []struct {
			Library struct {
				ID string `json:"id"`
			} `json:"library"`
			ItemCount int64 `json:"item_count"`
			TotalSize int64 `json:"total_size"`
		} `json:"libraries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Libraries) != 2 {
		t.Fatalf("libraries = %#v, want two local libraries", payload.Libraries)
	}
	stats := make(map[string][2]int64, len(payload.Libraries))
	for _, library := range payload.Libraries {
		stats[library.Library.ID] = [2]int64{library.ItemCount, library.TotalSize}
	}
	if got := stats[primary.ID]; got != ([2]int64{1, 100}) {
		t.Fatalf("primary stats = %#v, want count=1 size=100", got)
	}
	if got := stats[secondary.ID]; got != ([2]int64{1, 200}) {
		t.Fatalf("secondary stats = %#v, want count=1 size=200", got)
	}
}
