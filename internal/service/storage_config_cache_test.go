package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestCloudResolveHotCacheRefreshesInBackground(t *testing.T) {
	var resolves atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fs/get" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		n := resolves.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"code":200,"data":{"raw_url":"http://cdn.local/%d.mkv"}}`, n)
	}))
	defer upstream.Close()

	_, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "token",
		},
	}); err != nil {
		t.Fatal(err)
	}

	link, cacheHit, err := storage.CloudResolveWithCacheStatus(t.Context(), "openlist", "/Movies/f1.mkv", "Player/1")
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != "http://cdn.local/1.mkv" || cacheHit || resolves.Load() != 1 {
		t.Fatalf("first resolve link=%#v cacheHit=%t resolves=%d", link, cacheHit, resolves.Load())
	}
	for i := 0; i < cloudResolveHotHitThreshold-1; i++ {
		link, cacheHit, err = storage.CloudResolveWithCacheStatus(t.Context(), "openlist", "/Movies/f1.mkv", "Player/1")
		if err != nil {
			t.Fatal(err)
		}
		if link.URL != "http://cdn.local/1.mkv" || !cacheHit || resolves.Load() != 1 {
			t.Fatalf("cached resolve link=%#v cacheHit=%t resolves=%d", link, cacheHit, resolves.Load())
		}
	}

	key := storage.resolveCacheKey("openlist", "/Movies/f1.mkv", "Player/1")
	storage.resolveMu.Lock()
	entry := storage.resolveCache[key]
	entry.hits = cloudResolveHotHitThreshold
	entry.expiresAt = time.Now().Add(5 * time.Second)
	storage.resolveCache[key] = entry
	storage.resolveMu.Unlock()

	link, err = storage.CloudResolve(t.Context(), "openlist", "/Movies/f1.mkv", "Player/1")
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != "http://cdn.local/1.mkv" {
		t.Fatalf("hot hit should return cached link immediately, got %s", link.URL)
	}
	deadline := time.Now().Add(2 * time.Second)
	for resolves.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if resolves.Load() < 2 {
		t.Fatalf("background refresh did not run, resolves=%d", resolves.Load())
	}
	link, err = storage.CloudResolve(t.Context(), "openlist", "/Movies/f1.mkv", "Player/1")
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != "http://cdn.local/2.mkv" {
		t.Fatalf("refreshed link = %s, want second URL", link.URL)
	}
}

func TestCloudResolveCacheTTLUsesOneHourForCloudPlaybackLinks(t *testing.T) {
	for _, typ := range []string{"cloud115", "clouddrive2", "openlist"} {
		if got := cloudResolveCacheTTL(typ); got != time.Hour {
			t.Fatalf("%s cloud resolve cache ttl = %v, want 1h", typ, got)
		}
	}
}

func TestResolveHTTPRedirectDoesNotExposeTargetOnTransportError(t *testing.T) {
	svc := NewStorageConfigService(zap.NewNop(), nil, nil)
	svc.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed")
	})}

	_, _, err := svc.ResolveHTTPRedirectWithCacheStatus(t.Context(), "https://openlist.example.test/d/movie.mp4?token=secret", "player")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "openlist.example.test") {
		t.Fatalf("transport error must not expose target URL: %v", err)
	}
}
