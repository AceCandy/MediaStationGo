package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testutil"
)

func firstIndexFunc(values []string, match func(string) bool) int {
	for i, value := range values {
		if match(value) {
			return i
		}
	}
	return -1
}

func newTestScraper(t *testing.T) (*ScraperService, *repository.Container, func()) {
	t.Helper()
	imageData := testArtworkPNG(t, 4, 3)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/images/") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData)
			return
		}
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
				"overview":     "单集剧情",
				"still_path":   "/still.jpg",
				"air_date":     "2023-10-07",
				"vote_average": 9.1,
				"runtime":      24,
			})
		case r.URL.Path == "/tv/12345/credits":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cast": []map[string]any{{"id": 99, "name": "Test Actor", "character": "Hero", "order": 0, "profile_path": "/actor.jpg"}},
				"crew": []map[string]any{{"id": 100, "name": "Test Director", "job": "Director"}},
			})
		case strings.HasPrefix(r.URL.Path, "/tv/12345"):
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

	dsn := "file:" + url.QueryEscape(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := migrateScraperTestModels(t, db); err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	cfg.Secrets.TMDbImageProxy = "https://images.example.test/images"
	cfg.App.DataDir = t.TempDir()
	cfg.Cache.CacheDir = filepath.Join(cfg.App.DataDir, "cache")
	log := zap.NewNop()
	tmdb := NewTMDbProvider(cfg, log, nil)
	scraper := NewScraperService(cfg, log, repos, tmdb, nil, nil, nil, NewHub(log))
	images := NewImageProxy(cfg, log)
	images.client = &http.Client{Transport: imageRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(imageData)),
		}, nil
	})}
	scraper.SetImageProxy(images)
	scraper.SetArtworkStore(NewArtworkStore(cfg, repos.Artwork, images))

	return scraper, repos, upstream.Close
}

func migrateScraperTestModels(t *testing.T, db *gorm.DB, extra ...any) error {
	t.Helper()
	models := []any{
		&model.Library{}, &model.MetadataItem{}, &model.MetadataIdentifier{},
		&model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.Media{},
		&model.Favorite{}, &model.PlaybackHistory{}, &model.PlaylistItem{},
		&model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{},
	}
	models = append(models, extra...)
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}
	return testutil.RegisterMediaMetadataFixtures(db)
}

func firstQuery(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func lastIndexFunc(values []string, match func(string) bool) int {
	for i := len(values) - 1; i >= 0; i-- {
		if match(values[i]) {
			return i
		}
	}
	return -1
}
