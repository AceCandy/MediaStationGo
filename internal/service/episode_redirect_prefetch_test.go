package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestNextEpisodeRedirectPrefetch(t *testing.T) {
	for _, source := range []string{"", model.CatalogSourceNFO, model.TaskSystemHongGuo} {
		t.Run(source, func(t *testing.T) {
			e, current := newEpisodeProbeTestEnv(t, source)
			e.SetMediaProbe(NewMediaProbeService(e.repo, nil))
			var mu sync.Mutex
			calls := map[string]int{}
			started := make(chan struct{}, 4)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.UserAgent() != "Player/1" || req.Header.Get("Range") != "bytes=0-0" {
					t.Error("prefetch did not preserve player UA and validation range")
				}
				mu.Lock()
				calls[req.URL.Path]++
				mu.Unlock()
				if req.URL.Path == "/direct/next-a" || req.URL.Path == "/direct/next-b.mkv" {
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("v"))
					return
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-req.Context().Done():
					return
				}
				w.Header().Set("Location", "/direct/"+filepath.Base(req.URL.Path))
				w.WriteHeader(http.StatusFound)
			}))
			defer upstream.Close()
			defer unblock()
			stream := NewStreamService(&config.Config{}, zap.NewNop(), e.repo)
			for _, id := range []string{"next-a", "hidden-file", "later", "cross-season"} {
				if err := e.repo.DB.Model(&model.Media{}).Where("id = ?", id).Update("strm_url", upstream.URL+"/origin/"+id).Error; err != nil {
					t.Fatal(err)
				}
			}
			nextB, err := e.repo.Media.FindByID(t.Context(), "next-b")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.repo.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey, filepath.Dir(nextB.Path)+" => "+upstream.URL+"/origin"); err != nil {
				t.Fatal(err)
			}
			if err := e.repo.Setting.Set(t.Context(), PlaybackRedirectResolvePrefixesSettingKey, upstream.URL+"/origin"); err != nil {
				t.Fatal(err)
			}
			storeRedirectPrefetchTracks(t, e, "next-b", "filled", "hidden-file", "later", "cross-season")
			if err := e.repo.MediaProbe.Upsert(t.Context(), &model.MediaProbeMetadata{MediaID: "next-a", ProbeJSON: "{}", SchemaVersion: ProbeDocumentSchemaVersion}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			canceled, stop := context.WithCancel(ctx)
			stop()
			e.PrefetchNextEpisodeRedirects(canceled, current.ID, "viewer", "Player/1", stream)
			e.PrefetchNextEpisodeRedirects(ctx, current.ID, "viewer", "", stream)
			mu.Lock()
			if len(calls) != 0 {
				t.Error("canceled or missing-UA prefetch requested upstream")
			}
			mu.Unlock()
			returned := make(chan struct{})
			go func() {
				e.PrefetchNextEpisodeRedirects(ctx, current.ID, "viewer", "Player/1", stream)
				close(returned)
			}()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("prefetch blocked its caller")
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("next episode was not prefetched")
			}
			mu.Lock()
			if calls["/origin/next-a"] != 0 {
				t.Error("redirect requested before valid track metadata")
			}
			mu.Unlock()
			storeRedirectPrefetchTracks(t, e, "next-a")
			unblock()
			deadline := time.Now().Add(5 * time.Second)
			for {
				stream.redirectResolver.mu.Lock()
				cached := len(stream.redirectResolver.cache)
				stream.redirectResolver.mu.Unlock()
				if cached == 2 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("cached next episode versions = %d, want 2", cached)
				}
				time.Sleep(10 * time.Millisecond)
			}
			for _, id := range []string{"next-a", "next-b"} {
				media, err := e.repo.Media.FindByID(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodGet, "/Videos/"+id+"/stream", nil)
				req.Header.Set("User-Agent", "Player/1")
				w := httptest.NewRecorder()
				if err := stream.ServeMedia(w, req, media); err != nil || w.Code != http.StatusFound {
					t.Fatalf("prefetched playback status=%d error=%v", w.Code, err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(calls) != 4 || calls["/origin/next-a"] != 1 || calls["/origin/next-b.mkv"] != 1 {
				t.Fatalf("prefetch/playback upstream calls = %v", calls)
			}
		})
	}
}

func TestNextEpisodeRedirectPrefetchCancellation(t *testing.T) {
	e, current := newEpisodeProbeTestEnv(t, "")
	e.SetMediaProbe(NewMediaProbeService(e.repo, nil))
	started, stopped := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer upstream.Close()
	stream := NewStreamService(&config.Config{}, zap.NewNop(), e.repo)
	media, err := e.repo.Media.FindByID(t.Context(), "next-a")
	if err != nil {
		t.Fatal(err)
	}
	media.STRMURL = upstream.URL
	stream.prefetchRedirect(t.Context(), media, "Player/1")
	select {
	case <-started:
		t.Fatal("unconfigured prefix requested upstream")
	default:
	}
	if err := e.repo.DB.Model(media).Update("strm_url", upstream.URL).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Setting.Set(t.Context(), PlaybackRedirectResolvePrefixesSettingKey, upstream.URL); err != nil {
		t.Fatal(err)
	}
	storeRedirectPrefetchTracks(t, e, "next-a", "next-b", "filled")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e.PrefetchNextEpisodeRedirects(ctx, current.ID, "viewer", "Player/1", stream)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("prefetch did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("lifecycle cancellation did not stop upstream")
	}
	deadline := time.Now().Add(time.Second)
	for {
		stream.redirectResolver.mu.Lock()
		flights, cached := len(stream.redirectResolver.flights), len(stream.redirectResolver.cache)
		stream.redirectResolver.mu.Unlock()
		if flights == 0 {
			if cached != 0 {
				t.Fatal("canceled prefetch was cached")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled prefetch retained in-flight request")
		}
		time.Sleep(time.Millisecond)
	}
}

func storeRedirectPrefetchTracks(t *testing.T, e *EmbyService, ids ...string) {
	t.Helper()
	document, err := MarshalProbeDocument(probeResultFixture().Document)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := e.repo.MediaProbe.Upsert(t.Context(), &model.MediaProbeMetadata{MediaID: id, ProbeJSON: document, SchemaVersion: ProbeDocumentSchemaVersion}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNextEpisodeRedirectPrefetchWaitsForBackfill(t *testing.T) {
	e, current := newEpisodeProbeTestEnv(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probeStarted, probeRelease := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(probeRelease) }) }
	defer unblock()
	runner := &stubMediaProbeRunner{result: probeResultFixture(), onProbe: func() { close(probeStarted); <-probeRelease }}
	e.SetMediaProbe(NewMediaProbeService(e.repo, runner).SetTaskTracker(zap.NewNop(), NewTaskTrackerService(zap.NewNop(), nil), ctx))
	storeRedirectPrefetchTracks(t, e, "current", "next-b", "filled")
	redirectStarted := make(chan struct{}, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/direct" {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("v"))
			return
		}
		redirectStarted <- struct{}{}
		w.Header().Set("Location", "/direct")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	next, err := e.repo.Media.FindByID(t.Context(), "next-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey, next.Path+" => "+upstream.URL+"/origin"); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Setting.Set(t.Context(), PlaybackRedirectResolvePrefixesSettingKey, upstream.URL+"/origin"); err != nil {
		t.Fatal(err)
	}
	out, err := e.PlaybackInfo(t.Context(), current.ID, "viewer")
	if err != nil || out == nil {
		t.Fatalf("PlaybackInfo error=%v", err)
	}
	stream := NewStreamService(&config.Config{}, zap.NewNop(), e.repo)
	e.PrefetchNextEpisodeRedirects(ctx, current.ID, "viewer", "Player/1", stream)
	select {
	case <-probeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("track backfill did not start")
	}
	select {
	case <-redirectStarted:
		t.Fatal("redirect overlapped unfinished track probe")
	case <-time.After(1200 * time.Millisecond):
	}
	unblock()
	select {
	case <-redirectStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("completed backfill did not allow redirect prefetch")
	}
	if _, ok := e.mediaProbe.Load(t.Context(), next.ID); !ok {
		t.Fatal("redirect started without persisted track document")
	}
	// 等到回填与预取均完成后再释放测试数据库。
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.mediaProbe.autoMu.Lock()
		busy := e.mediaProbe.autoRunning
		e.mediaProbe.autoMu.Unlock()
		stream.redirectResolver.mu.Lock()
		cached := len(stream.redirectResolver.cache)
		stream.redirectResolver.mu.Unlock()
		if !busy && cached == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background work did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
