package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"go.uber.org/zap"
)

type observedImageWaitContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *observedImageWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestRemoteImageFetchCoalescesAndCancels(t *testing.T) {
	for _, mode := range []string{"shared", "cancel_waiter", "cancel_owner"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			proxy := NewImageProxy(&config.Config{}, zap.NewNop())
			started, release := make(chan struct{}), make(chan struct{})
			want := testArtworkPNG(t, 2, 2)
			var calls atomic.Int32
			proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					close(started)
					select {
					case <-release:
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(want)), Request: req}, nil
			})}
			fetch := func(callCtx context.Context, result chan<- error) {
				data, _, _, err := proxy.fetchRemoteImageUncached(callCtx, "https://image.tmdb.org/a.png", "image.tmdb.org", false)
				if err == nil && !bytes.Equal(data, want) {
					t.Error("shared image differs")
				}
				result <- err
			}
			ownerCtx, cancelOwner := context.WithCancel(ctx)
			defer cancelOwner()
			ownerResult, waiterResult := make(chan error, 1), make(chan error, 1)
			go fetch(ownerCtx, ownerResult)
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			waitCtx, cancelWaiter := context.WithCancel(ctx)
			defer cancelWaiter()
			observed := &observedImageWaitContext{Context: waitCtx, waiting: make(chan struct{})}
			go fetch(observed, waiterResult)
			select {
			case <-observed.waiting:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode == "cancel_waiter" {
				cancelWaiter()
				if err := <-waiterResult; err != context.Canceled {
					t.Fatalf("waiter err=%v", err)
				}
			}
			if mode == "cancel_owner" {
				cancelOwner()
			} else {
				close(release)
			}
			ownerErr := <-ownerResult
			if (ownerErr != nil) != (mode == "cancel_owner") {
				t.Fatalf("owner err=%v", ownerErr)
			}
			if mode != "cancel_waiter" {
				if err := <-waiterResult; err != nil {
					t.Fatal(err)
				}
			}
			wantCalls := int32(1)
			if mode == "cancel_owner" {
				wantCalls = 2
			}
			if calls.Load() != wantCalls {
				t.Fatalf("requests=%d want=%d", calls.Load(), wantCalls)
			}
		})
	}
}

func TestFanartArtworkUsesMatchingMediaKind(t *testing.T) {
	for _, kind := range []string{"movie", "tv"} {
		t.Run(kind, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Secrets.FanartAPIKey = "test-key"
			provider := NewFanartProvider(cfg, zap.NewNop())
			calls := 0
			provider.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				want := "/v3/movies/123"
				if kind == "tv" {
					want = "/v3/tv/456"
				}
				if req.URL.Path != want {
					t.Errorf("path=%s want=%s", req.URL.Path, want)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewBufferString(`{}`)), Request: req}, nil
			})}
			scraper := &ScraperService{fanart: provider, log: zap.NewNop()}
			scraper.applyFanartArtwork(t.Context(), &Match{TMDbID: 123, TheTVDBID: "456"}, kind)
			if calls != 1 {
				t.Fatalf("requests=%d want=1", calls)
			}
			if kind == "tv" {
				scraper.applyFanartArtwork(t.Context(), &Match{TMDbID: 123}, kind)
				if calls != 1 {
					t.Fatal("TV without TVDB ID must not query movie artwork")
				}
			}
		})
	}
}

func TestPeopleImageTimeoutCooldown(t *testing.T) {
	cfg := &config.Config{}
	cfg.App.DataDir = t.TempDir()
	proxy := NewImageProxy(cfg, zap.NewNop())
	var calls atomic.Int32
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, context.DeadlineExceeded
	})}
	store := NewPeopleImageStore(cfg, nil, proxy)
	source := "https://image.tmdb.org/timeout.png"
	for i := 0; i < 2; i++ {
		if _, err := store.ImportCached(t.Context(), source); err == nil {
			t.Fatal("expected timeout/cooldown")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("timeout requests=%d want=1", calls.Load())
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	other := "https://image.tmdb.org/canceled.png"
	if _, err := store.ImportCached(canceled, other); err != context.Canceled {
		t.Fatalf("cancel err=%v", err)
	}
	var cached peopleImageSource
	if store.sources.GetJSON(t.Context(), other, &cached) {
		t.Fatal("caller cancellation must not populate cooldown")
	}
}
