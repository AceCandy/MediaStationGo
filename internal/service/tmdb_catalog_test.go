package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestTMDbSeasonDetailsPreservesInventoryAndRawJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/42/season/0" {
			http.NotFound(w, r)
			return
		}
		for _, name := range []string{"external_ids", "credits", "translations", "videos"} {
			if !strings.Contains(r.URL.Query().Get("append_to_response"), name) {
				t.Fatalf("append_to_response missing %s: %q", name, r.URL.RawQuery)
			}
		}
		_, _ = io.WriteString(w, `{"id":99,"season_number":0,"name":"特别篇","poster_path":"/season.jpg","external_ids":{"tvdb_id":123},"episodes":[{"id":1001,"episode_number":1,"name":"特别集"}],"future_field":{"kept":true}}`)
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	details, err := provider.GetTVSeasonDetails(t.Context(), 42, 0)
	if err != nil {
		t.Fatal(err)
	}
	if details == nil || details.ID != 99 || details.SeasonNumber != 0 || len(details.Episodes) != 1 || details.Episodes[0].ID != 1001 {
		t.Fatalf("details = %#v", details)
	}
	if details.PosterURL != "https://image.test/t/p/original/season.jpg" || !strings.Contains(string(details.RawJSON), "future_field") {
		t.Fatalf("poster/raw = %q %s", details.PosterURL, details.RawJSON)
	}
}

func TestTMDbGetJSONRetries429AndHidesAPIKey(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	provider := newTMDbTestProvider(server.URL)
	var response map[string]bool
	if err := provider.getJSON(t.Context(), server.URL+"/test?api_key=super-secret", &response); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !response["ok"] {
		t.Fatalf("calls=%d response=%#v", calls.Load(), response)
	}

	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("denied")), Request: req}, nil
	})}
	err := provider.getJSON(t.Context(), server.URL+"/private?api_key=super-secret", &response)
	if err == nil || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "?") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func TestTMDb429WaitHonorsCancellation(t *testing.T) {
	provider := newTMDbTestProvider("https://tmdb.test")
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"10"}}, Body: io.NopCloser(strings.NewReader("busy")), Request: req}, nil
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := provider.getJSON(ctx, "https://tmdb.test/tv/42?api_key=super-secret", &map[string]any{})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("cancellation err=%v elapsed=%v", err, time.Since(started))
	}
}

func newTMDbTestProvider(base string) *TMDbProvider {
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = base
	cfg.Secrets.TMDbImageProxy = "https://image.test/t/p"
	return NewTMDbProvider(cfg, zap.NewNop(), nil)
}
