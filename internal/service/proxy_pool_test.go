package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestNormalizeProxyPoolURL(t *testing.T) {
	for _, raw := range []string{
		"proxy.example:8080",
		"http://proxy.example:8080",
		"https://proxy.example:8443",
		"socks5://proxy.example:1080",
		"socks5h://proxy.example:1080",
	} {
		if _, err := normalizeProxyPoolURL(raw); err != nil {
			t.Fatalf("normalize %q: %v", raw, err)
		}
	}

	for _, raw := range []string{
		"ftp://proxy.example:21",
		"http:///missing-host",
		"http://proxy.example/path",
		"http://proxy.example?token=secret",
		"http://proxy.example#fragment",
	} {
		if _, err := normalizeProxyPoolURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}

	const sensitive = "ftp://alice:top-secret@proxy.example:21"
	_, err := normalizeProxyPoolURL(sensitive)
	if err == nil || strings.Contains(err.Error(), "alice") || strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("unsafe validation error: %v", err)
	}
}

func TestProxyPoolServiceReplaceEncryptsAndProjectsCredentials(t *testing.T) {
	db := newServiceTestDB(t, &model.ProxyPoolEntry{})
	crypto := NewCryptoService("proxy-pool-test-secret", zap.NewNop())
	svc := NewProxyPoolService(&repository.Container{DB: db}, crypto)
	authURL := "http://alice:top-secret@proxy-a.example:8080"
	plainURL := "socks5://proxy-b.example:1080"

	items, err := svc.Replace(t.Context(), []ProxyPoolInput{{URL: &authURL}, {URL: &plainURL}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].DisplayURL != "http://proxy-a.example:8080" || !items[0].HasAuth {
		t.Fatalf("unsafe or incomplete projection: %#v", items)
	}
	response, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"alice", "top-secret", "enc:v1:"} {
		if strings.Contains(string(response), forbidden) {
			t.Fatalf("response exposed %q: %s", forbidden, response)
		}
	}

	var rows []model.ProxyPoolEntry
	if err := db.Order("position asc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !crypto.IsEncrypted(rows[0].URL) || !crypto.IsEncrypted(rows[1].URL) {
		t.Fatalf("proxy URLs were not encrypted: %#v", rows)
	}
	for _, row := range rows {
		if strings.Contains(row.URL, "alice") || strings.Contains(row.URL, "top-secret") || strings.Contains(row.URL, "proxy-") {
			t.Fatal("proxy plaintext reached the database")
		}
	}

	firstID, secondID := items[0].ID, items[1].ID
	generation := svc.generation
	items, err = svc.Replace(t.Context(), []ProxyPoolInput{{ID: secondID}, {ID: firstID}})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ID != secondID || items[1].ID != firstID || svc.generation != generation+1 {
		t.Fatalf("reorder result = %#v, generation=%d", items, svc.generation)
	}

	replacement := "socks5h://bob:new-secret@proxy-c.example:1080"
	items, err = svc.Replace(t.Context(), []ProxyPoolInput{{ID: firstID, URL: &replacement}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != firstID || items[0].DisplayURL != "socks5h://proxy-c.example:1080" || !items[0].HasAuth {
		t.Fatalf("replacement result = %#v", items)
	}
	var deleted model.ProxyPoolEntry
	if err := db.First(&deleted, "id = ?", secondID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("removed proxy was not physically deleted: %v", err)
	}

	if _, err := svc.Replace(t.Context(), []ProxyPoolInput{{ID: "missing"}}); err == nil {
		t.Fatal("expected unknown proxy id to fail")
	}
	if _, err := svc.Replace(t.Context(), []ProxyPoolInput{{ID: firstID}, {ID: firstID}}); err == nil {
		t.Fatal("expected duplicate proxy id to fail")
	}
	badReplacement := "ftp://bob:do-not-leak@proxy.example:21"
	if _, err := svc.Replace(t.Context(), []ProxyPoolInput{{ID: firstID, URL: &badReplacement}}); err == nil || strings.Contains(err.Error(), "bob") || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("unsafe replacement error: %v", err)
	}
	items, err = svc.List(t.Context())
	if err != nil || len(items) != 1 || items[0].DisplayURL != "socks5h://proxy-c.example:1080" {
		t.Fatalf("failed update changed the proxy pool: %#v, %v", items, err)
	}
}

func TestAPIConfigUseProxyPoolRoundTripAndRevision(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	if err := db.Create(&model.APIConfig{Provider: "douban", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	resolved, err := svc.Resolve(t.Context(), "douban")
	if err != nil || resolved.UseProxyPool || resolved.Revision != 0 {
		t.Fatalf("default resolved config = %#v, %v", resolved, err)
	}

	enabled := true
	view, err := svc.Update(t.Context(), "douban", APIConfigPatch{UseProxyPool: &enabled})
	if err != nil || !view.UseProxyPool {
		t.Fatalf("public config = %#v, %v", view, err)
	}
	resolved, err = svc.Resolve(t.Context(), "douban")
	if err != nil || !resolved.UseProxyPool || resolved.Revision != 1 {
		t.Fatalf("resolved config = %#v, %v", resolved, err)
	}
	if _, err := svc.Update(t.Context(), "douban", APIConfigPatch{UseProxyPool: &enabled}); err != nil {
		t.Fatal(err)
	}
	resolved, err = svc.Resolve(t.Context(), "douban")
	if err != nil || resolved.Revision != 2 {
		t.Fatalf("same-value save did not advance revision: %#v, %v", resolved, err)
	}
}

func TestDoubanProxyPoolRouting(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	enabled := true
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{UseProxyPool: &enabled}); err != nil {
		t.Fatal(err)
	}
	resolved, err := apiConfig.Resolve(t.Context(), "douban")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ordered selection and sticky reset", func(t *testing.T) {
		calls := []string{}
		direct := scriptedDoubanClient(t, "direct", &calls,
			doubanTestOutcome{status: 400}, doubanTestOutcome{status: 200}, doubanTestOutcome{status: 200})
		proxy1 := scriptedDoubanClient(t, "proxy1", &calls, doubanTestOutcome{status: 400})
		proxy2 := scriptedDoubanClient(t, "proxy2", &calls,
			doubanTestOutcome{status: 200}, doubanTestOutcome{status: 200}, doubanTestOutcome{status: 400})
		provider := doubanProxyTestProvider(apiConfig, direct, proxy1, proxy2)

		for range 2 {
			if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
				t.Fatalf("request status=%d err=%v", status, err)
			}
		}
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("proxy reset status=%d err=%v", status, err)
		}
		if got, want := strings.Join(calls, ","), "direct,proxy1,proxy2,proxy2,proxy2,direct"; got != want {
			t.Fatalf("calls = %s, want %s", got, want)
		}
		if route := provider.currentRoute(1, resolved.Revision); route != doubanDirectRoute {
			t.Fatalf("route = %d, want direct", route)
		}
	})

	t.Run("all proxies then final direct", func(t *testing.T) {
		calls := []string{}
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls, doubanTestOutcome{status: 400}, doubanTestOutcome{status: 200}),
			scriptedDoubanClient(t, "proxy1", &calls, doubanTestOutcome{status: 400}),
			scriptedDoubanClient(t, "proxy2", &calls, doubanTestOutcome{status: 400}),
		)
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("status=%d err=%v", status, err)
		}
		if got, want := strings.Join(calls, ","), "direct,proxy1,proxy2,direct"; got != want {
			t.Fatalf("calls = %s, want %s", got, want)
		}
	})

	t.Run("empty pool still retries direct", func(t *testing.T) {
		calls := []string{}
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls, doubanTestOutcome{status: 400}, doubanTestOutcome{status: 400}),
		)
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 400 {
			t.Fatalf("status=%d err=%v", status, err)
		}
		if got := strings.Join(calls, ","); got != "direct,direct" {
			t.Fatalf("calls = %s", got)
		}
	})

	t.Run("network error does not change sticky proxy", func(t *testing.T) {
		calls := []string{}
		proxyErr := errors.New("proxy network failure")
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls),
			scriptedDoubanClient(t, "proxy1", &calls, doubanTestOutcome{err: proxyErr}),
		)
		provider.setRoute(1, resolved.Revision, 0)
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); !errors.Is(err, proxyErr) || status != 0 {
			t.Fatalf("status=%d err=%v", status, err)
		}
		if route := provider.currentRoute(1, resolved.Revision); route != 0 {
			t.Fatalf("route changed to %d", route)
		}
		if got := strings.Join(calls, ","); got != "proxy1" {
			t.Fatalf("calls = %s", got)
		}
	})

	t.Run("non-400 statuses do not switch routes", func(t *testing.T) {
		calls := []string{}
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls,
				doubanTestOutcome{status: 403},
				doubanTestOutcome{status: 404},
				doubanTestOutcome{status: 429},
				doubanTestOutcome{status: 500},
			),
			scriptedDoubanClient(t, "proxy1", &calls),
		)
		for _, want := range []int{403, 404, 429, 500} {
			if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != want {
				t.Fatalf("status=%d err=%v, want %d", status, err, want)
			}
		}
		if got := strings.Join(calls, ","); got != "direct,direct,direct,direct" {
			t.Fatalf("calls = %s", got)
		}
	})

	t.Run("pool generation resets to direct", func(t *testing.T) {
		calls := []string{}
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls, doubanTestOutcome{status: 400}, doubanTestOutcome{status: 200}),
			scriptedDoubanClient(t, "proxy1", &calls, doubanTestOutcome{status: 200}),
		)
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("select proxy status=%d err=%v", status, err)
		}
		provider.proxyPool.mu.Lock()
		provider.proxyPool.generation++
		provider.proxyPool.mu.Unlock()
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("reset request status=%d err=%v", status, err)
		}
		if got, want := strings.Join(calls, ","), "direct,proxy1,direct"; got != want {
			t.Fatalf("calls = %s, want %s", got, want)
		}
	})

	t.Run("proxy response status becomes sticky even when body read fails", func(t *testing.T) {
		calls := []string{}
		readErr := errors.New("read failed")
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls, doubanTestOutcome{status: 400}),
			scriptedDoubanClient(t, "proxy1", &calls,
				doubanTestOutcome{status: 200, readErr: readErr},
				doubanTestOutcome{status: 200},
			),
		)
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); !errors.Is(err, readErr) || status != 200 {
			t.Fatalf("status=%d err=%v", status, err)
		}
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("sticky request status=%d err=%v", status, err)
		}
		if got, want := strings.Join(calls, ","), "direct,proxy1,proxy1"; got != want {
			t.Fatalf("calls = %s, want %s", got, want)
		}
	})

	t.Run("configuration revisions reset to direct", func(t *testing.T) {
		calls := []string{}
		provider := doubanProxyTestProvider(apiConfig,
			scriptedDoubanClient(t, "direct", &calls, doubanTestOutcome{status: 400}, doubanTestOutcome{status: 200}),
			scriptedDoubanClient(t, "proxy1", &calls, doubanTestOutcome{status: 200}),
		)
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("select proxy status=%d err=%v", status, err)
		}
		disabled := false
		if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{UseProxyPool: &disabled}); err != nil {
			t.Fatal(err)
		}
		if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{UseProxyPool: &enabled}); err != nil {
			t.Fatal(err)
		}
		if _, status, err := provider.requestJSON(t.Context(), "https://example.test/data", ""); err != nil || status != 200 {
			t.Fatalf("reset request status=%d err=%v", status, err)
		}
		if got, want := strings.Join(calls, ","), "direct,proxy1,direct"; got != want {
			t.Fatalf("calls = %s, want %s", got, want)
		}
	})

	t.Run("concurrent 400 responses keep proxy order", func(t *testing.T) {
		var directCalls atomic.Int32
		var proxy1Calls atomic.Int32
		var proxy2Calls atomic.Int32
		bothDirect := make(chan struct{})
		direct := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if directCalls.Add(1) == 2 {
				close(bothDirect)
			}
			<-bothDirect
			return doubanTestResponse(req, 400), nil
		})}
		proxy1 := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			proxy1Calls.Add(1)
			return doubanTestResponse(req, 200), nil
		})}
		proxy2 := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			proxy2Calls.Add(1)
			return doubanTestResponse(req, 200), nil
		})}
		provider := doubanProxyTestProvider(apiConfig, direct, proxy1, proxy2)

		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, status, err := provider.requestJSON(t.Context(), "https://example.test/data", "")
				if err == nil && status != 200 {
					err = errors.New("unexpected response status")
				}
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if directCalls.Load() != 2 || proxy1Calls.Load() != 2 || proxy2Calls.Load() != 0 {
			t.Fatalf("calls: direct=%d proxy1=%d proxy2=%d", directCalls.Load(), proxy1Calls.Load(), proxy2Calls.Load())
		}
	})
}

type doubanTestOutcome struct {
	status  int
	err     error
	readErr error
}

func scriptedDoubanClient(t *testing.T, name string, calls *[]string, outcomes ...doubanTestOutcome) *http.Client {
	t.Helper()
	next := 0
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		*calls = append(*calls, name)
		if next >= len(outcomes) {
			t.Fatalf("unexpected %s request", name)
		}
		outcome := outcomes[next]
		next++
		if outcome.err != nil {
			return nil, outcome.err
		}
		body := io.ReadCloser(io.NopCloser(strings.NewReader(`[]`)))
		if outcome.readErr != nil {
			body = &doubanErrorReadCloser{err: outcome.readErr}
		}
		response := doubanTestResponse(req, outcome.status)
		response.Body = body
		return response, nil
	})}
}

func doubanTestResponse(req *http.Request, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`[]`)), Request: req}
}

func doubanProxyTestProvider(apiConfig *APIConfigService, direct *http.Client, proxies ...*http.Client) *DoubanProvider {
	provider := NewDoubanProvider(apiConfig)
	provider.directClient = direct
	provider.proxyPool = &ProxyPoolService{loaded: true, generation: 1, clients: proxies}
	return provider
}

type doubanErrorReadCloser struct{ err error }

func (r *doubanErrorReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (r *doubanErrorReadCloser) Close() error             { return nil }

func TestDoubanSearchAndDiscoverPreferHTTPStatusErrors(t *testing.T) {
	provider := NewDoubanProvider(nil)
	provider.client = scriptedDoubanClient(t, "legacy", &[]string{},
		doubanTestOutcome{status: 400, readErr: errors.New("read failed")},
		doubanTestOutcome{status: 400, readErr: errors.New("read failed")},
	)
	if _, err := provider.Search(t.Context(), "test"); err == nil || err.Error() != "douban search: 400" {
		t.Fatalf("search error = %v", err)
	}
	if _, err := provider.Discover(t.Context(), "douban_hot_movie"); err == nil || err.Error() != "douban discover: 400" {
		t.Fatalf("discover error = %v", err)
	}
}
