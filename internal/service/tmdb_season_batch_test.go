package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTMDbSeasonBatchRecheckOversizedResponseDoesNotRefetch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprintf(w, `{"id":50,"season_number":1,"episodes":[],"extra":"%s"}`, strings.Repeat("x", 16<<20))
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	ctx := withTMDbRecheckSeason(t.Context())
	for range 2 {
		if _, err := provider.GetTVSeasonDetails(ctx, 42, 1); err == nil || !strings.Contains(err.Error(), "16 MiB") {
			t.Fatalf("oversized response: %v", err)
		}
	}
	if calls.Load() != 1 || tmdbSeasonBatchFromContext(ctx).bytes != 0 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestTMDbSeasonBatchWaiterCancellationAndFailureRetry(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
			http.Error(w, "failed", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"id":50,"season_number":1,"episodes":[]}`)
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	ctx := withTMDbSeasonBatch(t.Context())
	done := make(chan error, 1)
	go func() { _, err := provider.GetTVSeasonDetails(ctx, 42, 1); done <- err }()
	<-started
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err := provider.GetTVSeasonDetails(waitCtx, 42, 1)
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter err=%v", err)
	}
	if err := <-done; !isTMDbHTTPStatus(err, http.StatusBadGateway) {
		t.Fatalf("owner err=%v", err)
	}
	for range 2 {
		if _, err := provider.GetTVSeasonDetails(ctx, 42, 1); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestTMDbSeasonBatchOwnerCancellationAllowsRetry(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"id":50,"season_number":1,"episodes":[]}`)
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	ctx := withTMDbSeasonBatch(t.Context())
	owner, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := provider.GetTVSeasonDetails(owner, 42, 1); done <- err }()
	<-started
	cancel()
	waiter, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	details, err := provider.GetTVSeasonDetails(waiter, 42, 1)
	if err != nil || details == nil || calls.Load() != 2 {
		t.Fatalf("details=%+v err=%v calls=%d", details, err, calls.Load())
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("owner err=%v", err)
	}
}

func TestTMDbSeasonBatchBoundedCacheAndCredentialIsolation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var season int
		_, _ = fmt.Sscanf(r.URL.Path, "/tv/42/season/%d", &season)
		fmt.Fprintf(w, `{"id":%d,"season_number":%d,"episodes":[]}`, season+100, season)
	}))
	defer server.Close()
	ctx := withTMDbSeasonBatch(t.Context())
	provider := newTMDbTestProvider(server.URL)
	for season := 0; season < 34; season++ {
		if _, err := provider.GetTVSeasonDetails(ctx, 42, season); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := provider.GetTVSeasonDetails(ctx, 42, 33); err != nil {
		t.Fatal(err)
	}
	other := newTMDbTestProvider(server.URL)
	other.cfg.Secrets.TMDbAPIKey = "other-test-key"
	if _, err := other.GetTVSeasonDetails(ctx, 42, 33); err != nil {
		t.Fatal(err)
	}
	batch := tmdbSeasonBatchFromContext(ctx)
	if calls.Load() != 35 || len(batch.entries) > 32 || batch.bytes > 16<<20 {
		t.Fatalf("calls=%d entries=%d bytes=%d", calls.Load(), len(batch.entries), batch.bytes)
	}
}

func TestTMDbSeasonBatchUsesOriginalLanguageAndKeepsChinese(t *testing.T) {
	var seasons, singles, shows atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/42":
			shows.Add(1)
			fmt.Fprint(w, `{"id":42,"original_language":"ja"}`)
		case "/tv/42/season/1":
			seasons.Add(1)
			switch r.URL.Query().Get("language") {
			case "zh-CN":
				fmt.Fprint(w, `{"id":50,"season_number":1,"episodes":[{"id":101,"season_number":1,"episode_number":1,"name":"中文名称","overview":"中文简介","still_path":"/first.jpg"},{"id":102,"season_number":1,"episode_number":2,"name":"第 2 集","overview":"","still_path":"/second.jpg","future_field":true}]}`)
			case "ja":
				fmt.Fprint(w, `{"id":50,"season_number":1,"episodes":[{"id":101,"season_number":1,"episode_number":1,"name":"日本語の一","overview":"日本語の説明"},{"id":102,"season_number":1,"episode_number":2,"name":"日本語の二","overview":"第二話の説明"}]}`)
			default:
				t.Errorf("unexpected language %q", r.URL.Query().Get("language"))
			}
		case "/tv/42/season/1/episode/2":
			singles.Add(1)
			fmt.Fprint(w, `{"id":102,"name":"手动刷新","translations":{"translations":[]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	ctx := withTMDbSeasonBatch(t.Context())
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			details, err := provider.GetTVEpisodeDetails(ctx, 42, 1, 2)
			if err != nil || details == nil {
				t.Errorf("details unavailable: %v", err)
				return
			}
			if details.Name != "日本語の二" || details.Overview != "第二話の説明" || !strings.HasSuffix(details.CatalogStillURL, "/second.jpg") || !strings.Contains(string(details.RawJSON), "future_field") || len(details.Credits) != 0 {
				t.Errorf("details=%+v", details)
			}
		}()
	}
	workers.Wait()
	first, err := provider.GetTVEpisodeDetails(ctx, 42, 1, 1)
	if err != nil || first == nil || first.Name != "中文名称" || first.Overview != "中文简介" {
		t.Fatalf("Chinese overwritten: %+v %v", first, err)
	}
	missing, err := provider.GetTVEpisodeDetails(ctx, 42, 1, 99)
	if !errors.Is(err, errTMDbEpisodeMissingFromSeason) || missing != nil {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
	if seasons.Load() != 2 || shows.Load() != 1 || singles.Load() != 0 {
		t.Fatalf("seasons=%d shows=%d singles=%d", seasons.Load(), shows.Load(), singles.Load())
	}
	manual, err := provider.GetTVEpisodeDetails(t.Context(), 42, 1, 2)
	if err != nil || manual == nil || manual.Name != "手动刷新" || singles.Load() != 1 {
		t.Fatalf("manual=%+v err=%v", manual, err)
	}
}

func TestTMDbSeasonBatchCompleteChineseNeedsOneRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/tv/42/season/0" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":50,"season_number":0,"episodes":[{"id":101,"season_number":0,"episode_number":1,"name":"特别集","overview":"特别集简介","still_path":null}]}`)
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	for range 2 {
		ctx := withTMDbSeasonBatch(t.Context())
		for range 3 {
			details, err := provider.GetTVEpisodeDetails(ctx, 42, 0, 1)
			if err != nil || details == nil || details.CatalogStillURL != "" {
				t.Fatalf("details=%+v err=%v", details, err)
			}
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}
