package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestPlaybackRedirectResolvePrefixes(t *testing.T) {
	raw := strings.Join([]string{
		"",
		"  https://media.example.test/d  ",
		"ftp://media.example.test/d",
		"/relative/d",
		"http://user:secret@media.example.test/d",
	}, "\n")
	prefixes := parsePlaybackRedirectResolvePrefixes(raw)
	if got := strings.Join(prefixes, "|"); got != "https://media.example.test/d" {
		t.Fatalf("prefixes = %q", got)
	}
	if !matchesPlaybackRedirectPrefix("https://media.example.test/d/Movie.mkv", prefixes) {
		t.Fatal("expected literal prefix match")
	}
	if matchesPlaybackRedirectPrefix("HTTPS://media.example.test/d/Movie.mkv", prefixes) {
		t.Fatal("prefix matching must remain case-sensitive")
	}
	if matchesPlaybackRedirectPrefix("/d/Movie.mkv", prefixes) {
		t.Fatal("relative target must not match")
	}
}

func TestPlaybackRedirectResolverCoalescesConcurrentRequests(t *testing.T) {
	resolver := newPlaybackRedirectResolver()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	resolver.client = &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return redirectResponseTransport(http.StatusFound, "/direct/Movie.mkv")(req)
		}),
	}

	type result struct {
		target   string
		cacheHit bool
		err      error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	resolve := func() {
		defer workers.Done()
		target, cacheHit, err := resolver.Resolve(t.Context(), "http://origin.example/d/Movie.mkv", "Player/1")
		results <- result{target: target, cacheHit: cacheHit, err: err}
	}
	workers.Add(1)
	go resolve()
	<-started
	workers.Add(1)
	go resolve()
	time.Sleep(20 * time.Millisecond)
	upstreamCalls := calls.Load()
	close(release)
	workers.Wait()
	close(results)

	if upstreamCalls != 1 || calls.Load() != 1 {
		t.Fatalf("concurrent upstream calls = %d/%d, want 1", upstreamCalls, calls.Load())
	}
	cacheHits := 0
	for got := range results {
		if got.err != nil || got.target != "http://origin.example/direct/Movie.mkv" {
			t.Fatalf("resolve result = %#v", got)
		}
		if got.cacheHit {
			cacheHits++
		}
	}
	if cacheHits != 1 {
		t.Fatalf("concurrent cache hits = %d, want 1", cacheHits)
	}
}

func TestServeFileResolvesAndCachesConfiguredSTRMRedirect(t *testing.T) {
	var sourceCalls atomic.Int32
	var followedCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/d/Movie.mkv":
			sourceCalls.Add(1)
			if r.Method != http.MethodGet || r.Header.Get("Range") != "bytes=0-0" {
				t.Errorf("resolver request = %s Range %q", r.Method, r.Header.Get("Range"))
			}
			if ua := r.UserAgent(); ua != "Player/1" && ua != "player/1" {
				t.Errorf("resolver User-Agent = %q", ua)
			}
			for name := range r.Header {
				if name != "Range" && name != "User-Agent" {
					t.Errorf("unexpected resolver header %q", name)
				}
			}
			w.Header().Set("Location", "/direct/Movie.mkv?sig=resolved-secret")
			w.WriteHeader(http.StatusFound)
		case "/direct/Movie.mkv":
			followedCalls.Add(1)
			w.Header().Set("Location", "https://cdn.example.test/final.mkv")
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	repos := newStreamTestRepo(t)
	if err := repos.Setting.Set(t.Context(), PlaybackRedirectResolvePrefixesSettingKey,
		"\nftp://ignored.example/d\n  "+upstream.URL+"/d  \n"); err != nil {
		t.Fatal(err)
	}
	source := upstream.URL + "/d/Movie.mkv?access=source-secret"
	if err := repos.DB.Create(&model.Media{
		Base: model.Base{ID: "resolved-strm"}, Path: "/media/Movie.strm", Container: "strm", STRMURL: source,
	}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
	now := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	svc.redirectResolver.now = func() time.Time { return now }
	wantLocation := upstream.URL + "/direct/Movie.mkv?sig=resolved-secret"

	for i := 0; i < 2; i++ {
		w := servePlaybackRedirectRequest(t, svc, "resolved-strm", http.MethodGet, "Player/1")
		if w.Code != http.StatusFound || w.Header().Get("Location") != wantLocation {
			t.Fatalf("request %d status/location = %d/%q", i+1, w.Code, w.Header().Get("Location"))
		}
		if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
			t.Fatalf("Cache-Control = %q", got)
		}
	}
	if got := sourceCalls.Load(); got != 1 {
		t.Fatalf("same URL/User-Agent upstream calls = %d, want 1", got)
	}
	if got := followedCalls.Load(); got != 0 {
		t.Fatalf("resolver followed Location %d times", got)
	}

	servePlaybackRedirectRequest(t, svc, "resolved-strm", http.MethodGet, "player/1")
	if got := sourceCalls.Load(); got != 2 {
		t.Fatalf("different User-Agent upstream calls = %d, want 2", got)
	}
	now = now.Add(time.Hour + time.Second)
	servePlaybackRedirectRequest(t, svc, "resolved-strm", http.MethodGet, "Player/1")
	if got := sourceCalls.Load(); got != 3 {
		t.Fatalf("expired cache upstream calls = %d, want 3", got)
	}

	redirects := observed.FilterMessage("media playback redirect").All()
	if len(redirects) != 4 {
		t.Fatalf("redirect logs = %d, want 4", len(redirects))
	}
	if fields := redirects[0].ContextMap(); fields["redirect_resolve_source"] != "upstream" || fields["cache_hit"] != false {
		t.Fatalf("first redirect fields = %#v", fields)
	}
	if fields := redirects[1].ContextMap(); fields["redirect_resolve_source"] != "cache" || fields["cache_hit"] != true {
		t.Fatalf("cached redirect fields = %#v", fields)
	}
	assertRedirectLogsHideValues(t, observed, "source-secret", "resolved-secret", "player-secret")
}

func TestServeFileResolvesMappedURLBeforeRedirect(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Range") != "bytes=0-0" || r.UserAgent() != "SenPlayer/1" {
			t.Errorf("resolver request = %s Range %q UA %q", r.Method, r.Header.Get("Range"), r.UserAgent())
		}
		if r.URL.Path != "/d/archive/Movie.mkv" {
			t.Errorf("mapped resolver path = %q", r.URL.Path)
		}
		w.Header().Set("Location", "https://cdn.example.test/Movie.mkv?token=direct-secret")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	repos := newStreamTestRepo(t)
	if err := repos.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey,
		"/mnt/media-a/ => "+upstream.URL+"/d/\n/mnt/media-b/ => "+upstream.URL+"/d/"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), PlaybackRedirectResolvePrefixesSettingKey,
		upstream.URL+"/d"); err != nil {
		t.Fatal(err)
	}
	media := []struct {
		id   string
		path string
	}{
		{id: "mapped-a", path: "/mnt/media-a/archive/Movie.mkv"},
		{id: "mapped-b", path: "/mnt/media-b/archive/Movie.mkv"},
	}
	for _, item := range media {
		if err := repos.DB.Create(&model.Media{
			Base: model.Base{ID: item.id}, Path: item.path,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
	for _, item := range media {
		w := servePlaybackRedirectRequest(t, svc, item.id, http.MethodHead, "SenPlayer/1")
		if w.Code != http.StatusFound || w.Header().Get("Location") != "https://cdn.example.test/Movie.mkv?token=direct-secret" {
			t.Fatalf("%s status/location = %d/%q", item.id, w.Code, w.Header().Get("Location"))
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("post-mapping URL cache calls = %d, want 1", got)
	}
	redirects := observed.FilterMessage("media playback redirect").All()
	if len(redirects) != 2 || redirects[0].ContextMap()["resolve_source"] != "path_mapping" ||
		redirects[1].ContextMap()["redirect_resolve_source"] != "cache" {
		t.Fatalf("mapped redirect logs = %#v", redirects)
	}
	assertRedirectLogsHideValues(t, observed, "direct-secret")
}

func TestServeFileFallsBackWhenRedirectResolutionFails(t *testing.T) {
	tests := []struct {
		name      string
		timeout   time.Duration
		transport roundTripFunc
	}{
		{
			name:      "non redirect",
			transport: redirectResponseTransport(http.StatusOK, ""),
		},
		{
			name:      "missing location",
			transport: redirectResponseTransport(http.StatusFound, ""),
		},
		{
			name:      "invalid location",
			transport: redirectResponseTransport(http.StatusFound, "ftp://cdn.example/Movie.mkv?token=location-secret"),
		},
		{
			name: "request error",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("transport-secret")
			},
		},
		{
			name:    "timeout",
			timeout: time.Millisecond,
			transport: func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			repos := newStreamTestRepo(t)
			source := "http://origin.example.test/d/Movie.mkv?access=source-secret"
			if err := repos.Setting.Set(t.Context(), PlaybackRedirectResolvePrefixesSettingKey,
				"http://origin.example.test/d"); err != nil {
				t.Fatal(err)
			}
			if err := repos.DB.Create(&model.Media{
				Base: model.Base{ID: "fallback"}, Path: "/media/Movie.strm", Container: "strm", STRMURL: source,
			}).Error; err != nil {
				t.Fatal(err)
			}
			core, observed := observer.New(zap.InfoLevel)
			svc := NewStreamService(&config.Config{}, zap.New(core), repos)
			timeout := tt.timeout
			if timeout == 0 {
				timeout = time.Second
			}
			svc.redirectResolver.client = &http.Client{
				Timeout: timeout,
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls.Add(1)
					return tt.transport(req)
				}),
			}

			for i := 0; i < 2; i++ {
				w := servePlaybackRedirectRequest(t, svc, "fallback", http.MethodGet, "Yamby/1")
				if w.Code != http.StatusFound || w.Header().Get("Location") != source {
					t.Fatalf("request %d status/location = %d/%q", i+1, w.Code, w.Header().Get("Location"))
				}
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("failed resolutions cached: upstream calls = %d, want 2", got)
			}
			redirects := observed.FilterMessage("media playback redirect").All()
			if len(redirects) != 2 || redirects[0].ContextMap()["redirect_resolve_source"] != "fallback" {
				t.Fatalf("fallback redirect logs = %#v", redirects)
			}
			assertRedirectLogsHideValues(t, observed, "source-secret", "location-secret", "transport-secret")
		})
	}
}

func servePlaybackRedirectRequest(t *testing.T, svc *StreamService, mediaID, method, userAgent string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://nas.local/api/stream/"+mediaID, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", "bytes=123-")
	req.Header.Set("Authorization", "Bearer player-secret")
	req.Header.Set("Cookie", "session=player-secret")
	req.Header.Set("X-Test-Secret", "player-secret")
	w := httptest.NewRecorder()
	if err := svc.ServeFile(w, req, mediaID); err != nil {
		t.Fatal(err)
	}
	return w
}

func redirectResponseTransport(status int, location string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		if location != "" {
			header.Set("Location", location)
		}
		return &http.Response{
			StatusCode: status,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}
}

func assertRedirectLogsHideValues(t *testing.T, observed *observer.ObservedLogs, values ...string) {
	t.Helper()
	for _, entry := range observed.All() {
		logged := entry.Message + fmt.Sprint(entry.ContextMap())
		for _, value := range values {
			if strings.Contains(logged, value) {
				t.Fatalf("redirect log leaked %q: %s", value, logged)
			}
		}
	}
}
