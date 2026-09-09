package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestDoubanProviderResolvesCookiePerRequest(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	provider := NewDoubanProvider(apiConfig)
	wantCookie := ""
	seen := map[string]int{}
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Cookie") != wantCookie {
			t.Fatal("douban request used an unexpected cookie state")
		}
		seen[req.URL.Path]++
		status := http.StatusOK
		body := `[]`
		switch req.URL.Path {
		case "/rexxar/api/v2/movie/1":
			status = http.StatusServiceUnavailable
			body = `unavailable`
		case "/j/subject_abstract":
			body = `{"subject":{"title":"测试"}}`
		case "/j/search_subjects":
			body = `{"subjects":[]}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	if _, err := provider.Search(t.Context(), "测试"); err != nil {
		t.Fatal(err)
	}
	cookie := strings.Repeat("session=test-initial;", 40)
	enabled := true
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{APIKey: &cookie, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	wantCookie = cookie
	if _, err := provider.Search(t.Context(), "测试"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetMatchByID(t.Context(), "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Discover(t.Context(), "douban_hot_movie"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/j/subject_suggest", "/rexxar/api/v2/movie/1", "/j/subject_abstract", "/j/search_subjects"} {
		if seen[path] == 0 {
			t.Fatal("expected douban request path was not exercised")
		}
	}

	cookie = "session=test-updated"
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{APIKey: &cookie}); err != nil {
		t.Fatal(err)
	}
	wantCookie = cookie
	if _, err := provider.Search(t.Context(), "测试"); err != nil {
		t.Fatal(err)
	}
	if err := apiConfig.Delete(t.Context(), "douban"); err != nil {
		t.Fatal(err)
	}
	wantCookie = ""
	if _, err := provider.Search(t.Context(), "测试"); err != nil {
		t.Fatal(err)
	}

	cookie = "session=test-disabled"
	enabled = false
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{APIKey: &cookie, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Search(t.Context(), "测试"); err != nil {
		t.Fatal(err)
	}

	enabled = true
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	wantCookie = ""
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Search(t.Context(), "测试"); err != nil {
		t.Fatal(err)
	}
}

func TestDoubanArtworkURLKeepsOfficialOriginAndProjectsCDNForDownload(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	provider := NewDoubanProvider(apiConfig)
	sourceURL := "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p123.jpg?imageView2/2/q/80/w/600/h/3000/format/jpg"
	largeURL := "https://img9.doubanio.com/view/photo/l/public/p123.jpg"

	if got := provider.ResolveArtworkURL(t.Context(), sourceURL); got != largeURL {
		t.Fatalf("official artwork URL = %q", got)
	}
	unknownQueryURL := "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p123.jpg?token=keep"
	if got := provider.ResolveArtworkURL(t.Context(), unknownQueryURL); got != "https://img9.doubanio.com/view/photo/l/public/p123.jpg?token=keep" {
		t.Fatalf("unknown artwork query changed = %q", got)
	}
	origin := "http://db-pic1.acecandy.cn/"
	imageDirect := true
	view, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{BaseURL: &origin, ImageDirect: &imageDirect})
	if err != nil {
		t.Fatal(err)
	}
	if !view.ImageDirect {
		t.Fatal("public config did not preserve image_direct")
	}
	resolved, err := apiConfig.Resolve(t.Context(), "douban")
	if err != nil || !resolved.ImageDirect {
		t.Fatalf("resolved image_direct = %v, err = %v", resolved.ImageDirect, err)
	}
	if got := provider.ResolveArtworkURL(t.Context(), sourceURL); got != largeURL {
		t.Fatalf("configured provider persisted artwork URL = %q", got)
	}
	wantCDN := "http://db-pic1.acecandy.cn/view/photo/l/public/p123.webp"
	if got := projectDoubanArtworkURL(largeURL, resolved.BaseURL); got != wantCDN {
		t.Fatalf("projected CDN artwork URL = %q", got)
	}
	if got := provider.ResolveArtworkURL(t.Context(), "https://img9.doubanio.com/custom/poster?token=keep"); got != "https://img9.doubanio.com/custom/poster?token=keep" {
		t.Fatalf("extensionless artwork URL = %q", got)
	}
	if got := projectDoubanArtworkURL("https://img9.doubanio.com/custom/poster?token=keep", resolved.BaseURL); got != "http://db-pic1.acecandy.cn/custom/poster?token=keep" {
		t.Fatalf("extensionless CDN artwork URL = %q", got)
	}
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"title":"详情","cover":{"image":{"large":{"url":"https://img9.doubanio.com/view/photo/l/public/p456.jpg"}}}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	match, err := provider.GetMatchByID(t.Context(), "456")
	if err != nil || match == nil || match.PosterURL != "https://img9.doubanio.com/view/photo/l/public/p456.jpg" {
		t.Fatalf("detail match = %#v, %v", match, err)
	}

	invalid := "ftp://db-pic1.acecandy.cn/"
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{BaseURL: &invalid}); err == nil {
		t.Fatal("expected invalid Douban image domain rejection")
	}
	if got := provider.ResolveArtworkURL(t.Context(), sourceURL); got != largeURL {
		t.Fatalf("invalid update changed artwork URL = %q", got)
	}
}

func TestDoubanSearchAndDiscoverDeriveLargePosters(t *testing.T) {
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `[{"id":"1","title":"搜索","img":"https://img1.doubanio.com/view/photo/s_ratio_poster/public/p1.jpg?imageView2/2/w/600/format/jpg"}]`
		if req.URL.Path == "/j/search_subjects" {
			body = `{"subjects":[{"id":"2","title":"发现","cover":"https://img2.doubanio.com/view/photo/s_ratio_poster/public/p2.jpg?imageView2/2/w/600/format/jpg"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	match, err := provider.Search(t.Context(), "测试")
	if err != nil || match == nil || match.Img != "https://img1.doubanio.com/view/photo/l/public/p1.jpg" {
		t.Fatalf("search match = %#v, %v", match, err)
	}
	items, err := provider.Discover(t.Context(), "douban_hot_movie")
	if err != nil || len(items) != 1 || items[0].PosterURL != "https://img2.doubanio.com/view/photo/l/public/p2.jpg" {
		t.Fatalf("discover items = %#v, %v", items, err)
	}
}

func TestDeriveDoubanLargePosterURLRejectsMissingFile(t *testing.T) {
	if got := deriveDoubanLargePosterURL("https://img1.doubanio.com/view/photo/s_ratio_poster/public/"); got != "" {
		t.Fatalf("missing-file poster URL = %q", got)
	}
}

func TestNormalizeDoubanImageOrigin(t *testing.T) {
	for _, tt := range []struct {
		raw, want string
		valid     bool
	}{
		{"", "", true},
		{"http://db-pic1.acecandy.cn/", "http://db-pic1.acecandy.cn", true},
		{"https://db-pic1.acecandy.cn", "https://db-pic1.acecandy.cn", true},
		{"ftp://db-pic1.acecandy.cn/", "", false},
		{"https://user@example.com/", "", false},
		{"https://example.com/path", "", false},
	} {
		got, err := normalizeDoubanImageOrigin(tt.raw)
		if (err == nil) != tt.valid || got != tt.want {
			t.Fatalf("normalize %q = %q, %v", tt.raw, got, err)
		}
	}
}

func TestDoubanPosterURLPrefersLargestSnapshotField(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"cover large", `{"title":"test","cover":{"image":{"large":{"url":"https://img.test/view/photo/l/public/1.jpg"}}},"pic":{"large":"https://img.test/m.jpg","normal":"https://img.test/s.jpg"}}`, "https://img.test/view/photo/l/public/1.jpg"},
		{"invalid cover large", `{"title":"test","cover":{"image":{"large":{"url":"not-a-url"}}},"pic":{"large":"https://img.test/m.jpg","normal":"https://img.test/s.jpg"}}`, "https://img.test/m.jpg"},
		{"pic large with root title fallback", `{"title":"test","subject":{"pic":{"large":"https://img.test/m.jpg","normal":"https://img.test/s.jpg"}}}`, "https://img.test/m.jpg"},
		{"nested small in large field", `{"title":"test","pic":{"large":{"small":"https://img.test/s.jpg"}}}`, ""},
		{"pic normal", `{"title":"test","pic":{"normal":"https://img.test/s.jpg"}}`, ""},
		{"legacy cover", `{"title":"test","cover_url":"https://img.test/legacy.jpg"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := doubanMatchFromRawJSON("1", []byte(tt.raw))
			if err != nil || match.PosterURL != tt.want {
				t.Fatalf("poster URL = %q, err = %v", match.PosterURL, err)
			}
		})
	}
}

func TestDoubanGetMatchByIDPreservesRawJSON(t *testing.T) {
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/rexxar/api/v2/movie/1295644" || req.Header.Get("Referer") != "https://m.douban.com/subject/1295644/" {
			t.Fatalf("request = %s", req.URL.String())
		}
		body := `{"title":"豆瓣详情","original_title":"Original","intro":"完整简介","cover":{"image":{"large":{"url":"https://img.test/poster.jpg"}}},"tmdb_id":603,"rating":{"value":9.4},"future_field":{"kept":true}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	match, err := provider.GetMatchByID(t.Context(), "1295644")
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Source != "douban" || match.DoubanID != "1295644" || match.TMDbID != 603 || match.Overview != "完整简介" || match.PosterURL != "https://img.test/poster.jpg" || match.Rating != 9.4 {
		t.Fatalf("match = %#v", match)
	}
	if !strings.Contains(string(match.RawJSON), `"future_field":{"kept":true}`) {
		t.Fatalf("raw json = %s", match.RawJSON)
	}
}

func TestDoubanGetMatchByIDFallsBackToSubjectAbstract(t *testing.T) {
	provider := NewDoubanProvider(nil)
	requests := 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
		}
		if req.URL.Path != "/j/subject_abstract" || req.URL.Query().Get("subject_id") != "1295644" {
			t.Fatalf("fallback request = %s", req.URL.String())
		}
		body := `{"subject":{"title":"摘要详情","rating":8.8}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	match, err := provider.GetMatchByID(t.Context(), "1295644")
	if err != nil || match == nil || match.Title != "摘要详情" || requests != 2 {
		t.Fatalf("match = %#v, requests = %d, err = %v", match, requests, err)
	}
}

func TestDoubanGetEnrichmentMatchByIDDoesNotFallbackForRetryableErrors(t *testing.T) {
	tests := []struct {
		name, body, wantError string
		status                int
		err                   error
	}{
		{name: "network", err: errors.New("network unavailable"), wantError: "network unavailable"},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `rate limited`, wantError: "HTTP 429"},
		{name: "server error", status: http.StatusServiceUnavailable, body: `unavailable`, wantError: "HTTP 503"},
		{name: "empty response", status: http.StatusOK, wantError: "invalid json"},
		{name: "invalid json", status: http.StatusOK, body: `{`, wantError: "invalid json"},
		{name: "error object", status: http.StatusOK, body: `{"code":"subject_ip_rate_limit"}`, wantError: `response code "subject_ip_rate_limit"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewDoubanProvider(nil)
			requests := 0
			provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Path != "/rexxar/api/v2/movie/1295644" {
					t.Fatalf("unexpected fallback request = %s", req.URL.String())
				}
				if tt.err != nil {
					return nil, tt.err
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Request: req}, nil
			})}

			_, _, err := provider.GetEnrichmentMatchByID(t.Context(), "1295644", model.MetadataKindMovie)
			if !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want reason %q", err, tt.wantError)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("error = %v, want wrapped error %v", err, tt.err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestDoubanGetEnrichmentMatchByIDFallsBackForPermissionErrors(t *testing.T) {
	for _, tt := range []struct {
		name, kind, detailPath string
		status                 int
		body                   string
	}{
		{name: "movie forbidden", kind: model.MetadataKindMovie, detailPath: "/rexxar/api/v2/movie/1295644", status: http.StatusForbidden, body: `forbidden`},
		{name: "series code 1000", kind: model.MetadataKindSeries, detailPath: "/rexxar/api/v2/tv/1295644", status: http.StatusOK, body: `{"code":1000}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewDoubanProvider(nil)
			requests := []string{}
			provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.Path)
				if len(requests) == 1 {
					if req.URL.Path != tt.detailPath {
						t.Fatalf("detail request = %s", req.URL.String())
					}
					return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Request: req}, nil
				}
				if req.URL.Path != "/rexxar/api/v2/subject/1295644" {
					t.Fatalf("subject request = %s", req.URL.String())
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"降级标题"}`)), Request: req}, nil
			})}

			match, degraded, err := provider.GetEnrichmentMatchByID(t.Context(), "1295644", tt.kind)
			if err != nil || match == nil || match.Title != "降级标题" || !degraded {
				t.Fatalf("match = %#v, degraded = %v, err = %v", match, degraded, err)
			}
			if len(requests) != 2 {
				t.Fatalf("requests = %v", requests)
			}
		})
	}
}

func TestDoubanGetEnrichmentMatchByIDTreatsNotFoundAsPermanent(t *testing.T) {
	provider := NewDoubanProvider(nil)
	requests := 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`not found`)), Request: req}, nil
	})}

	if _, _, err := provider.GetEnrichmentMatchByID(t.Context(), "1295644", model.MetadataKindMovie); !errors.Is(err, ErrDoubanSubjectNotFound) {
		t.Fatalf("error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestDoubanGetEpisodeCountByIDUsesMobileDetail(t *testing.T) {
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/rexxar/api/v2/movie/35588177" {
			t.Fatalf("request = %s", req.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"type":"tv","episodes_count":12}`)), Request: req}, nil
	})}

	count, err := provider.GetEpisodeCountByID(t.Context(), "35588177")
	if err != nil || count != 12 {
		t.Fatalf("episode count = %d, err = %v", count, err)
	}
}

func TestDoubanProviderMatchPersistsSnapshotWithoutFusingDetailFields(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	requests := 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		body := `{"title":"详情标题","tmdb_id":603,"rating":{"value":9.4},"future_field":{"kept":true}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider

	media := model.Media{Title: "扫描标题", Path: "/movie.mkv"}
	lib := model.Library{Type: "movie"}
	match := &Match{Source: "douban", MediaType: "movie", DoubanID: "1295644", Title: "已选择标题", Rating: 7.2}
	persisted, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Target.Title != "已选择标题" || persisted.Target.Rating != 7.2 {
		t.Fatalf("canonical fields were fused: %#v", persisted.Target)
	}
	if match.TMDbID != 603 {
		t.Fatalf("tmdb crosswalk = %d", match.TMDbID)
	}
	if requests != 1 {
		t.Fatalf("douban detail requests = %d", requests)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), persisted.Target.ID, "douban")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || !strings.Contains(snapshot.Payload, `"future_field":{"kept":true}`) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := repos.DB.Model(&model.MetadataProviderSnapshot{}).
		Where("metadata_id = ? AND provider = ?", persisted.Target.ID, "douban").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("snapshot count = %d, err = %v", count, err)
	}
}

func TestDoubanDetailFailureKeepsAcceptedMatchWithoutSnapshot(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
	})}
	scraper.douban = provider

	media := model.Media{Title: "扫描标题", Path: "/movie.mkv"}
	lib := model.Library{Type: "movie"}
	match := &Match{Source: "douban", MediaType: "movie", DoubanID: "1295644", Title: "已选择标题", Rating: 7.2}
	persisted, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Target.Title != "已选择标题" {
		t.Fatalf("canonical title = %q", persisted.Target.Title)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), persisted.Target.ID, "douban")
	if err != nil || snapshot != nil {
		t.Fatalf("snapshot = %#v, err = %v", snapshot, err)
	}
}

func TestDoubanMovieEnrichmentOnlyFillsMissingFields(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	requests := 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		body := `{"title":"中文标题","intro":"中文简介","year":"1997","rating":{"value":9.4},"languages":["汉语","英语"],"tmdb_id":603}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider
	metadata := model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "English title", Rating: 8.1, Source: "tmdb",
	}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "603"},
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"},
	}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"subject":{"title":"旧标题"}}`)
	previousFetchedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), metadata.ID, "douban", raw, previousFetchedAt); err != nil {
		t.Fatal(err)
	}
	result, err := scraper.enrichMovieFromDouban(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "中文标题" || updated.OriginalName != "English title" || updated.Overview != "中文简介" || updated.Year != 1997 {
		t.Fatalf("enriched metadata = %#v", updated)
	}
	if updated.Rating != 8.1 || updated.Source != "tmdb" || !result.SnapshotSaved || requests != 1 {
		t.Fatalf("existing fields or snapshot changed: metadata=%#v result=%#v", updated, result)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot == nil || !snapshot.FetchedAt.After(previousFetchedAt) || !strings.Contains(snapshot.Payload, `"中文简介"`) {
		t.Fatalf("refreshed snapshot = %#v, err = %v", snapshot, err)
	}
}

func TestDoubanMovieEnrichmentFailureDoesNotAdvanceSnapshotCooldown(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
	})}
	scraper.douban = provider
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "English title", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"}}); err != nil {
		t.Fatal(err)
	}
	fetchedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), metadata.ID, "douban", []byte(`{"subject":{}}`), fetchedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := scraper.enrichMovieFromDouban(t.Context(), metadata.ID); err == nil {
		t.Fatal("expected douban request failure")
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot == nil || !snapshot.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("snapshot cooldown changed after failure: %#v, err = %v", snapshot, err)
	}
}

func TestEnrichMovieFromDoubanDoesNotTouchBatchCursor(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), doubanMovieEnrichmentCursorKey, "keep-cursor"); err != nil {
		t.Fatal(err)
	}
	provider := NewDoubanProvider(nil)
	requests := 0
	fail := false
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != "/rexxar/api/v2/movie/1295644" {
			t.Fatalf("request = %s", req.URL.String())
		}
		if fail {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`unavailable`)), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"中文标题","intro":"简介"}`)), Request: req}, nil
	})}
	scraper.douban = provider

	withoutID := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "无豆瓣 ID", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &withoutID, nil); err != nil {
		t.Fatal(err)
	}
	if err := scraper.EnrichMovieFromDouban(t.Context(), withoutID.ID); !errors.Is(err, ErrDoubanEnrichmentIneligible) || requests != 0 {
		t.Fatalf("missing ID error = %v, requests = %d", err, requests)
	}

	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "English", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"}}); err != nil {
		t.Fatal(err)
	}
	if err := scraper.EnrichMovieFromDouban(t.Context(), metadata.ID); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := scraper.EnrichMovieFromDouban(t.Context(), metadata.ID); !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
		t.Fatalf("temporary error = %v", err)
	}
	cursor, err := repos.Setting.Get(t.Context(), doubanMovieEnrichmentCursorKey)
	if err != nil || cursor != "keep-cursor" {
		t.Fatalf("cursor = %q, err = %v", cursor, err)
	}
}

func TestEnrichFromDoubanPersistsAndClearsSeriesDegradedSnapshot(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(nil)
	restricted := true
	subjectFails := true
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rexxar/api/v2/tv/35763827":
			if restricted {
				return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`forbidden`)), Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"完整中文标题","intro":"完整简介"}`)), Request: req}, nil
		case "/rexxar/api/v2/subject/35763827":
			if subjectFails {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`unavailable`)), Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"降级中文标题","intro":"降级简介"}`)), Request: req}, nil
		default:
			t.Fatalf("unexpected request = %s", req.URL.String())
			return nil, nil
		}
	})}
	scraper.douban = provider
	metadata := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Existing title", Overview: "已有简介", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "35763827"}}); err != nil {
		t.Fatal(err)
	}

	degraded, err := scraper.EnrichFromDouban(t.Context(), metadata.ID)
	if !errors.Is(err, ErrDoubanTemporarilyUnavailable) || degraded {
		t.Fatalf("subject failure degraded = %v, err = %v", degraded, err)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot != nil {
		t.Fatalf("subject failure snapshot = %#v, err = %v", snapshot, err)
	}
	subjectFails = false

	degraded, err = scraper.EnrichFromDouban(t.Context(), metadata.ID)
	if err != nil || !degraded {
		t.Fatalf("degraded = %v, err = %v", degraded, err)
	}
	updated, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil || updated.Title != "Existing title" || updated.Overview != "已有简介" {
		t.Fatalf("degraded metadata = %#v, err = %v", updated, err)
	}
	snapshot, err = repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot == nil || !snapshot.Degraded || !strings.Contains(snapshot.Payload, "降级中文标题") {
		t.Fatalf("degraded snapshot = %#v, err = %v", snapshot, err)
	}

	restricted = false
	degraded, err = scraper.EnrichFromDouban(t.Context(), metadata.ID)
	if err != nil || degraded {
		t.Fatalf("restored degraded = %v, err = %v", degraded, err)
	}
	snapshot, err = repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot == nil || snapshot.Degraded || !strings.Contains(snapshot.Payload, "完整中文标题") {
		t.Fatalf("restored snapshot = %#v, err = %v", snapshot, err)
	}
}

func TestDoubanEnrichmentBatchPreservesSeriesChildren(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &series, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "35763827"}, {Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"}}); err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &season, nil); err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &episode, nil); err != nil {
		t.Fatal(err)
	}
	// 即使历史季/集留有豆瓣标识和信息缺口，也不能进入整剧补齐。
	for i, child := range []model.MetadataItem{season, episode} {
		if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: child.ID, Provider: "douban", EntityKind: child.Kind, ExternalID: strconv.Itoa(i + 1)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), child.ID, "tmdb", []byte(`{"id":1}`), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		artType := model.ArtworkTypePoster
		if child.Kind == model.MetadataKindEpisode {
			artType = model.ArtworkTypeStill
		}
		if _, err := scraper.artwork.ImportRemote(t.Context(), child.ID, artType, "tmdb", "https://img.test/child.jpg"); err != nil {
			t.Fatal(err)
		}
	}
	media := model.Media{MetadataID: episode.ID, Path: "/test/series/S01E01.mkv", Title: "Episode", SeasonNum: 1, EpisodeNum: 1}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	// 比较持久化全行，覆盖字段、编号、归属、标识、图片和快照。
	unchanged := func() []string {
		t.Helper()
		var rows []string
		for _, table := range []string{"metadata_items", "metadata_identifiers", "metadata_provider_snapshots", "metadata_artworks", "metadata_artwork_candidates", "media"} {
			column := "metadata_id"
			if table == "metadata_items" {
				column = "id"
			}
			var value string
			if err := repos.DB.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text), '[]'::jsonb)::text FROM "+table+" t WHERE "+column+" IN (?, ?)", season.ID, episode.ID).Scan(&value).Error; err != nil {
				t.Fatal(err)
			}
			rows = append(rows, value)
		}
		return rows
	}
	before := unchanged()
	requests := 0
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != "/rexxar/api/v2/tv/35763827" {
			t.Fatalf("unexpected endpoint: %s", req.URL.Path)
		}
		body := `{"title":"整剧中文名","intro":"整剧简介","rating":{"value":8.5},"cover":{"image":{"large":{"url":"https://img.test/series.jpg"}}}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()
	for range 2 {
		if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
			t.Fatal(err)
		}
	}
	updated, err := repos.Metadata.FindByID(t.Context(), series.ID)
	if err != nil || updated == nil || updated.Title != "整剧中文名" || updated.Overview != "整剧简介" || updated.Rating != 8.5 || updated.Source != "tmdb" {
		t.Fatalf("updated Series = %#v, err = %v", updated, err)
	}
	poster, err := repos.Artwork.FindSelection(t.Context(), series.ID, model.ArtworkTypePoster)
	if err != nil || poster == nil {
		t.Fatalf("Series poster = %#v, err = %v", poster, err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 including cooldown rerun", requests)
	}
	if after := unchanged(); !reflect.DeepEqual(before, after) {
		t.Fatal("Series enrichment modified child metadata or media")
	}
}

func TestDoubanMovieEnrichmentContinuesAfterDegradedSnapshot(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{
		"15000000-0000-0000-0000-000000000001",
		"15000000-0000-0000-0000-000000000002",
	} {
		metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: fmt.Sprintf("电影%d", i+1), Source: "tmdb"}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: strconv.Itoa(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	requests := []string{}
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.Path)
		switch req.URL.Path {
		case "/rexxar/api/v2/movie/1":
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`forbidden`)), Request: req}, nil
		case "/rexxar/api/v2/subject/1":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"电影1"}`)), Request: req}, nil
		case "/rexxar/api/v2/movie/2":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"电影2","intro":"简介"}`)), Request: req}, nil
		default:
			t.Fatalf("unexpected request = %s", req.URL.String())
			return nil, nil
		}
	})}
	scraper.douban = provider
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(requests, ","); got != "/rexxar/api/v2/movie/1,/rexxar/api/v2/subject/1,/rexxar/api/v2/movie/2" {
		t.Fatalf("requests = %s", got)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), "15000000-0000-0000-0000-000000000001", "douban")
	if err != nil || snapshot == nil || !snapshot.Degraded {
		t.Fatalf("degraded snapshot = %#v, err = %v", snapshot, err)
	}
	candidates, err := repos.Metadata.ListDoubanMovieEnrichmentAfter(t.Context(), "", time.Now().UTC().Add(time.Hour), 20)
	if err != nil || len(candidates) != 1 || candidates[0].MetadataID != "15000000-0000-0000-0000-000000000002" {
		t.Fatalf("post-run candidates = %#v, err = %v", candidates, err)
	}
}

func TestDoubanMovieEnrichmentStartsEachTaskDirect(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}, &model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	createCandidate := func(id, externalID string) {
		metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: "电影", Source: "tmdb"}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: externalID}}); err != nil {
			t.Fatal(err)
		}
	}
	createCandidate("16000000-0000-0000-0000-000000000001", "1")

	apiConfig := NewAPIConfigService(zap.NewNop(), repos, NewCryptoService("test-secret", zap.NewNop()))
	enabled := true
	if _, err := apiConfig.Update(t.Context(), "douban", APIConfigPatch{UseProxyPool: &enabled}); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	directCalls := 0
	direct := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, "direct")
		directCalls++
		status := http.StatusOK
		if directCalls == 1 {
			status = http.StatusBadRequest
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"title":"电影"}`)), Request: req}, nil
	})}
	proxy := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, "proxy")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"电影"}`)), Request: req}, nil
	})}
	scraper.douban = doubanProxyTestProvider(apiConfig, direct, proxy)
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	createCandidate("16000000-0000-0000-0000-000000000002", "2")
	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls, ","), "direct,proxy,direct"; got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}

func TestDoubanMovieEnrichmentPausesWithoutAdvancingFailedCursor(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{
		"10000000-0000-0000-0000-000000000001",
		"10000000-0000-0000-0000-000000000002",
		"10000000-0000-0000-0000-000000000003",
	} {
		metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: "电影", Source: "tmdb"}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: strconv.Itoa(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	provider := NewDoubanProvider(nil)
	requests := []string{}
	failSecond := true
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(req.URL.Path, "/rexxar/api/v2/movie/")
		requests = append(requests, id)
		if failSecond && id == "2" {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`unavailable`)), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"电影","intro":"简介"}`)), Request: req}, nil
	})}
	scraper.douban = provider
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	scraper.SetTaskTracker(tasks)
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "1,2" {
		t.Fatalf("first requests = %v", requests)
	}
	cursor, err := repos.Setting.Get(t.Context(), doubanMovieEnrichmentCursorKey)
	if err != nil || cursor != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("cursor = %q, err = %v", cursor, err)
	}
	logResult, err := tasks.ReadDefinitionLog(TaskDefinitionDoubanEnrichment, "", 0)
	if err != nil || !strings.Contains(logResult.Content, "豆瓣接口异常，已暂停本批") || !strings.Contains(logResult.Content, "原因：HTTP 503") || !strings.Contains(logResult.Content, "当前条目将在下次重试，未使用摘要降级") {
		t.Fatalf("paused task log = %q, err = %v", logResult.Content, err)
	}

	failSecond = false
	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "1,2,2,3" {
		t.Fatalf("retry requests = %v", requests)
	}
}

func TestDoubanMovieEnrichmentContinuesAfterNotFound(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{
		"20000000-0000-0000-0000-000000000001",
		"20000000-0000-0000-0000-000000000002",
	} {
		metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: "电影", Source: "tmdb"}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: strconv.Itoa(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	provider := NewDoubanProvider(nil)
	requests := []string{}
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(req.URL.Path, "/rexxar/api/v2/movie/")
		requests = append(requests, id)
		if id == "1" {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`not found`)), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"电影","intro":"简介"}`)), Request: req}, nil
	})}
	scraper.douban = provider
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "1,2" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestDoubanMovieEnrichmentPaginatesUntilExhausted(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= doubanMovieEnrichmentBatchLimit+1; i++ {
		metadata := model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: fmt.Sprintf("30000000-0000-0000-0000-%012d", i)},
			Kind:          model.MetadataKindMovie,
			Title:         "电影",
			Source:        "tmdb",
		}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{
			Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: strconv.Itoa(i),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"title":"电影","intro":"简介"}`)), Request: req}, nil
	})}
	scraper.douban = provider
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if requests != doubanMovieEnrichmentBatchLimit+1 {
		t.Fatalf("requests = %d, want %d", requests, doubanMovieEnrichmentBatchLimit+1)
	}
	cursor, err := repos.Setting.Get(t.Context(), doubanMovieEnrichmentCursorKey)
	if err != nil || cursor != "" {
		t.Fatalf("cursor = %q, err = %v", cursor, err)
	}
}

func TestDoubanMovieEnrichmentLogsActualChangesAndIdleRun(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	var liveLog string
	requests := 0
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 2 {
			logResult, err := tasks.ReadDefinitionLog(TaskDefinitionDoubanEnrichment, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			liveLog = logResult.Content
		}
		title, summary, poster := "已有中文标题", "已有简介", ""
		if strings.HasSuffix(req.URL.Path, "/2") {
			title, summary, poster = "补齐中文标题", "补齐简介", `,"cover":{"image":{"large":{"url":"https://img.test/poster.jpg"}}}`
		}
		body := `{"title":"` + title + `","intro":"` + summary + `"` + poster + `}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider
	for _, item := range []struct {
		title, overview, doubanID string
	}{
		{title: "已有中文标题", overview: "已有简介", doubanID: "1"},
		{title: "English", doubanID: "2"},
	} {
		metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: item.title, Overview: item.overview, Source: "tmdb"}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: item.doubanID}}); err != nil {
			t.Fatal(err)
		}
	}
	scraper.SetTaskTracker(tasks)
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(liveLog, "✅ 刷新") && !strings.Contains(liveLog, "🔄 更新") {
		t.Fatalf("first result was not logged before the second request:\n%s", liveLog)
	}
	if strings.Contains(liveLog, "请求 2") {
		t.Fatalf("final summary was logged before completion:\n%s", liveLog)
	}
	snapshot := tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Metrics["added"] != 1 || snapshot.Recent[0].Metrics["updated"] != 1 || snapshot.Recent[0].Metrics["snapshot_only"] != 1 {
		t.Fatalf("douban enrichment metrics = %#v", snapshot.Recent)
	}
	logResult, err := tasks.ReadDefinitionLog(TaskDefinitionDoubanEnrichment, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ℹ️ 请求 2，更新 1，新增海报 1，仅刷新完整快照 1",
		"✅ 刷新 《已有中文标题》（",
		"：已保存完整豆瓣快照，未补到新的字段或海报",
		"🔄 更新 《补齐中文标题》（",
		"：标题、原名、简介",
		"➕ 新增 《补齐中文标题》（",
		"：豆瓣海报（已设为当前海报）",
	} {
		if !strings.Contains(logResult.Content, want) {
			t.Fatalf("task log missing %q:\n%s", want, logResult.Content)
		}
	}
	for _, unwanted := range []string{"扫描 2", "无变化", "跳过"} {
		if strings.Contains(logResult.Content, unwanted) {
			t.Fatalf("task log contains %q:\n%s", unwanted, logResult.Content)
		}
	}
	for _, detail := range []string{"✅ 刷新 《已有中文标题》（", "🔄 更新 《补齐中文标题》（", "➕ 新增 《补齐中文标题》（"} {
		if count := strings.Count(logResult.Content, detail); count != 1 {
			t.Fatalf("task log contains %d copies of %q:\n%s", count, detail, logResult.Content)
		}
	}

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	logResult, err = tasks.ReadDefinitionLog(TaskDefinitionDoubanEnrichment, "", 0)
	if err != nil || !strings.Contains(logResult.Content, "ℹ️ 未发现待补齐项") || strings.Contains(logResult.Content, "本次无变更") {
		t.Fatalf("idle task log = %q, err = %v", logResult.Content, err)
	}
}

func TestUniqueIdentifierRejectsAmbiguousDoubanMovieIDs(t *testing.T) {
	identifiers := []model.MetadataIdentifier{
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1"},
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "2"},
	}
	if value, ok := uniqueIdentifier(identifiers, "douban", model.MetadataKindMovie); ok || value != "2" {
		t.Fatalf("unique identifier = %q, %v", value, ok)
	}
}
