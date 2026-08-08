package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestCatalogHydrationQueueDeduplicatesByKindAndID(t *testing.T) {
	scraper := &ScraperService{}
	scraper.QueueCatalogHydration([]ExternalMediaResult{
		{Source: "tmdb", MediaType: "movie", TMDbID: 10, Title: "电影"},
		{Source: "TMDB", MediaType: "movie", TMDbID: 10, Title: "重复电影"},
		{Source: "tmdb", MediaType: "tv", TMDbID: 10, Title: "剧集"},
		{Source: "douban", MediaType: "movie", TMDbID: 20, Title: "豆瓣"},
		{Source: "tmdb", MediaType: "movie", TMDbID: 0, Title: "无 ID"},
	})

	batch := scraper.takeCatalogHydrationBatch()
	if len(batch) != 2 {
		t.Fatalf("queued item count = %d, want 2", len(batch))
	}
	keys := make(map[string]bool, len(batch))
	for _, item := range batch {
		keys[catalogHydrationKey(item)] = true
	}
	if !keys["tmdb:movie:10"] || !keys["tmdb:series:10"] {
		t.Fatalf("queued keys = %#v, want movie and series entries", keys)
	}
}

func TestCatalogHydrationQueueAllowsObservedRetry(t *testing.T) {
	scraper := &ScraperService{}
	item := ExternalMediaResult{Source: "tmdb", MediaType: "movie", TMDbID: 10}
	scraper.QueueCatalogHydration([]ExternalMediaResult{item})
	_ = scraper.takeCatalogHydrationBatch()
	scraper.QueueCatalogHydration([]ExternalMediaResult{item})
	if retry := scraper.takeCatalogHydrationBatch(); len(retry) != 1 {
		t.Fatalf("retry queue length = %d, want 1", len(retry))
	}
}

func TestCatalogHydrationWorkerStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	scraper := &ScraperService{}
	scraper.StartCatalogHydrationWorker(ctx)
	scraper.QueueCatalogHydration([]ExternalMediaResult{{Source: "tmdb", TMDbID: 10}})
	cancel()
	scraper.WaitCatalogHydrationWorker()
}

func TestCatalogHydrationPersistsMetadataArtworkAndNoMedia(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	scraper.people = NewPeopleImageStore(scraper.cfg, repos.Person, scraper.images)
	scraper.tmdb.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/tv/12345" {
			return http.DefaultTransport.RoundTrip(req)
		}
		body := `{"id":12345,"name":"间谍过家家","original_name":"SPY FAMILY","overview":"测试简介","poster_path":"/poster.jpg","backdrop_path":"/backdrop.jpg","first_air_date":"2022-04-09","vote_average":8.6,"origin_country":["JP"],"spoken_languages":[{"iso_639_1":"ja"}],"genres":[{"name":"Animation"}],"credits":{"cast":[{"id":99,"name":"Test Actor","character":"Hero","order":0,"profile_path":"/actor.jpg"}],"crew":[{"id":100,"name":"Test Director","job":"Director","order":0,"profile_path":"/director.jpg"}]}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	item := ExternalMediaResult{
		Source: "tmdb", MediaType: "tv", TMDbID: 12345,
		Title: "发现页标题", PosterURL: "https://images.example.test/images/w500/poster.jpg",
		BackdropURL: "https://images.example.test/images/w1280/backdrop.jpg",
	}
	if err := scraper.hydrateCatalogItem(t.Context(), item); err != nil {
		t.Fatal(err)
	}

	metadata, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, "12345")
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil || metadata.Title != "间谍过家家" {
		t.Fatalf("metadata = %#v, want hydrated TMDb title", metadata)
	}
	if metadata.CatalogHydratedAt == nil {
		t.Fatal("catalog hydration timestamp is empty")
	}
	for _, artworkType := range []string{model.ArtworkTypePoster, model.ArtworkTypeBackdrop} {
		asset, findErr := repos.Artwork.FindSelection(t.Context(), metadata.ID, artworkType)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if asset == nil {
			t.Fatalf("missing %s artwork", artworkType)
		}
		path := filepath.Join(scraper.cfg.App.DataDir, "artwork", filepath.FromSlash(asset.StorageKey))
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("stored %s artwork %q: %v", artworkType, path, statErr)
		}
	}
	credits, err := repos.Person.ListCreditsWithPeople(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(credits) != 2 {
		t.Fatalf("credit count = %d, want 2", len(credits))
	}
	for _, credit := range credits {
		if credit.Person.ProfileImageKey == "" {
			t.Fatalf("credit %q has no localized profile image", credit.Person.Name)
		}
		path := filepath.Join(scraper.cfg.App.DataDir, "people", filepath.FromSlash(credit.Person.ProfileImageKey))
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("stored profile image %q: %v", path, statErr)
		}
	}

	var mediaCount int64
	if err := repos.DB.Model(&model.Media{}).Count(&mediaCount).Error; err != nil {
		t.Fatal(err)
	}
	if mediaCount != 0 {
		t.Fatalf("media rows = %d, want 0 for catalog-only hydration", mediaCount)
	}
}

func TestCatalogHydrationSkipsCompletedMetadata(t *testing.T) {
	scraper, _, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	item := ExternalMediaResult{Source: "tmdb", MediaType: "tv", TMDbID: 12345}

	if err := scraper.hydrateCatalogItem(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	scraper.tmdb.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("completed catalog should not call TMDb")
	})}
	if err := scraper.hydrateCatalogItem(t.Context(), item); err != nil {
		t.Fatalf("completed catalog was hydrated again: %v", err)
	}
}

func TestCatalogHydrationRetriesAfterProviderFailure(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	item := ExternalMediaResult{Source: "tmdb", MediaType: "tv", TMDbID: 12345}
	failed := false
	scraper.tmdb.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !failed {
			failed = true
			return nil, errors.New("temporary TMDb failure")
		}
		return http.DefaultTransport.RoundTrip(req)
	})}

	if err := scraper.hydrateCatalogItem(t.Context(), item); err == nil {
		t.Fatal("first hydration unexpectedly succeeded")
	}
	if metadata, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, "12345"); err != nil {
		t.Fatal(err)
	} else if metadata != nil && metadata.CatalogHydratedAt != nil {
		t.Fatal("failed hydration was marked complete")
	}
	if err := scraper.hydrateCatalogItem(t.Context(), item); err != nil {
		t.Fatalf("retry hydration failed: %v", err)
	}
}
