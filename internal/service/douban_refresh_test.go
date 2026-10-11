package service

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestDoubanDataRefreshBoundedOldestFirstAndPreservesSupplement(t *testing.T) {
	var paths []string
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		id := strings.TrimPrefix(req.URL.Path, "/rexxar/api/v2/movie/")
		if id == req.URL.Path {
			t.Fatalf("task requested non-base endpoint: %s", req.URL.Path)
		}
		rating := 8
		if id == "100" {
			rating = 9
		}
		return doubanCacheTestResponse(req, 200, fmt.Sprintf(`{"id":%q,"type":"movie","title":"资料","rating":%d}`, id, rating)), nil
	})
	if err := repos.Setting.Set(t.Context(), doubanDataRefreshCountKey, "2"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	for i, id := range []string{"300", "100", "200"} {
		if err := repos.Douban.Save(t.Context(), id, "movie", []byte(fmt.Sprintf(`{"id":%q,"type":"movie","title":"资料","rating":8}`, id)), model.DoubanQualityComplete, old.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	rich, retry := `{"writers":["旧编剧"],"credits":[{"name":"演员","image_key":"local-avatar"}]}`, old.Add(20*24*time.Hour)
	if err := repos.DB.Model(&model.DoubanSubject{}).Where("douban_id = ?", "300").Updates(map[string]any{"rich_payload": rich, "rich_fetched_at": old, "rich_next_retry_at": retry, "poster_source_url": "https://img.doubanio.com/view/photo/l/public/test.jpg"}).Error; err != nil {
		t.Fatal(err)
	}
	before, _ := repos.Douban.Find(t.Context(), "300", "movie")
	tracker := NewTaskTrackerService(nil, nil)
	scraper := &ScraperService{repo: repos, douban: provider, tasks: tracker}
	if err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerScheduled); err != nil {
		t.Fatal(err)
	}
	first := tracker.Snapshot().Recent[0]
	for key, want := range map[string]int64{"total": 3, "selected": 2, "processed": 2, "changed": 1, "unchanged": 1, "remaining": 1, "requested": 2, "updated": 2, "failed": 0, "skipped": 0} {
		if first.Metrics[key] != want {
			t.Fatalf("%s: got=%d want=%d (%#v)", key, first.Metrics[key], want, first)
		}
	}
	after, _ := repos.Douban.Find(t.Context(), "300", "movie")
	if !reflect.DeepEqual(before.RichPayload, after.RichPayload) || !reflect.DeepEqual(before.RichFetchedAt, after.RichFetchedAt) || !reflect.DeepEqual(before.RichNextRetryAt, after.RichNextRetryAt) || before.PosterSourceURL != after.PosterSourceURL || !after.FetchedAt.After(*before.FetchedAt) {
		t.Fatal("base refresh touched supplement/image or failed to advance unchanged check")
	}
	for range 2 {
		if err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerManual); err != nil {
			t.Fatal(err)
		}
	}
	if joined := strings.Join(paths, ","); joined != "/rexxar/api/v2/movie/300,/rexxar/api/v2/movie/100,/rexxar/api/v2/movie/200" {
		t.Fatalf("cap/order/freshness: %s", joined)
	}
	last := tracker.Snapshot().Recent[0]
	if last.Metrics["requested"] != 0 || last.Metrics["selected"] != 0 || last.Metrics["remaining"] != 0 {
		t.Fatalf("fresh base retried because of supplement: %#v", last)
	}
}

func TestDoubanDataRefreshPermissionDenialDoesNotFallback(t *testing.T) {
	var paths []string
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		return doubanCacheTestResponse(req, 403, `{"msg":"permission denied"}`), nil
	})
	if err := repos.Douban.RecordFailure(t.Context(), "100", "series", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerService(nil, nil)
	scraper := &ScraperService{repo: repos, douban: provider, tasks: tracker}
	if err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerManual); !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
		t.Fatalf("denial: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{"/rexxar/api/v2/tv/100"}) {
		t.Fatalf("unexpected fallback: %v", paths)
	}
	row, _ := repos.Douban.Find(t.Context(), "100", "series")
	if row.Payload != nil || row.NextRetryAt == nil || !row.NextRetryAt.After(time.Now()) {
		t.Fatalf("denial replaced data or lost retry: %#v", row)
	}
}

func TestDoubanDataRefreshConcurrentNewerSaveIsSkipped(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, nil)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","type":"movie","title":"旧资料"}`), model.DoubanQualityComplete, old); err != nil {
		t.Fatal(err)
	}
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","type":"movie","title":"更新资料"}`), model.DoubanQualityComplete, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"迟到资料"}`), nil
	})}
	tracker := NewTaskTrackerService(nil, nil)
	if err := (&ScraperService{repo: repos, douban: provider, tasks: tracker}).runDoubanDataRefresh(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	metrics := tracker.Snapshot().Recent[0].Metrics
	if metrics["skipped"] != 1 || metrics["updated"] != 0 || metrics["changed"] != 0 || metrics["requested"] != 1 {
		t.Fatalf("rejected write counted as update: %v", metrics)
	}
}

func TestDoubanBaseRefreshRechecksSharedCache(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		t.Fatal("fresh cache requested upstream")
		return nil, nil
	})
	release, err := provider.enterDataRequest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan doubanDetailObservation, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		outcome, err := provider.refreshBaseDetail(t.Context(), "100", "movie")
		if err != nil {
			t.Error(err)
		}
		result <- outcome
	}()
	<-started
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","type":"movie","title":"资料"}`), model.DoubanQualityComplete, time.Now()); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if outcome := <-result; !outcome.Cached || outcome.Saved {
		t.Fatalf("cache outcome: %#v", outcome)
	}
}

func TestDoubanBaseRefreshUsesConfiguredTTL(t *testing.T) {
	calls := 0
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls++
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"资料"}`), nil
	})
	if err := repos.Setting.Set(t.Context(), "metadata.douban_detail_ttl_seconds", "3600"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","type":"movie","title":"资料"}`), model.DoubanQualityComplete, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repos, douban: provider}
	for range 2 {
		if err := scraper.runDoubanDataRefresh(t.Context(), TaskTriggerManual); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("task and provider disagree on TTL: %d", calls)
	}
}

func TestDoubanBaseRefreshRechecksConcurrentRetry(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		t.Fatal("deferred base requested upstream")
		return nil, nil
	})
	if err := repos.Douban.RecordFailure(t.Context(), "100", "movie", time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	outcome, err := provider.refreshBaseDetail(t.Context(), "100", "movie")
	if err != nil || !outcome.Deferred || outcome.Saved || outcome.Cached {
		t.Fatalf("concurrent retry outcome: %#v err=%v", outcome, err)
	}
}
