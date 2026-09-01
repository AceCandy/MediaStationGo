package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestImageProxyCachesFailedRemoteImageFetch(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("not found")),
			Request:    req,
		}, nil
	})}
	raw := "https://image.tmdb.org/t/p/w500/poster.jpg"
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.Len() != len(transparent1x1PNG) {
			t.Fatalf("body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
		}
		if got := rec.Header().Get("Cache-Control"); got != imagePlaceholderCacheControl {
			t.Fatalf("Cache-Control = %q, want %q", got, imagePlaceholderCacheControl)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 due to negative cache", got)
	}
}

func TestImageProxyDoesNotCacheTransientRemoteImageFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		err    error
	}{
		{name: "network", err: errors.New("network unavailable")},
		{name: "server error", status: http.StatusBadGateway},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
			proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				if tt.err != nil {
					return nil, tt.err
				}
				return &http.Response{StatusCode: tt.status, Status: http.StatusText(tt.status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader("failed")), Request: req}, nil
			})}
			raw := "https://image.tmdb.org/t/p/original/transient.jpg"
			for range 2 {
				if _, _, err := proxy.Fetch(t.Context(), raw); err == nil {
					t.Fatal("expected transient image fetch failure")
				}
			}
			if got := atomic.LoadInt32(&calls); got != 2 {
				t.Fatalf("upstream calls = %d, want 2 without negative cache", got)
			}
			_, _, failPath, err := proxy.remoteImageCachePaths(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(failPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("transient failure marker exists: %v", err)
			}
		})
	}
}

func TestRemoteImageHTTPStatusOnlyMatchesExact404(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadGateway} {
		proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
		proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader("failed")), Request: req}, nil
		})}
		_, _, err := proxy.Fetch(t.Context(), "https://image.tmdb.org/t/p/original/status.jpg")
		if got := isRemoteImageHTTPStatus(err, http.StatusNotFound); got != (status == http.StatusNotFound) {
			t.Fatalf("status %d matched 404 = %v, err=%v", status, got, err)
		}
		if err != nil && strings.Contains(err.Error(), "image.tmdb.org") {
			t.Fatalf("status error leaked URL: %v", err)
		}
	}
}

func TestImageProxyRemoveFailedAllowsRetry(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://image.tmdb.org/t/p/w500/retry-poster.jpg"
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("first body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
	}
	_, _, failPath, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directImageFailPath(failPath), []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := proxy.RemoveFailed(raw); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{failPath, directImageFailPath(failPath)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failure marker %q was not removed: %v", path, err)
		}
	}
	rec = httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?v=retry", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("retried body = %x, want poster bytes", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 after retry", got)
	}
}

func TestImageProxyPrefetchRemoteUsesProviderHeaders(t *testing.T) {
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Referer"); got != "https://movie.douban.com/" {
			t.Fatalf("Referer = %q, want Douban movie referer", got)
		}
		if got := req.Header.Get("User-Agent"); !strings.Contains(got, "Mozilla/5.0") {
			t.Fatalf("User-Agent = %q, want browser-like UA", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2933012346.jpg"
	if err := proxy.PrefetchRemote(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestImageProxyUsesDirectModeOnlyForConfiguredDoubanImages(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.setAPIConfigService(apiConfig)
	origin := "http://db-pic1.acecandy.cn"
	direct := true
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{BaseURL: &origin, ImageDirect: &direct}); err != nil {
		t.Fatal(err)
	}

	for _, host := range []string{"img9.doubanio.com", "db-pic1.acecandy.cn"} {
		if !proxy.useDoubanImageDirect(t.Context(), host) {
			t.Fatalf("host %q did not use Douban direct mode", host)
		}
	}
	for _, host := range []string{"image.tmdb.org", "doubanio.com.evil.example", "notdoubanio.com"} {
		if proxy.useDoubanImageDirect(t.Context(), host) {
			t.Fatalf("non-Douban host %q used direct mode", host)
		}
	}
	direct = false
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{ImageDirect: &direct}); err != nil {
		t.Fatal(err)
	}
	if proxy.useDoubanImageDirect(t.Context(), "db-pic1.acecandy.cn") {
		t.Fatal("disabled Douban image direct option was ignored")
	}

	clients := proxy.remoteImageFetchClients(false)
	if len(clients) != 2 || clients[0].name != "default" || clients[1].name != "direct" {
		t.Fatalf("default clients = %#v", clients)
	}
	clients = proxy.remoteImageFetchClients(true)
	if len(clients) != 1 || clients[0].name != "direct" {
		t.Fatalf("direct clients = %#v", clients)
	}
	transport, ok := clients[0].client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("direct client did not bypass proxy")
	}
	if !proxy.canUseExternalImageFallback(true, "db-pic1.acecandy.cn") {
		t.Fatal("custom Douban image domain cannot use curl fallback")
	}
	if proxy.canUseExternalImageFallback(false, "db-pic1.acecandy.cn") {
		t.Fatal("custom domain used curl fallback while direct mode was disabled")
	}
	if !proxy.canUseExternalImageFallback(false, "img9.doubanio.com") {
		t.Fatal("existing official Douban curl fallback changed")
	}
	if directImageFailPath("poster.fail") == "poster.fail" {
		t.Fatal("direct mode reused the default failure marker")
	}
}

func TestImageProxyUsesLiveDoubanCDNAndFallsBackToOfficial(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	origin := "https://images-one.test"
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{BaseURL: &origin}); err != nil {
		t.Fatal(err)
	}

	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.setAPIConfigService(apiConfig)
	requests := []string{}
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		if req.URL.Host == "images-one.test" && strings.Contains(req.URL.Path, "p2.webp") {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
		}
		if strings.Contains(req.URL.Path, "p4.") {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(testJPEG)), Request: req}, nil
	})}

	officialOne := "https://img9.doubanio.com/view/photo/l/public/p1.jpg"
	if _, _, err := proxy.Fetch(t.Context(), officialOne); err != nil {
		t.Fatal(err)
	}
	if _, _, err := proxy.Fetch(t.Context(), officialOne); err != nil {
		t.Fatal(err)
	}
	officialFallback := "https://img9.doubanio.com/view/photo/l/public/p2.jpg"
	if _, _, err := proxy.Fetch(t.Context(), officialFallback); err != nil {
		t.Fatal(err)
	}
	origin = "https://images-two.test"
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{BaseURL: &origin}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := proxy.Fetch(t.Context(), "https://img9.doubanio.com/view/photo/l/public/p3.jpg"); err != nil {
		t.Fatal(err)
	}
	officialMissing := "https://img9.doubanio.com/view/photo/l/public/p4.jpg"
	for range 2 {
		if _, _, err := proxy.Fetch(t.Context(), officialMissing); err == nil {
			t.Fatal("expected missing official Douban artwork")
		}
	}

	want := []string{
		"https://images-one.test/view/photo/l/public/p1.webp",
		"https://images-one.test/view/photo/l/public/p2.webp",
		officialFallback,
		"https://images-two.test/view/photo/l/public/p3.webp",
		"https://images-two.test/view/photo/l/public/p4.webp",
		officialMissing,
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
	_, officialCache, _, err := proxy.remoteImageCachePaths(officialOne)
	if err != nil {
		t.Fatal(err)
	}
	_, cdnCache, _, err := proxy.remoteImageCachePaths(want[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(officialCache); err != nil {
		t.Fatalf("official URL cache is missing: %v", err)
	}
	if _, err := os.Stat(cdnCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CDN URL unexpectedly owns cache: %v", err)
	}
}

func TestImageProxyDirectFailureUsesCurlForCustomHost(t *testing.T) {
	var directCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&directCalls, 1)
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(upstream.Close)

	originalCurl := fetchRemoteImageWithCurl
	t.Cleanup(func() { fetchRemoteImageWithCurl = originalCurl })
	var curlCalls int32
	fetchRemoteImageWithCurl = func(_ context.Context, raw, host string) ([]byte, string, string, error) {
		atomic.AddInt32(&curlCalls, 1)
		if raw != upstream.URL+"/poster.webp" || host != strings.TrimPrefix(upstream.URL, "http://") {
			t.Fatalf("curl fallback received raw=%q host=%q", raw, host)
		}
		return testJPEG, "image/jpeg", "", nil
	}

	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	raw := upstream.URL + "/poster.webp"
	host := strings.TrimPrefix(upstream.URL, "http://")
	data, ctype, _, err := proxy.fetchRemoteImageUncached(t.Context(), raw, host, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, testJPEG) || ctype != "image/jpeg" {
		t.Fatalf("curl fallback result = %x, %q", data, ctype)
	}
	if atomic.LoadInt32(&directCalls) != 1 || atomic.LoadInt32(&curlCalls) != 1 {
		t.Fatalf("calls = direct %d, curl %d", directCalls, curlCalls)
	}
}

func TestImageProxyRemoveCachedAllowsRefresh(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://image.tmdb.org/t/p/w500/refresh-poster.jpg"
	if err := proxy.PrefetchRemote(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	_, _, failPath, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directImageFailPath(failPath), []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := proxy.RemoveCached(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directImageFailPath(failPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("direct failure marker was not removed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls before refresh = %d, want 1", got)
	}
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?refresh=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls after refresh = %d, want 2", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestImageProxyRefreshKeepsCachedImageOnUpstreamFailure(t *testing.T) {
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
			Request:    req,
		}, nil
	})}

	raw := "https://image.tmdb.org/t/p/w500/cached-poster.jpg"
	_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	cachedPoster := testJPEG
	if err := os.WriteFile(cachePath, cachedPoster, 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?refresh=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, cachedPoster) {
		t.Fatalf("body = %x, want cached poster after failed refresh", got)
	}
}

func TestImageProxyRefetchesTransparentPlaceholderCache(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p2925358079.jpg"
	_, cachePath, failPath, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, transparent1x1PNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?retry=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("body = %x, want refetched poster bytes", got)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestImageProxyDoesNotCacheNonImageRemoteResponse(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<html>not image</html>")),
			Request:    req,
		}, nil
	})}

	raw := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p-bad.jpg"
	for range 2 {
		rec := httptest.NewRecorder()
		if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
			t.Fatal(err)
		}
		if rec.Body.Len() != len(transparent1x1PNG) {
			t.Fatalf("body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
		}
	}
	_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-image response should not be cached, stat err=%v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 without negative cache", got)
	}
}

func TestImageProxyDoesNotCacheMislabeledRemoteResponse(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(strings.NewReader("<html>not a poster</html>")),
			Request:    req,
		}, nil
	})}

	raw := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p-mislabeled.jpg"
	for range 2 {
		rec := httptest.NewRecorder()
		if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
			t.Fatal(err)
		}
		if rec.Body.Len() != len(transparent1x1PNG) {
			t.Fatalf("body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
		}
	}
	_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mislabeled non-image response should not be cached, stat err=%v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 without negative cache", got)
	}
}
