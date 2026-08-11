package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestEnrichLibraryDefersEpisodeDetailsUntilMainMetadataFinishes(t *testing.T) {
	var mu sync.Mutex
	paths := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/tv"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":             12345,
					"name":           "间谍过家家",
					"original_name":  "SPY×FAMILY",
					"overview":       "测试简介",
					"poster_path":    "/poster.jpg",
					"backdrop_path":  "/backdrop.jpg",
					"first_air_date": "2022-04-09",
					"vote_average":   8.6,
				}},
			})
		case r.URL.Path == "/tv/12345/season/2/episode/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":         "任务代号: 猫",
				"overview":     "第一集剧情",
				"still_path":   "/still-1.jpg",
				"vote_average": 8.9,
				"runtime":      24,
			})
		case r.URL.Path == "/tv/12345/season/2/episode/2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":         "接近目标",
				"overview":     "第二集剧情",
				"still_path":   "/still-2.jpg",
				"vote_average": 9.0,
				"runtime":      25,
			})
		case r.URL.Path == "/tv/12345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             12345,
				"name":           "间谍过家家",
				"overview":       "测试简介",
				"poster_path":    "/poster.jpg",
				"backdrop_path":  "/backdrop.jpg",
				"first_air_date": "2022-04-09",
				"vote_average":   8.6,
				"origin_country": []string{"JP"},
				"spoken_languages": []map[string]any{{
					"iso_639_1": "ja",
				}},
				"genres": []map[string]any{{
					"name": "Animation",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateScraperTestModels(t, db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	cfg.Secrets.TMDbImageProxy = upstream.URL + "/images"
	log := zap.NewNop()
	scraper := NewScraperService(cfg, log, repos, NewTMDbProvider(cfg, log, nil), nil, nil, nil, NewHub(log))

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{
			Base:         model.Base{ID: "episode-1"},
			LibraryID:    lib.ID,
			Title:        "间谍过家家",
			Path:         filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv"),
			SeasonNum:    2,
			EpisodeNum:   1,
			ScrapeStatus: "pending",
		},
		{
			Base:         model.Base{ID: "episode-2"},
			LibraryID:    lib.ID,
			Title:        "间谍过家家",
			Path:         filepath.Join(lib.Path, "间谍过家家 - S02E02.mkv"),
			SeasonNum:    2,
			EpisodeNum:   2,
			ScrapeStatus: "pending",
		},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	result, err := scraper.EnrichLibraryDetailed(t.Context(), lib.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 2 || result.Matched != 2 {
		t.Fatalf("result=%+v, want two matched episodes", result)
	}

	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	mu.Unlock()
	firstEpisodeDetail := firstIndexFunc(gotPaths, func(path string) bool {
		return strings.Contains(path, "/season/")
	})
	lastMainMetadata := lastIndexFunc(gotPaths, func(path string) bool {
		return strings.HasPrefix(path, "/search/tv") || path == "/tv/12345"
	})
	if firstEpisodeDetail < 0 {
		t.Fatalf("no deferred episode detail requests recorded: %v", gotPaths)
	}
	if firstEpisodeDetail <= lastMainMetadata {
		t.Fatalf("episode detail ran before main metadata finished: paths=%v", gotPaths)
	}

	var stored []model.Media
	if err := repos.DB.Where("library_id = ?", lib.ID).Order("episode_num ASC").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored episodes = %d, want 2", len(stored))
	}
	views := []*model.MediaView{
		serviceTestMediaView(t, repos, stored[0].ID),
		serviceTestMediaView(t, repos, stored[1].ID),
	}
	if views[0].Overview != "第一集剧情" || views[1].Overview != "第二集剧情" {
		t.Fatalf("deferred episode metadata not saved: %+v", views)
	}
	if views[0].Title != "任务代号: 猫" || views[1].Title != "接近目标" {
		t.Fatalf("episode canonical titles not saved: %+v", views)
	}
	if views[0].SeriesTitle != "间谍过家家" || views[1].SeriesTitle != "间谍过家家" {
		t.Fatalf("parent series titles not projected: %+v", views)
	}
	if views[0].OriginalName != "" || views[1].OriginalName != "" {
		t.Fatalf("episode original_name must not inherit the series: %+v", views)
	}
}

func TestEnrichLibrarySkipsDeferredEpisodeStillWhenDisabled(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv"),
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	episodeArtwork := false
	result, err := scraper.EnrichLibraryDetailedWithOptions(t.Context(), lib.ID, ScrapeOptions{
		RetryNoMatch:   true,
		EpisodeArtwork: &episodeArtwork,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 1 || result.Matched != 1 {
		t.Fatalf("result=%+v, want one matched episode", result)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	if got.Overview != "单集剧情" || got.DurationSec != 24*60 {
		t.Fatalf("deferred episode text metadata should still be saved: overview=%q duration=%d", got.Overview, got.DurationSec)
	}
	var stillCount int64
	if got.MetadataID == "" {
		t.Fatal("matched episode has no metadata link")
	}
	if err := repos.DB.Model(&model.MetadataArtwork{}).
		Where("metadata_id = ? AND artwork_type = ?", got.MetadataID, model.ArtworkTypeStill).Count(&stillCount).Error; err != nil {
		t.Fatal(err)
	}
	if stillCount != 0 {
		t.Fatalf("episode still disabled state invalid: rows=%d", stillCount)
	}
	series := serviceTestTMDbSeries(t, repos, got, 12345)
	serviceTestArtworkSelections(t, repos, series.ID, model.ArtworkTypePoster, model.ArtworkTypeBackdrop)
}
