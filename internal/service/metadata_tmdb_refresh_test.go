package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestRefreshMetadataTMDbOnlyUpdatesCurrentMetadata(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{})
	repos := repository.New(db)
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	makeItem := func(kind, externalID string, parent *string, season, episode int) *model.MetadataItem {
		return createServiceTestMetadata(t, db, model.MetadataItem{
			Kind: kind, ParentID: parent, SeasonNum: season, EpisodeNum: episode,
			Title: "旧标题", Overview: "旧简介", Source: "manual", NSFW: true,
			CatalogMetadataHydratedAt: &old, CatalogArtworkHydratedAt: &old, CatalogHydratedAt: &old,
		}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: kind, ExternalID: externalID})
	}
	movie := makeItem(model.MetadataKindMovie, "10", nil, 0, 0)
	series := makeItem(model.MetadataKindSeries, "20", nil, 0, 0)
	season := makeItem(model.MetadataKindSeason, "30", &series.ID, 0, 0)
	episode := makeItem(model.MetadataKindEpisode, "40", &season.ID, 0, 1)
	media := model.Media{MetadataID: movie.ID, Title: "扫描标题", Path: "/test/movie.mkv", TMDbID: 999, ScrapeStatus: "error", ScrapeError: "kept"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	beforeMedia, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	wrongID := false
	imageRequests := 0
	failImage := false
	imageData := testArtworkPNG(t, 4, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		id := map[string]int{"/movie/10": 10, "/tv/20": 20, "/tv/20/season/0": 30, "/tv/20/season/0/episode/1": 40}[r.URL.Path]
		if id == 0 {
			t.Errorf("unexpected TMDB request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if wrongID {
			id++
		}
		w.Header().Set("Content-Type", "application/json")
		cast := []any{}
		if requests == 1 {
			cast = append(cast, map[string]any{"id": 100, "name": "演员", "character": "角色"})
		}
		payload := map[string]any{
			"id": id, "title": fmt.Sprintf("最新标题%d", requests), "name": fmt.Sprintf("最新标题%d", requests),
			"overview": "最新简介", "runtime": 42, "season_number": 0, "vote_average": 8,
			"release_date": "2026-09-06", "first_air_date": "2026-09-06", "air_date": "2026-09-06",
			"credits":     map[string]any{"cast": cast, "crew": []any{}},
			"poster_path": "/poster.png", "backdrop_path": "/backdrop.png", "still_path": "/still.png",
		}
		if r.URL.Path == "/tv/20/season/0" {
			payload["episodes"] = []any{map[string]any{"id": 40, "episode_number": 1, "name": "季接口单集", "overview": "季接口简介", "air_date": "2026-09-07", "vote_average": 9, "runtime": 43, "still_path": "/episode.png"}}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
	cfg.Secrets.TMDbImageProxy = "https://images.example.test/images"
	cfg.App.DataDir = t.TempDir()
	cfg.Cache.CacheDir = filepath.Join(cfg.App.DataDir, "cache")
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
	images := NewImageProxy(cfg, zap.NewNop())
	images.client = &http.Client{Transport: imageRoundTripFunc(func(*http.Request) (*http.Response, error) {
		imageRequests++
		status := http.StatusOK
		if failImage {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(imageData))}, nil
	})}
	scraper.SetArtworkStore(NewArtworkStore(cfg, repos.Artwork, images))
	for _, item := range []*model.MetadataItem{movie, series, season, episode, movie} {
		if err := scraper.RefreshMetadataTMDb(t.Context(), item.ID); err != nil {
			t.Fatal(err)
		}
		stored, err := repos.Metadata.FindByID(t.Context(), item.ID)
		if err != nil || stored == nil {
			t.Fatalf("metadata read failed: %v", err)
		}
		if stored.Title != fmt.Sprintf("最新标题%d", requests) || stored.Overview != "最新简介" || stored.Rating != 8 {
			t.Fatalf("metadata not refreshed: %#v", stored)
		}
		if stored.ID != item.ID || !reflect.DeepEqual(stored.ParentID, item.ParentID) || stored.Kind != item.Kind || !stored.NSFW || !stored.CatalogHydratedAt.Equal(old) {
			t.Fatalf("metadata identity or unrelated state changed: %#v", stored)
		}
		assertServiceTestTMDbSnapshot(t, repos, item.ID)
		if item.ID == season.ID {
			storedEpisode, err := repos.Metadata.FindByID(t.Context(), episode.ID)
			if err != nil || storedEpisode == nil || storedEpisode.Title != "季接口单集" || storedEpisode.RuntimeSec != 43*60 {
				t.Fatalf("season response did not refresh episode: %#v, err=%v", storedEpisode, err)
			}
			assertServiceTestTMDbSnapshot(t, repos, episode.ID)
			hasStill, err := repos.Artwork.HasProviderArtwork(t.Context(), episode.ID, model.ArtworkTypeStill, "tmdb")
			if err != nil || !hasStill {
				t.Fatalf("season response did not refresh episode still: has=%v err=%v", hasStill, err)
			}
		}
		if item.ID == movie.ID {
			var creditCount int64
			if err := db.Model(&model.MetadataCredit{}).Where("metadata_id = ?", item.ID).Count(&creditCount).Error; err != nil {
				t.Fatal(err)
			}
			if (requests == 1 && creditCount != 1) || (requests == 5 && creditCount != 0) {
				t.Fatalf("loaded credit snapshot not replaced: %d", creditCount)
			}
		}
	}
	if requests != 5 {
		t.Fatalf("requests = %d, want 5 including forced repeat", requests)
	}
	if imageRequests != 9 {
		t.Fatalf("image requests = %d, want 9 including season episode still and already-cached images", imageRequests)
	}
	afterMedia, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil || !reflect.DeepEqual(beforeMedia, afterMedia) {
		t.Fatalf("media changed: before=%#v after=%#v err=%v", beforeMedia, afterMedia, err)
	}
	// 没有媒体文件的条目同样可刷新；不能创建新实体或扩展父子目录。
	var count int64
	if err := db.Model(&model.MetadataItem{}).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("metadata count = %d, err = %v", count, err)
	}
	before, _ := repos.Metadata.FindByID(t.Context(), movie.ID)
	beforeSnapshot, _ := repos.Metadata.FindProviderSnapshot(t.Context(), movie.ID, "tmdb")
	wrongID = true
	if err := scraper.RefreshMetadataTMDb(t.Context(), movie.ID); !errors.Is(err, ErrTMDbRefreshIdentity) {
		t.Fatalf("wrong identity error = %v", err)
	}
	after, _ := repos.Metadata.FindByID(t.Context(), movie.ID)
	afterSnapshot, _ := repos.Metadata.FindProviderSnapshot(t.Context(), movie.ID, "tmdb")
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeSnapshot, afterSnapshot) {
		t.Fatal("mismatched provider identity changed metadata or snapshot")
	}
	unbound := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "未绑定", Source: "manual"})
	beforeRequests := requests
	if err := scraper.RefreshMetadataTMDb(t.Context(), unbound.ID); !errors.Is(err, ErrTMDbRefreshIdentity) || requests != beforeRequests {
		t.Fatalf("missing identity: err=%v requests=%d", err, requests)
	}
	wrongID, failImage = false, true
	if err := scraper.RefreshMetadataTMDb(t.Context(), movie.ID); err == nil {
		t.Fatal("failed image refresh reported success")
	}
	failedSnapshot, _ := repos.Metadata.FindProviderSnapshot(t.Context(), movie.ID, "tmdb")
	if !reflect.DeepEqual(beforeSnapshot, failedSnapshot) {
		t.Fatal("incomplete refresh advanced snapshot")
	}
}
