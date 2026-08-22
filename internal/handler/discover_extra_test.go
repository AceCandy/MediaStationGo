package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestDiscoverProviderEnabledHonorsAPIConfigToggle(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	apiConfig := service.NewAPIConfigService(zap.NewNop(), repos, service.NewCryptoService("", zap.NewNop()))
	enabled := false
	if _, err := apiConfig.Update(t.Context(), "douban", service.APIConfigPatch{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{APIConfig: apiConfig}

	if discoverProviderEnabled(t.Context(), svc, "douban") {
		t.Fatal("disabled API config should disable discover provider")
	}
	if !discoverProviderEnabled(t.Context(), svc, "missing-provider") {
		t.Fatal("missing API config should keep discover provider available")
	}
}

func TestDiscoverFetchFailureLogIncludesDiagnostics(t *testing.T) {
	core, observed := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	logDiscoverFetchFailed(
		&service.Container{Log: logger},
		"tmdb_latest_movie",
		2,
		1500*time.Millisecond,
		discoverSectionTimeout("tmdb_latest_movie"),
		context.DeadlineExceeded,
	)

	entries := observed.FilterMessage("discover section fetch failed").All()
	if len(entries) != 1 {
		t.Fatalf("expected one failure log entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["section"] != "tmdb_latest_movie" || fields["provider"] != "tmdb" {
		t.Fatalf("unexpected section/provider fields: %#v", fields)
	}
	if fields["page"] != int64(2) && fields["page"] != 2 {
		t.Fatalf("page field missing or wrong: %#v", fields["page"])
	}
	if fields["duration_ms"] != int64(1500) && fields["duration_ms"] != 1500 {
		t.Fatalf("duration_ms field missing or wrong: %#v", fields["duration_ms"])
	}
	if _, ok := fields["timeout"]; !ok {
		t.Fatalf("timeout field missing: %#v", fields)
	}
}

func TestDiscoverSectionTimeoutRaisesBangumiBudget(t *testing.T) {
	if got := discoverSectionTimeout("bangumi_calendar"); got != discoverFeedBangumiTimeout {
		t.Fatalf("bangumi timeout = %s, want %s", got, discoverFeedBangumiTimeout)
	}
	if got := discoverSectionTimeout("tmdb_latest_movie"); got != discoverFeedSectionTimeout {
		t.Fatalf("tmdb timeout = %s, want %s", got, discoverFeedSectionTimeout)
	}
}

func TestRunDiscoverProviderGroupsSerializesProvidersAndRunsGroupsInParallel(t *testing.T) {
	jobs := []discoverSectionJob{
		{index: 0, key: "tmdb-1", provider: "tmdb"},
		{index: 1, key: "tmdb-2", provider: "tmdb"},
		{index: 2, key: "douban-1", provider: "douban"},
		{index: 3, key: "douban-2", provider: "douban"},
		{index: 4, key: "bangumi-1", provider: "bangumi"},
	}
	var mu sync.Mutex
	active := map[string]int{}
	maxByProvider := map[string]int{}
	activeTotal := 0
	maxTotal := 0
	results := runDiscoverProviderGroups(t.Context(), jobs, 2, func(ctx context.Context, job discoverSectionJob) discoverSectionResult {
		mu.Lock()
		active[job.provider]++
		if active[job.provider] > maxByProvider[job.provider] {
			maxByProvider[job.provider] = active[job.provider]
		}
		activeTotal++
		if activeTotal > maxTotal {
			maxTotal = activeTotal
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active[job.provider]--
		activeTotal--
		mu.Unlock()
		return discoverSectionResult{index: job.index, key: job.key}
	})

	if len(results) != len(jobs) {
		t.Fatalf("got %d results, want %d", len(results), len(jobs))
	}
	if maxByProvider["tmdb"] != 1 || maxByProvider["douban"] != 1 {
		t.Fatalf("same-provider jobs overlapped: max=%v", maxByProvider)
	}
	if maxTotal < 2 {
		t.Fatalf("different providers did not run in parallel: max total=%d", maxTotal)
	}
}

func TestDiscoverSectionCacheHitSkipsProvider(t *testing.T) {
	svc, calls := newDiscoverTMDbTestService(t, http.StatusOK, "fresh")
	svc.Discover.RememberSection("tmdb_popular_movie", 1, []service.ExternalMediaResult{{Title: "cached"}})

	result := loadDiscoverSection(t.Context(), svc, discoverSectionJob{
		key: "tmdb_popular_movie", provider: "tmdb", page: 1,
	})

	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", calls.Load())
	}
	if len(result.items) != 1 || result.items[0].Title != "cached" {
		t.Fatalf("items = %#v", result.items)
	}
}

func TestDiscoverSectionsMixedCacheHitStillLoadsMissesInOrder(t *testing.T) {
	svc, calls := newDiscoverTMDbTestService(t, http.StatusOK, "fresh")
	svc.Discover.RememberSection("tmdb_popular_movie", 1, []service.ExternalMediaResult{{Title: "cached"}})

	results := loadDiscoverSections(t.Context(), svc, []string{
		"tmdb_popular_movie",
		"tmdb_top_rated_movie",
	}, 1, false)

	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
	if len(results) != 2 || results[0].key != "tmdb_popular_movie" || results[1].key != "tmdb_top_rated_movie" {
		t.Fatalf("results order = %#v", results)
	}
	if len(results[0].items) != 1 || results[0].items[0].Title != "cached" || len(results[1].items) != 1 || results[1].items[0].Title != "fresh" {
		t.Fatalf("results items = %#v", results)
	}
}

func TestDiscoverFeedRefreshQueryCallsProviderAndUpdatesCache(t *testing.T) {
	svc, calls := newDiscoverTMDbTestService(t, http.StatusOK, "fresh")
	svc.Discover.RememberSection("tmdb_popular_movie", 1, []service.ExternalMediaResult{{Title: "cached"}})
	request := func(target string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		discoverFeedHandler(svc)(c)
		return recorder
	}
	assertFreshResponse := func(recorder *httptest.ResponseRecorder) {
		t.Helper()
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		var items []service.ExternalMediaResult
		if err := json.Unmarshal(response["tmdb_popular_movie"], &items); err != nil {
			t.Fatal(err)
		}
		var meta map[string]map[string]any
		if err := json.Unmarshal(response["_meta"], &meta); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Title != "fresh" {
			t.Fatalf("items = %#v", items)
		}
		if _, ok := meta["tmdb_popular_movie"]["has_next"]; !ok {
			t.Fatalf("meta = %#v", meta)
		}
	}

	assertFreshResponse(request("/discover/feed?sections=tmdb_popular_movie&refresh=1"))
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
	assertFreshResponse(request("/discover/feed?sections=tmdb_popular_movie"))
	if calls.Load() != 1 {
		t.Fatalf("cached request called provider; calls = %d", calls.Load())
	}
}

func TestDiscoverSectionRefreshFailureReturnsCachedItems(t *testing.T) {
	svc, calls := newDiscoverTMDbTestService(t, http.StatusServiceUnavailable, "")
	cached := make([]service.ExternalMediaResult, 20)
	for i := range cached {
		cached[i].Title = "cached"
	}
	svc.Discover.RememberSection("tmdb_popular_movie", 1, cached)

	result := loadDiscoverSection(t.Context(), svc, discoverSectionJob{
		key: "tmdb_popular_movie", provider: "tmdb", page: 1, refresh: true,
	})

	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
	if len(result.items) != len(cached) || result.meta["stale"] != true || result.meta["has_next"] != true {
		t.Fatalf("result = %#v", result)
	}
	if _, ok := result.meta["fallback"]; ok {
		t.Fatalf("fallback should not run when cache is available: %#v", result.meta)
	}
}

func TestDiscoverSlowFetchLogIncludesSectionTiming(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	logDiscoverFetchSlow(&service.Container{Log: logger}, "douban_hot_movie", 1, discoverFeedSlowSectionThreshold-time.Millisecond, 24)
	if got := observed.FilterMessage("discover section fetch slow").Len(); got != 0 {
		t.Fatalf("fast section should not log, got %d entries", got)
	}

	logDiscoverFetchSlow(&service.Container{Log: logger}, "douban_hot_movie", 1, discoverFeedSlowSectionThreshold, 24)
	entries := observed.FilterMessage("discover section fetch slow").All()
	if len(entries) != 1 {
		t.Fatalf("expected one slow log entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["section"] != "douban_hot_movie" || fields["provider"] != "douban" {
		t.Fatalf("unexpected section/provider fields: %#v", fields)
	}
	if fields["items"] != int64(24) && fields["items"] != 24 {
		t.Fatalf("items field missing or wrong: %#v", fields["items"])
	}
	if _, ok := fields["duration_ms"]; !ok {
		t.Fatalf("duration_ms field missing: %#v", fields)
	}
	if _, ok := fields["slow_threshold"]; !ok {
		t.Fatalf("slow_threshold field missing: %#v", fields)
	}
}

func TestDiscoverFeedErrorMessageHidesTechnicalTimeout(t *testing.T) {
	for _, err := range []error{
		context.DeadlineExceeded,
		errors.New("timeout of 30000ms exceeded"),
	} {
		got := discoverFeedErrorMessage(err)
		if got != "推荐源响应超时，已跳过本次加载" {
			t.Fatalf("message for %q = %q", err, got)
		}
	}
	if got := discoverFeedErrorMessage(errors.New("upstream 503")); got != "推荐源暂时不可用，已跳过本次加载" {
		t.Fatalf("generic message = %q", got)
	}
}

func TestDefaultDiscoverSectionKeysSkipDisabledProviders(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	apiConfig := service.NewAPIConfigService(zap.NewNop(), repos, service.NewCryptoService("", zap.NewNop()))
	disabled := false
	for _, provider := range []string{"douban", "bangumi"} {
		if _, err := apiConfig.Update(t.Context(), provider, service.APIConfigPatch{Enabled: &disabled}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &service.Container{APIConfig: apiConfig}

	keys := defaultDiscoverSectionKeys(t.Context(), svc)
	for _, key := range keys {
		switch discoverSectionProvider(key) {
		case "douban", "bangumi":
			t.Fatalf("disabled provider key %q should not be selected by default; keys=%v", key, keys)
		}
	}
	if len(keys) == 0 {
		t.Fatal("default keys should keep enabled providers")
	}
}

func TestDefaultDiscoverSectionKeysIncludeLatestTMDbRails(t *testing.T) {
	keys := defaultDiscoverSectionKeys(t.Context(), &service.Container{})
	keySet := map[string]struct{}{}
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	for _, key := range []string{"tmdb_latest_movie", "tmdb_latest_tv"} {
		if _, ok := keySet[key]; !ok {
			t.Fatalf("default discover keys should include %q: %v", key, keys)
		}
	}
}

func TestFallbackDiscoverSectionKeyUsesTMDbForDoubanRails(t *testing.T) {
	cases := map[string]string{
		"douban_hot_movie": "tmdb_popular_movie",
		"douban_hot_tv":    "tmdb_popular_tv",
		"douban_top_movie": "tmdb_top_rated_movie",
		"tmdb_latest_tv":   "",
	}
	for key, want := range cases {
		if got := fallbackDiscoverSectionKey(key); got != want {
			t.Fatalf("fallbackDiscoverSectionKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func newDiscoverTMDbTestService(t *testing.T, status int, title string) (*service.Container, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if status >= http.StatusBadRequest {
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"id":1,"title":"`+title+`"}]}`)
	}))
	t.Cleanup(server.Close)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = server.URL
	tmdb := service.NewTMDbProvider(cfg, zap.NewNop(), nil)
	return &service.Container{
		Discover: service.NewDiscoverService(zap.NewNop(), tmdb),
		Log:      zap.NewNop(),
	}, &calls
}
