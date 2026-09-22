package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTMDbMatchMarksExtendedDetailsLoaded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/41":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 41, "title": "Known Movie", "original_language": "en",
				"production_countries": []map[string]any{{"iso_3166_1": "US"}},
				"genres":               []map[string]any{{"name": "Drama"}},
			})
		case "/tv/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42, "name": "Known Series", "original_language": "ja",
				"origin_country": []string{"JP"},
				"genres":         []map[string]any{{"name": "Animation"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	provider := NewTMDbProvider(cfg, zap.NewNop(), nil)
	movie, err := provider.GetMovieMatch(t.Context(), 41)
	if err != nil {
		t.Fatal(err)
	}
	series, err := provider.GetTVMatch(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if movie == nil || !movie.TMDbDetailsLoaded || strings.Join(movie.Countries, ",") != "US" || strings.Join(movie.Genres, ",") != "Drama" {
		t.Fatalf("movie details not marked complete: %#v", movie)
	}
	if series == nil || !series.TMDbDetailsLoaded || strings.Join(series.Countries, ",") != "JP" || strings.Join(series.Genres, ",") != "Animation" {
		t.Fatalf("series details not marked complete: %#v", series)
	}
}

func TestWakeScrapeWorkerSignalsThreeMediaWorkers(t *testing.T) {
	scraper := NewScraperService(&config.Config{}, zap.NewNop(), nil, nil, nil, nil, nil, nil)
	scraper.WakeScrapeWorker()
	if got := len(scraper.mediaScrapeWake); got != autoMediaScrapeWorkerCount {
		t.Fatalf("media wake signals = %d, want %d", got, autoMediaScrapeWorkerCount)
	}
	if got := len(scraper.catalogHydrationWake); got != 1 {
		t.Fatalf("catalog wake signals = %d, want 1", got)
	}
}

func TestKnownTMDbIDReusesLoadedDetails(t *testing.T) {
	scraper, repos, closeDefaultUpstream := newTestScraper(t)
	defer closeDefaultUpstream()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}

	var movieCalls atomic.Int32
	var tvCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/41":
			movieCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 41, "title": "Known Movie", "original_title": "Known Movie", "original_language": "en",
				"spoken_languages":     []map[string]any{{"iso_639_1": "fr"}},
				"production_countries": []map[string]any{{"iso_3166_1": "US"}},
				"genres":               []map[string]any{{"name": "Drama"}},
			})
		case "/tv/42":
			tvCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42, "name": "Known Series", "original_name": "Known Series", "original_language": "ja",
				"origin_country":   []string{"JP"},
				"spoken_languages": []map[string]any{{"iso_639_1": "en"}},
				"genres":           []map[string]any{{"name": "Animation"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	scraper.cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper.tmdb = NewTMDbProvider(scraper.cfg, zap.NewNop(), nil)
	movieLibrary := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	tvLibrary := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&[]*model.Library{&movieLibrary, &tvLibrary}).Error; err != nil {
		t.Fatal(err)
	}
	media := []model.Media{
		{LibraryID: movieLibrary.ID, Title: "Known Movie", Path: "/media/movies/known.mkv", TMDbID: 41, SeasonNum: 20, EpisodeNum: 24, ScrapeStatus: "pending"},
		{LibraryID: tvLibrary.ID, Title: "Known Series", Path: "/media/tv/Known Series {tmdb-42}/known.mkv", TMDbID: 42, ScrapeStatus: "pending"},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	for i := range media {
		if err := scraper.enrichOneWithOptions(t.Context(), &media[i], ScrapeOptions{DeferEpisodeDetails: true}); err != nil {
			t.Fatal(err)
		}
	}
	if movieCalls.Load() != 1 || tvCalls.Load() != 1 {
		t.Fatalf("TMDb detail calls movie=%d tv=%d, want one each", movieCalls.Load(), tvCalls.Load())
	}
	var storedMovie model.Media
	if err := repos.DB.First(&storedMovie, "id = ?", media[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedMovie.SeasonNum != 0 || storedMovie.EpisodeNum != 0 {
		t.Fatalf("matched movie kept dirty season/episode: %+v", storedMovie)
	}
	assertMetadataDetails(t, repos.Metadata, model.MetadataKindMovie, "41", "en,fr", "US", "Drama")
	assertMetadataDetails(t, repos.Metadata, model.MetadataKindSeries, "42", "ja,en", "JP", "Animation")
	movieMetadata, _ := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "41")
	seriesMetadata, _ := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, "42")
	assertServiceTestTMDbSnapshot(t, repos, movieMetadata.ID)
	assertServiceTestTMDbSnapshot(t, repos, seriesMetadata.ID)
}

func TestSearchTMDbMatchStillLoadsExtendedDetails(t *testing.T) {
	scraper, repos, closeDefaultUpstream := newTestScraper(t)
	defer closeDefaultUpstream()

	var searchCalls atomic.Int32
	var detailCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/movie":
			searchCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{
				"id": 43, "title": "搜索电影", "original_title": "Search Movie", "original_language": "en",
			}}})
		case "/movie/43":
			detailCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 43, "title": "搜索电影", "original_title": "Search Movie",
				"original_language":    "en",
				"spoken_languages":     []map[string]any{{"iso_639_1": "fr"}},
				"production_countries": []map[string]any{{"iso_3166_1": "US"}},
				"genres":               []map[string]any{{"name": "Drama"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	scraper.cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper.tmdb = NewTMDbProvider(scraper.cfg, zap.NewNop(), nil)
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "搜索电影", Path: "/media/movies/搜索电影.mkv", ScrapeStatus: "pending"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	candidates, err := scraper.ManualSearch(t.Context(), &media, "搜索电影", "tmdb", "movie")
	if err != nil || len(candidates) != 1 || candidates[0].TMDbID != 43 {
		t.Fatalf("manual candidates = %#v, err=%v", candidates, err)
	}
	if _, err := scraper.ApplyManualMatch(t.Context(), media.ID, ManualScrapeRequest{
		Source: "tmdb", MediaType: "movie", TMDbID: candidates[0].TMDbID, Title: candidates[0].Title,
	}); err != nil {
		t.Fatal(err)
	}
	if searchCalls.Load() != 1 || detailCalls.Load() != 1 {
		t.Fatalf("TMDb calls search=%d details=%d, want one each", searchCalls.Load(), detailCalls.Load())
	}
	assertMetadataDetails(t, repos.Metadata, model.MetadataKindMovie, "43", "en,fr", "US", "Drama")
	metadata, _ := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "43")
	assertServiceTestTMDbSnapshot(t, repos, metadata.ID)
}

func TestAutoMediaScrapeUsesThreeWorkersAndReportsTiming(t *testing.T) {
	scraper, repos, closeDefaultUpstream := newTestScraper(t)
	defer closeDefaultUpstream()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.AutoMigrate(&model.MediaProbeMetadata{}, &model.MetadataArtworkRecheck{}); err != nil {
		t.Fatal(err)
	}

	core, observed := observer.New(zap.InfoLevel)
	log := zap.New(core)
	var activeSearches atomic.Int32
	var maxSearches atomic.Int32
	var catalogCalls atomic.Int32
	allSearchesStarted := make(chan struct{})
	releaseSearches := make(chan struct{})
	var startedOnce sync.Once

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/movie/101" || r.URL.Path == "/movie/102" || r.URL.Path == "/movie/103":
			active := activeSearches.Add(1)
			for {
				current := maxSearches.Load()
				if active <= current || maxSearches.CompareAndSwap(current, active) {
					break
				}
			}
			if active == autoMediaScrapeWorkerCount {
				startedOnce.Do(func() { close(allSearchesStarted) })
			}
			select {
			case <-releaseSearches:
			case <-r.Context().Done():
				return
			}
			activeSearches.Add(-1)
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movie/"))
			name := map[int]string{101: "Alpha", 102: "Bravo", 103: "Charlie"}[id]
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "title": name, "original_title": name})
		case strings.HasPrefix(r.URL.Path, "/movie/999"):
			catalogCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "title": "Catalog Movie"})
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"original_language":    "en",
				"production_countries": []map[string]any{{"iso_3166_1": "US"}},
				"genres":               []map[string]any{{"name": "Drama"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	scraper.log = log
	scraper.cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper.tmdb = NewTMDbProvider(scraper.cfg, log, nil)
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, Title: "Alpha", Path: "/media/movies/alpha.mkv", TMDbID: 101, ScrapeStatus: "pending"},
		{LibraryID: library.ID, Title: "Bravo", Path: "/media/movies/bravo.mkv", TMDbID: 102, ScrapeStatus: "pending"},
		{LibraryID: library.ID, Title: "Charlie", Path: "/media/movies/charlie.mkv", TMDbID: 103, ScrapeStatus: "pending"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.EnqueueCatalogJob(t.Context(), "tmdb", model.MetadataKindMovie, "999"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		scraper.WaitCatalogHydrationWorker()
	})
	scraper.StartCatalogHydrationWorker(ctx)
	select {
	case <-allSearchesStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("three media workers did not overlap")
	}
	var catalogJob model.CatalogHydrationJob
	if err := repos.DB.Where("external_id = ?", "999").First(&catalogJob).Error; err != nil {
		t.Fatal(err)
	}
	if catalogJob.Status == model.CatalogJobStatusRunning || catalogCalls.Load() != 0 {
		t.Fatalf("catalog started while media was active: status=%s calls=%d", catalogJob.Status, catalogCalls.Load())
	}
	close(releaseSearches)

	deadline := time.Now().Add(10 * time.Second)
	for {
		var matched int64
		if err := repos.DB.Model(&model.Media{}).Where("id IN ? AND scrape_status = ?", []string{rows[0].ID, rows[1].ID, rows[2].ID}, "matched").Count(&matched).Error; err != nil {
			t.Fatal(err)
		}
		if matched == int64(len(rows)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("media workers did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	scraper.WaitCatalogHydrationWorker()

	if maxSearches.Load() != autoMediaScrapeWorkerCount {
		t.Fatalf("max concurrent searches = %d, want %d", maxSearches.Load(), autoMediaScrapeWorkerCount)
	}
	entries := observed.FilterMessage("auto media scrape timing").All()
	if len(entries) != len(rows) {
		t.Fatalf("timing logs = %d, want %d", len(entries), len(rows))
	}
	for _, entry := range entries {
		fields := entry.ContextMap()
		total, ok := fields["total_ms"].(int64)
		if !ok || total < 0 {
			t.Fatalf("invalid total_ms: %#v", fields["total_ms"])
		}
		for _, key := range []string{"candidate_generation_ms", "provider_lookup_ms", "metadata_persist_ms", "artwork_ms", "tmdb_extended_details_ms", "total_ms"} {
			value, ok := fields[key].(int64)
			if !ok {
				t.Fatalf("timing log missing %s: %#v", key, fields)
			}
			if value < 0 || value > total {
				t.Fatalf("timing %s=%d outside [0,%d]", key, value, total)
			}
		}
		for _, forbidden := range []string{"path", "url", "api_key"} {
			if _, ok := fields[forbidden]; ok {
				t.Fatalf("timing log contains forbidden field %s", forbidden)
			}
		}
	}
}

func assertMetadataDetails(t *testing.T, repos interface {
	FindByIdentifier(context.Context, string, string, string) (*model.MetadataItem, error)
}, kind, externalID, languages, countries, genres string) {
	t.Helper()
	metadata, err := repos.FindByIdentifier(t.Context(), "tmdb", kind, externalID)
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil {
		t.Fatalf("metadata %s/%s not found", kind, externalID)
	}
	if metadata.Languages != languages || metadata.Countries != countries || metadata.Genres != genres {
		t.Fatalf("metadata details = %q/%q/%q, want %q/%q/%q", metadata.Languages, metadata.Countries, metadata.Genres, languages, countries, genres)
	}
}
