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
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestRefreshMetadataTMDbOnlyUpdatesCurrentMetadata(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}, &model.TMDbRecheckJob{})
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
		if requests == 1 || requests == 5 {
			cast = append(cast, map[string]any{"id": 100, "name": "演员", "character": "角色"})
		}
		payload := map[string]any{
			"id": id, "title": fmt.Sprintf("最新标题%d", requests), "name": fmt.Sprintf("最新标题%d", requests),
			"overview": "最新简介", "runtime": 42, "season_number": 0, "episode_number": 1, "vote_average": 8,
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
		if item.ID == movie.ID && stored.RuntimeSec != 42*60 {
			t.Fatalf("movie runtime = %d, want %d", stored.RuntimeSec, 42*60)
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
			if (requests == 1 && creditCount != 1) || (requests == 5 && creditCount != 1) {
				t.Fatalf("loaded credit snapshot not replaced: %d", creditCount)
			}
			if requests == 1 {
				var credit model.MetadataCredit
				if err := db.Where("metadata_id = ?", item.ID).First(&credit).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&model.Person{}).Where("id = ?", credit.PersonID).Update("name", "已翻译演员").Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&model.MetadataCredit{}).Where("id = ?", credit.ID).Update("role", "已翻译角色").Error; err != nil {
					t.Fatal(err)
				}
			}
			if requests == 5 {
				var credit model.MetadataCredit
				if err := db.Where("metadata_id = ?", item.ID).First(&credit).Error; err != nil {
					t.Fatal(err)
				}
				var person model.Person
				if err := db.First(&person, "id = ?", credit.PersonID).Error; err != nil || person.Name != "已翻译演员" || credit.Role != "已翻译角色" {
					t.Fatalf("translated credit was not preserved: person=%#v credit=%#v err=%v", person, credit, err)
				}
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

func TestRefreshMetadataTMDbClearsNotFound(t *testing.T) {
	for _, mode := range []string{"episode", "season", "episode_missing_id", "season_missing_id", "episode_stale_id", "running", "404", "wrong_identity"} {
		t.Run(mode, func(t *testing.T) {
			isSeason := mode == "season" || mode == "season_missing_id"
			db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{},
				&model.TMDbRecheckJob{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckAssetChange{})
			if err := database.EnsureTMDbRecheckTriggers(db); err != nil {
				t.Fatal(err)
			}
			repos := repository.New(db)
			series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "20"})
			season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "season", ExternalID: "30"})
			episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "episode", ExternalID: "40"})
			missing := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 2})
			if mode == "episode_missing_id" || mode == "season_missing_id" {
				if err := db.Where("metadata_id IN ?", []string{season.ID, episode.ID}).Delete(&model.MetadataIdentifier{}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "episode_stale_id" {
				if err := db.Model(&model.MetadataIdentifier{}).Where("metadata_id=?", episode.ID).Update("external_id", "999").Error; err != nil {
					t.Fatal(err)
				}
			}
			future := time.Now().Add(20 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
			for _, item := range []*model.MetadataItem{season, episode, missing} {
				if err := db.Create(&model.Media{MetadataID: item.ID, Path: "/test/" + item.ID + ".strm"}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.TMDbRecheckJob{MetadataID: item.ID, Status: "not_found", DueAt: &future, Attempts: 2, LastError: "清单未收录", NotFoundIdentity: "old"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			oldJob := model.TMDbRecheckJob{MetadataID: episode.ID, LeaseToken: "old-request", NotFoundIdentity: "old"}
			if mode == "running" {
				if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", episode.ID).Updates(map[string]any{"status": "running", "lease_token": oldJob.LeaseToken, "lease_until": future}).Error; err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/tv/20/season/1/episode/1"
				if isSeason {
					wantPath = "/tv/20/season/1"
				}
				if r.URL.Path != wantPath {
					t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
				}
				if mode == "404" {
					http.NotFound(w, r)
					return
				}
				if mode == "wrong_identity" {
					fmt.Fprint(w, `{"id":40,"season_number":2,"episode_number":1,"name":"测试单集"}`)
				} else if isSeason {
					fmt.Fprint(w, `{"id":30,"season_number":1,"name":"测试季","episodes":[{"id":40,"episode_number":1,"name":"测试单集"}]}`)
				} else {
					fmt.Fprint(w, `{"id":40,"season_number":1,"episode_number":1,"name":"测试单集"}`)
				}
			}))
			defer server.Close()
			cfg := &config.Config{Secrets: config.SecretsConfig{TMDbAPIKey: "test-key", TMDbAPIProxy: server.URL}}
			s := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
			target := episode
			if isSeason {
				target = season
			}
			err := s.RefreshMetadataTMDb(t.Context(), target.ID)
			failed := mode == "404" || mode == "wrong_identity"
			if (err != nil) != failed {
				t.Fatalf("refresh err=%v, want failure=%v", err, failed)
			}
			if !failed {
				wantID := 40
				if isSeason {
					wantID = 30
				}
				if id, err := s.metadataTMDbRefreshID(t.Context(), target); err != nil || id != wantID {
					t.Fatalf("refreshed identifier = %d, err=%v, want %d", id, err, wantID)
				}
				assertServiceTestTMDbSnapshot(t, repos, target.ID)
			}
			for _, item := range []*model.MetadataItem{season, episode, missing} {
				var job model.TMDbRecheckJob
				if err := db.First(&job, "metadata_id=?", item.ID).Error; err != nil {
					t.Fatal(err)
				}
				found := !failed && (item.ID == episode.ID || isSeason && item.ID == season.ID)
				if found {
					if job.Status != "pending" || job.LastError != "" || job.NotFoundIdentity != "" || job.Attempts != 0 || job.LeaseToken != "" || job.LeaseUntil != nil || job.DueAt == nil || job.DueAt.After(time.Now()) {
						t.Fatalf("found target retains old failure: %+v", job)
					}
				} else if job.Status != "not_found" || job.LastError != "清单未收录" || job.Attempts != 2 || job.DueAt == nil || !job.DueAt.Equal(future) {
					t.Fatalf("unconfirmed target changed: %+v", job)
				}
			}
			if mode == "running" {
				if err := repos.Metadata.FinishTMDbRecheck(t.Context(), &oldJob, "not_found", "旧结果", &future, 3); !errors.Is(err, repository.ErrTMDbRecheckChanged) {
					t.Fatalf("stale writer accepted: %v", err)
				}
			}
			stored, err := repos.Metadata.FindByID(t.Context(), episode.ID)
			if err != nil || stored.Overview != "" || stored.ReleaseDate != "" {
				t.Fatalf("fixture must retain missing fields: %+v %v", stored, err)
			}
		})
	}
}

func TestValidTMDbRefreshPosition(t *testing.T) {
	for _, test := range []struct {
		payload         string
		season, episode int
		valid           bool
	}{
		{`{"season_number":0}`, 0, 0, true},
		{`{"season_number":0,"episode_number":1}`, 0, 1, true},
		{`{"season_number":1,"episode_number":2}`, 1, 2, true},
		{`{"season_number":2,"episode_number":2}`, 1, 2, false},
		{`{"season_number":1,"episode_number":3}`, 1, 2, false},
		{`{"season_number":1}`, 1, 2, false},
		{`{"episode_number":1}`, 0, 1, false},
		{`{"season_number":null}`, 0, 0, false},
		{`invalid`, 0, 0, false},
	} {
		if got := validTMDbRefreshPosition([]byte(test.payload), test.season, test.episode); got != test.valid {
			t.Errorf("position %s for S%dE%d = %v, want %v", test.payload, test.season, test.episode, got, test.valid)
		}
	}
}

func TestMergeTMDbMetadataPreservesMissingFields(t *testing.T) {
	item := &model.MetadataItem{
		Title: "旧标题", OriginalName: "Old Original", Overview: "旧简介", Rating: 7.5,
		Year: 2020, ReleaseDate: "2020-01-02", RuntimeSec: 3600,
		Languages: "zh", Countries: "CN", Genres: "剧情",
	}
	mergeTMDbMetadata(item, &model.MetadataItem{Title: "新标题", Rating: 8.1, Year: 2026, Genres: "科幻"})
	if item.Title != "新标题" || item.Rating != 8.1 || item.Year != 2026 || item.Genres != "科幻" {
		t.Fatalf("valid TMDB fields were not applied: %#v", item)
	}
	if item.OriginalName != "Old Original" || item.Overview != "旧简介" || item.ReleaseDate != "2020-01-02" || item.RuntimeSec != 3600 || item.Languages != "zh" || item.Countries != "CN" {
		t.Fatalf("missing TMDB fields cleared local values: %#v", item)
	}
}

func TestDiscoverTMDbRefreshCooldown(t *testing.T) {
	db := newServiceTestDB(t, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{})
	repos := repository.New(db)
	item := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "movie", Title: "原资料", Source: "tmdb"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "movie", ExternalID: "10"})
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `{"id":10,"title":"最新资料"}`)
	}))
	defer upstream.Close()
	cfg := &config.Config{Secrets: config.SecretsConfig{TMDbAPIKey: "test-key", TMDbAPIProxy: upstream.URL}}
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
	id := repository.DiscoverIdentity{TMDbID: 10, MediaType: "movie"}
	for _, age := range []time.Duration{time.Hour, 3*time.Hour - time.Minute, 3 * time.Hour, 4 * time.Hour} {
		if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(`{"id":10}`), time.Now().Add(-age)); err != nil {
			t.Fatal(err)
		}
		before := requests
		if err := scraper.RefreshMetadataTMDbByIdentity(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		want := 0
		if age >= 3*time.Hour {
			want = 1
		}
		if requests-before != want {
			t.Fatalf("age %s: requests = %d, want %d", age, requests-before, want)
		}
	}
	before := requests
	if err := scraper.RefreshMetadataTMDbByIdentity(t.Context(), id); err != nil || requests != before {
		t.Fatalf("repeat refresh bypassed cooldown: requests=%d err=%v", requests-before, err)
	}
	if err := scraper.RefreshMetadataTMDb(t.Context(), item.ID); err != nil || requests != before+1 {
		t.Fatalf("manual refresh was throttled: requests=%d err=%v", requests-before, err)
	}
}
