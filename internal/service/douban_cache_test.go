package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func newDoubanCacheTestProvider(t *testing.T, transport roundTripFunc) (*DoubanProvider, *repository.Container) {
	t.Helper()
	db := newServiceTestDB(t, &model.Setting{})
	if err := database.EnsureDoubanCatalogSchema(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	provider := NewDoubanProvider(nil)
	provider.setDataRepository(repos, nil)
	provider.dataDelay = 0
	provider.client = &http.Client{Transport: transport}
	return provider, repos
}

func doubanCacheTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestDoubanCacheSharedFreshnessConcurrentAndFailure(t *testing.T) {
	var calls atomic.Int64
	var unavailable atomic.Bool
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if unavailable.Load() {
			return doubanCacheTestResponse(req, 503, `unavailable`), nil
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"tv","title":"剧集","rating":{"value":8.5},"future":{"kept":true}}`), nil
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			match, degraded, err := provider.GetEnrichmentMatchByID(t.Context(), "100", "series")
			if err != nil || match == nil || degraded {
				t.Errorf("fetch %#v %v %v", match, degraded, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent calls=%d", calls.Load())
	}
	stored, err := repos.Douban.Find(t.Context(), "100", "series")
	if err != nil || stored == nil || stored.Rating != 8.5 || !strings.Contains(*stored.Payload, "kept") {
		t.Fatalf("stored %#v %v", stored, err)
	}
	// 新 Provider 仍能复用数据库缓存，模拟进程内缓存丢失。
	restarted := NewDoubanProvider(nil)
	restarted.setDataRepository(repos, nil)
	restarted.dataDelay = 0
	restarted.client = provider.client
	if _, _, err := restarted.GetEnrichmentMatchByID(t.Context(), "100", "series"); err != nil || calls.Load() != 1 {
		t.Fatalf("restart cache %d %v", calls.Load(), err)
	}
	if _, _, err := provider.GetEnrichmentMatchByID(WithDoubanFresh(t.Context()), "100", "series"); err != nil || calls.Load() != 2 {
		t.Fatalf("forced cache %d %v", calls.Load(), err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Microsecond)
	if err := repos.DB.Model(&model.DoubanSubject{}).Where("douban_id = ?", "100").Update("fetched_at", old).Error; err != nil {
		t.Fatal(err)
	}
	unavailable.Store(true)
	for range 2 {
		if _, _, err := provider.GetEnrichmentMatchByID(t.Context(), "100", "series"); !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
			t.Fatalf("failure %v", err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("retry ignored %d", calls.Load())
	}
	stored, err = repos.Douban.Find(t.Context(), "100", "series")
	if err != nil || stored.Rating != 8.5 || !stored.FetchedAt.Equal(old) || stored.NextRetryAt == nil {
		t.Fatalf("failure destroyed data %#v %v", stored, err)
	}
	if _, _, err := provider.GetEnrichmentMatchByID(WithDoubanFresh(t.Context()), "100", "series"); !errors.Is(err, ErrDoubanTemporarilyUnavailable) || calls.Load() != 4 {
		t.Fatalf("forced failure %d %v", calls.Load(), err)
	}
}

func TestDoubanCacheSubjectDoesNotReplaceDetailAndWaitCancels(t *testing.T) {
	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		n := calls.Add(1)
		if strings.Contains(req.URL.Path, "/subject/") {
			return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"条目"}`), nil
		}
		if n == 2 {
			close(entered)
			<-release
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"详情","intro":"完整简介","rating":8.2}`), nil
	})
	if _, _, err := provider.bindingSubject(t.Context(), "100"); err != nil {
		t.Fatal(err)
	}
	partial, _ := repos.Douban.Find(t.Context(), "100", "movie")
	if partial == nil || partial.Quality != model.DoubanQualityPartial {
		t.Fatalf("subject quality %#v", partial)
	}
	done := make(chan error, 1)
	go func() {
		_, err := provider.GetMatchByID(t.Context(), "100")
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := provider.GetEnrichmentMatchByID(ctx, "100", "movie"); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait cancel %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, _ := repos.Douban.Find(t.Context(), "100", "movie")
	if stored == nil || stored.Quality != model.DoubanQualityComplete || calls.Load() != 2 {
		t.Fatalf("detail %#v calls %d", stored, calls.Load())
	}
	// subject 搜索可复用含已验证类型的完整详情。
	if _, _, err := provider.bindingSubject(t.Context(), "100"); err != nil || calls.Load() != 2 {
		t.Fatalf("type cache %d %v", calls.Load(), err)
	}
}

func TestDoubanQueryCachePersistsAndRejectsErrorObjects(t *testing.T) {
	var calls atomic.Int64
	var invalid atomic.Bool
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if invalid.Load() {
			return doubanCacheTestResponse(req, 200, `{"code":1000,"msg":"restricted"}`), nil
		}
		return doubanCacheTestResponse(req, 200, `{"subjects":[{"id":"100","title":"电影","rate":"8.5"}]}`), nil
	})
	for range 2 {
		items, err := provider.Discover(t.Context(), "douban_hot_movie")
		if err != nil || len(items) != 1 {
			t.Fatalf("discover %v %v", items, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("query calls=%d", calls.Load())
	}
	invalid.Store(true)
	if _, err := provider.Discover(WithDoubanFresh(t.Context()), "douban_hot_movie"); !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
		t.Fatalf("error object accepted %v", err)
	}
	if items, err := provider.Discover(t.Context(), "douban_hot_movie"); err != nil || len(items) != 1 || calls.Load() != 2 {
		t.Fatalf("valid cache replaced %v %v", items, err)
	}
	expired := time.Now().Add(-time.Hour)
	if err := repos.DB.Model(&model.DoubanQueryCache{}).Where("1 = 1").Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	invalid.Store(false)
	if _, err := provider.Discover(t.Context(), "douban_hot_movie"); err != nil || calls.Load() != 3 {
		t.Fatalf("expiration %d %v", calls.Load(), err)
	}
}

func TestDoubanDataRefreshOnlyChangesSharedData(t *testing.T) {
	var calls atomic.Int64
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/subject/100/" {
			return doubanCacheTestResponse(req, 200, doubanRichFixture), nil
		}
		calls.Add(1)
		if req.URL.Path != "/rexxar/api/v2/tv/100" {
			t.Errorf("wrong cross-kind endpoint %s", req.URL.Path)
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"tv","title":"新标题","intro":"新简介","rating":{"value":9.2},"pic":{"large":"https://image.test/poster.jpg"}}`), nil
	})
	item := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "本地标题", Overview: "本地简介", Rating: 6}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", DoubanEntityKind: "series", ExternalID: "100"})
	item, _ = repos.Metadata.FindByID(t.Context(), item.ID)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if err := repos.Douban.Save(t.Context(), "100", "series", []byte(`{"title":"旧详情","rating":8}`), model.DoubanQualityComplete, old); err != nil {
		t.Fatal(err)
	}
	cache := NewRuntimeCacheService(nil, nil)
	provider.cache = cache
	cache.SetJSON(t.Context(), "media:test", map[string]string{"old": "old"}, time.Hour)
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	scraper := &ScraperService{repo: repos, douban: provider, tasks: tracker}
	for range 2 {
		if err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerManual); err != nil {
			t.Fatal(err)
		}
	}
	history := tracker.Snapshot().Recent
	if len(history) != 2 {
		t.Fatalf("refresh history %#v", history)
	}
	for _, execution := range history {
		if execution.System != model.TaskSystemDouban || execution.Status != TaskStatusCompleted || taskDefinitionKeyForTask(execution) != TaskDefinitionDoubanDataRefresh {
			t.Fatalf("wrong history definition %#v", execution)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls %d", calls.Load())
	}
	current, err := repos.Metadata.FindByID(t.Context(), item.ID)
	if err != nil || current.Title != "新标题" || current.Overview != "新简介" || current.Rating != 0 || !current.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatalf("local metadata changed %#v %v", current, err)
	}
	ratings, err := repos.Metadata.DoubanRatingsByMetadataIDs(t.Context(), []string{item.ID})
	if err != nil || ratings[item.ID] != 9.2 {
		t.Fatalf("refreshed rating %v %v", ratings, err)
	}
	var cached map[string]string
	if cache.GetJSON(t.Context(), "media:test", &cached) {
		t.Fatal("display cache not invalidated")
	}
	// 旧刷新定义只保留历史查询，不再提供调度入口。
	spec, ok := taskDefinitionSpecForKey(TaskDefinitionDoubanDataRefresh)
	if !ok || spec.schedulerJob != "" || spec.filter.Name == "豆瓣电影信息补齐" {
		t.Fatalf("definition %#v", spec)
	}
}

func TestDoubanDetailSurvivesPosterFailureAndEnrichmentReusesIt(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	provider := NewDoubanProvider(nil)
	provider.setDataRepository(repos, nil)
	provider.dataDelay = 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return doubanCacheTestResponse(req, 200, `{"title":"中文标题","intro":"简介","rating":8.5,"pic":{"large":"https://image.test/poster.jpg"}}`), nil
	})}
	scraper.douban = provider
	item := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "English"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", ExternalID: "100"})
	var imageFail atomic.Bool
	imageFail.Store(true)
	imageData := testArtworkPNG(t, 4, 3)
	scraper.artwork.imageProxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if imageFail.Load() {
			return nil, fmt.Errorf("image unavailable")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader(string(imageData))), Request: req}, nil
	})}
	if _, err := scraper.enrichMovieFromDoubanMobile(t.Context(), item.ID); err == nil {
		t.Fatal("poster failure accepted")
	}
	subject, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || subject == nil || subject.Rating != 8.5 {
		t.Fatalf("detail lost %#v %v", subject, err)
	}
	fetched := *subject.FetchedAt
	candidates, err := repos.Metadata.ListDoubanMovieEnrichmentAfter(t.Context(), "", time.Now().Add(-24*time.Hour), 100)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("fresh data suppressed local gaps: %v %v", candidates, err)
	}
	imageFail.Store(false)
	result, err := scraper.enrichMovieFromDoubanMobile(t.Context(), item.ID)
	if err != nil || !result.CandidateSaved || result.Requested || calls.Load() != 1 {
		t.Fatalf("reuse result %#v calls %d err %v", result, calls.Load(), err)
	}
	subject, err = repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || !subject.FetchedAt.Equal(fetched) {
		t.Fatalf("local completion advanced fetch time %#v %v", subject, err)
	}
}

func TestDoubanPermissionFallbackSharesRequestPacing(t *testing.T) {
	var requests []time.Time
	provider, _ := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		requests = append(requests, time.Now())
		if strings.Contains(req.URL.Path, "/movie/") {
			return doubanCacheTestResponse(req, 403, "forbidden"), nil
		}
		return doubanCacheTestResponse(req, 200, `{"title":"降级详情"}`), nil
	})
	provider.dataDelay = 25 * time.Millisecond
	if _, degraded, err := provider.GetEnrichmentMatchByID(t.Context(), "100", "movie"); err != nil || !degraded {
		t.Fatalf("fallback %v %v", degraded, err)
	}
	if len(requests) != 2 || requests[1].Sub(requests[0]) < 15*time.Millisecond {
		t.Fatalf("unpaced fallback: %v", requests)
	}
}

func TestDoubanQueryCacheNormalizesParameters(t *testing.T) {
	var calls atomic.Int64
	provider, _ := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return doubanCacheTestResponse(req, 200, `{"subjects":[]}`), nil
	})
	for _, address := range []string{"https://movie.douban.com/j/search_subjects?type=movie&tag=%E7%83%AD%E9%97%A8", "https://movie.douban.com/j/search_subjects?tag=%E7%83%AD%E9%97%A8&type=movie"} {
		if _, _, err := provider.cachedQuery(t.Context(), address, "https://movie.douban.com/"); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("equivalent query fetched %d times", calls.Load())
	}
}

func TestDoubanFreshDegradedDataNeedsNoCopyTask(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		t.Errorf("fresh degraded data unexpectedly fetched %s", req.URL.Path)
		return nil, errors.New("unexpected request")
	})
	createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "中文标题"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", ExternalID: "100"})
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"title":"降级资料","intro":"可补齐的简介"}`), model.DoubanQualityDegraded, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	candidates, err := repos.Metadata.ListDoubanMovieEnrichmentAfter(t.Context(), "", time.Now().Add(-24*time.Hour), 100)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("fresh degraded data suppressed local gaps: %v %v", candidates, err)
	}
	if match, degraded, err := provider.GetEnrichmentMatchByID(t.Context(), "100", "movie"); err != nil || !degraded || match.Overview != "可补齐的简介" {
		t.Fatalf("cached completion: %#v %v %v", match, degraded, err)
	}
}

func TestDoubanLateLocalCompletionDoesNotOverwriteRefreshedSubject(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	provider.setDataRepository(repos, nil)
	scraper.douban = provider
	item := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "中文标题"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", ExternalID: "100"})
	oldRaw := []byte(`{"title":"中文标题","intro":"先前获取的简介","rating":8}`)
	old := time.Now().UTC().Add(-time.Hour)
	if err := repos.Douban.Save(t.Context(), "100", "movie", oldRaw, model.DoubanQualityComplete, old); err != nil {
		t.Fatal(err)
	}
	details, err := doubanMatchFromRawJSON("100", oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	details.DoubanFetchedAt = old
	fresh := time.Now().UTC().Truncate(time.Microsecond)
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"title":"中文标题","intro":"新获取的简介","rating":9}`), model.DoubanQualityComplete, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := scraper.enrichMovieFromDoubanDetails(t.Context(), item.ID, details); err != nil {
		t.Fatal(err)
	}
	stored, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || stored == nil || stored.Rating != 9 || stored.FetchedAt == nil || !stored.FetchedAt.Equal(fresh) || stored.Payload == nil || !strings.Contains(*stored.Payload, "新获取") {
		t.Fatalf("late local completion overwrote refresh: %#v %v", stored, err)
	}
}

func TestDoubanSeriesPersistenceKeepsOriginalFetchingTime(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	provider.setDataRepository(repos, nil)
	provider.dataDelay = 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return doubanCacheTestResponse(req, 200, `{"title":"剧集资料","rating":8.1,"episodes_count":12}`), nil
	})}
	scraper.douban = provider
	match, err := provider.GetMatchByID(t.Context(), "100")
	if err != nil {
		t.Fatal(err)
	}
	match.MediaType = "tv"
	lib := model.Library{Name: "Series", Path: "/series", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "series", Title: "剧集资料", Source: "douban"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "series", ExternalID: "100"})
	result, err := scraper.persistProviderMetadata(t.Context(), &model.Media{MetadataID: series.ID, LibraryID: lib.ID, Path: "/series/show.mkv"}, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := repos.Douban.FindForMetadata(t.Context(), result.Target.ID)
	if err != nil || subject == nil || subject.EntityKind != "series" || subject.Rating != 8.1 || !subject.FetchedAt.Equal(match.DoubanFetchedAt.Truncate(time.Microsecond)) {
		t.Fatalf("series subject %#v %v", subject, err)
	}
}

func TestDoubanSeriesLatePersistencePreservesNewerFailure(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	var calls atomic.Int64
	provider := NewDoubanProvider(nil)
	provider.setDataRepository(repos, nil)
	provider.dataDelay = 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return doubanCacheTestResponse(req, 200, `{"title":"剧集资料","rating":8.1,"episodes_count":12}`), nil
		}
		if req.URL.Path != "/rexxar/api/v2/tv/100" {
			t.Errorf("wrong series endpoint: %s", req.URL.Path)
		}
		return doubanCacheTestResponse(req, 503, "unavailable"), nil
	})}
	scraper.douban = provider
	match, err := provider.GetMatchByID(t.Context(), "100")
	if err != nil {
		t.Fatal(err)
	}
	match.MediaType = "tv"
	lib := model.Library{Name: "Series", Path: "/series", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "series", Title: "剧集资料", Source: "douban"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "series", ExternalID: "100"})
	if _, _, err := provider.GetEnrichmentMatchByID(WithDoubanFresh(t.Context()), "100", "series"); !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
		t.Fatalf("expected upstream failure: %v", err)
	}
	failed, err := repos.Douban.Find(t.Context(), "100", "series")
	if err != nil || failed == nil || failed.LastAttemptAt == nil || failed.NextRetryAt == nil {
		t.Fatalf("failure state not recorded: %#v %v", failed, err)
	}
	result, err := scraper.persistProviderMetadata(t.Context(), &model.Media{MetadataID: series.ID, LibraryID: lib.ID, Path: "/series/show.mkv"}, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repos.Douban.FindForMetadata(t.Context(), result.Target.ID)
	if err != nil || stored == nil || stored.EntityKind != "series" || stored.Payload == nil || stored.Rating != 8.1 || stored.FetchedAt == nil || !stored.FetchedAt.Equal(match.DoubanFetchedAt.Truncate(time.Microsecond)) {
		t.Fatalf("late details not preserved: %#v %v", stored, err)
	}
	if stored.LastAttemptAt == nil || !stored.LastAttemptAt.Equal(*failed.LastAttemptAt) || stored.NextRetryAt == nil || !stored.NextRetryAt.Equal(*failed.NextRetryAt) {
		t.Fatalf("late persistence cleared failure: %#v", stored)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected upstream requests: %d", calls.Load())
	}
}

func TestDoubanDataRefreshUpstreamFailureIsFailed(t *testing.T) {
	var calls atomic.Int64
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return doubanCacheTestResponse(req, 503, "unavailable"), nil
	})
	for _, id := range []string{"100", "101"} {
		createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "本地标题"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", ExternalID: id})
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Microsecond)
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"title":"旧资料","rating":8}`), model.DoubanQualityComplete, old); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	scraper := &ScraperService{repo: repos, douban: provider, tasks: tracker}
	err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerScheduled)
	if !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
		t.Errorf("refresh swallowed upstream failure: %v", err)
	}
	snapshot := tracker.Snapshot()
	if len(snapshot.Active) != 0 || len(snapshot.Recent) != 1 {
		t.Fatalf("refresh history: %#v", snapshot)
	}
	execution := snapshot.Recent[0]
	if execution.Status != TaskStatusFailed || execution.Stage != "failed" || execution.Error == "" || execution.Trigger != TaskTriggerScheduled || taskDefinitionKeyForTask(execution) != TaskDefinitionDoubanDataRefresh {
		t.Errorf("upstream failure reported as success: %#v", execution)
	}
	if execution.Metrics["failed"] != 1 || execution.Metrics["upstream_paused"] != 1 || execution.Metrics["requested"] != 1 || execution.Metrics["scanned"] != 1 || execution.Metrics["updated"] != 0 || calls.Load() != 1 {
		t.Errorf("batch did not stop at upstream failure: %#v calls=%d", execution.Metrics, calls.Load())
	}
	stored, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || stored == nil || stored.Payload == nil || stored.Rating != 8 || stored.FetchedAt == nil || !stored.FetchedAt.Equal(old) || stored.NextRetryAt == nil || stored.LastAttemptAt == nil || !stored.NextRetryAt.Equal(stored.LastAttemptAt.Add(5*time.Minute)) {
		t.Fatalf("refresh failure lost old data or backoff: %#v %v", stored, err)
	}
}

func TestDoubanDataRefreshIncludesUnboundSubjects(t *testing.T) {
	var calls atomic.Int64
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Path == "/subject/100/" {
			return doubanCacheTestResponse(req, 200, doubanRichFixture), nil
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"未关联作品","rating":{"value":9}}`), nil
	})
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","title":"旧资料"}`), model.DoubanQualityComplete, time.Now().Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repos, douban: provider}
	for range 2 {
		if err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerManual); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || stored == nil || stored.Rating != 9 || stored.RichFetchedAt != nil || calls.Load() != 1 {
		t.Fatalf("unbound refresh/fresh cache: %#v calls=%d err=%v", stored, calls.Load(), err)
	}
}

func TestDoubanDataRefreshCandidateFreshnessAndLimit(t *testing.T) {
	db := newServiceTestDB(t, &model.DoubanSubject{})
	repos := repository.New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	old, yesterday, future := now.Add(-8*24*time.Hour), now.Add(-25*time.Hour), now.Add(time.Hour)
	rows := []model.DoubanSubject{
		{DoubanID: "100", EntityKind: "movie", Quality: model.DoubanQualityComplete, FetchedAt: &old, RichFetchedAt: &old},
		{DoubanID: "100", EntityKind: "series", Quality: model.DoubanQualityComplete, FetchedAt: &old, RichFetchedAt: &old},
		{DoubanID: "101", EntityKind: "movie", Quality: model.DoubanQualityComplete, FetchedAt: &now, RichFetchedAt: &now},
		{DoubanID: "102", EntityKind: "movie", Quality: model.DoubanQualityComplete, FetchedAt: &old, NextRetryAt: &future},
		{DoubanID: "103", EntityKind: "movie", Quality: model.DoubanQualityPartial, FetchedAt: &now},
		{DoubanID: "104", EntityKind: "series", Quality: model.DoubanQualityDegraded, FetchedAt: &yesterday},
		{DoubanID: "105", EntityKind: "series", Quality: model.DoubanQualityDegraded, FetchedAt: &now},
		{DoubanID: "106", EntityKind: "movie"},
		{DoubanID: "107", EntityKind: "episode", FetchedAt: &old},
		{DoubanID: "108", EntityKind: "movie", Quality: model.DoubanQualityComplete, FetchedAt: &now},
		{DoubanID: "109", EntityKind: "movie", Quality: model.DoubanQualityComplete, FetchedAt: &now, RichNextRetryAt: &future},
	}
	payload := `{"title":"已保存资料"}`
	for i := range rows {
		if rows[i].FetchedAt != nil {
			rows[i].Payload = &payload
		}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	before := now.Add(-7 * 24 * time.Hour)
	page, err := repos.Douban.ListRefreshCandidates(t.Context(), before, now, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range page {
		got = append(got, row.DoubanID+"/"+row.EntityKind)
	}
	if joined := strings.Join(got, ","); joined != "106/movie,100/movie,100/series,104/series,103/movie" {
		t.Fatalf("refresh candidates: %s", joined)
	}
	limited, err := repos.Douban.ListRefreshCandidates(t.Context(), before, now, 2)
	count, countErr := repos.Douban.CountRefreshCandidates(t.Context(), before, now)
	if err != nil || countErr != nil || count != 5 || len(limited) != 2 || limited[0].DoubanID != "106" || limited[1].DoubanID != "100" {
		t.Fatalf("oldest-first limit/count: %#v %d %v %v", limited, count, err, countErr)
	}
}
