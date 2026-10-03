package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func waitDanmuFlights(t *testing.T, svc *HongGuoDanmuService, ready func() bool) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		svc.mu.Lock()
		ok := ready()
		svc.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatal("danmu flight state did not advance")
		}
	}
}

func TestHongGuoDanmuCoalescesRequests(t *testing.T) {
	repos, _, svc := newDanmuTestServices(t)
	for _, source := range []string{"123", "125"} {
		if err := repos.HongGuo.InsertDanmus(t.Context(), []model.HongGuoDanmu{{SourceID: source, EpisodeNumber: 1, CommentID: "10", OffsetMS: 0, Content: "history"}}); err != nil {
			t.Fatal(err)
		}
	}
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls, canceled atomic.Int32
	svc.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"code":0,"data":{"video_model":{"video_duration":2}}}`
		if strings.Contains(r.URL.Path, "commentapi") {
			body = `{"code":0,"data":{"data_list":[{"comment":{"comment_id":"20","common":{"content":{"text":"live"}},"expand":{"offset_time":1000}}}],"extra":{"next_query_danmaku_list_time":30000}}}`
		} else {
			select {
			case <-release:
			case <-r.Context().Done():
				canceled.Add(1)
				return nil, r.Context().Err()
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	type result struct {
		data []byte
		err  error
	}
	start := func(ctx context.Context, source string, episode int, video string) <-chan result {
		done := make(chan result, 1)
		go func() {
			data, err := svc.Get(ctx, source, episode, video)
			done <- result{data, err}
		}()
		return done
	}
	read := func(done <-chan result) result {
		t.Helper()
		select {
		case output := <-done:
			return output
		case <-time.After(3 * time.Second):
			t.Fatal("danmu request stuck")
			return result{}
		}
	}
	leaderCtx, cancelLeader := context.WithCancel(t.Context())
	defer cancelLeader()
	leader := start(leaderCtx, "123", 1, "456")
	otherEpisode := start(t.Context(), "123", 2, "457")
	otherSource := start(t.Context(), "124", 1, "458")
	waitDanmuFlights(t, svc, func() bool { return len(svc.flights) == 3 && calls.Load() == 3 })
	waiterCtx, cancelWaiter := context.WithCancel(t.Context())
	defer cancelWaiter()
	waiter := start(waiterCtx, "123", 1, "456")
	first := start(t.Context(), "123", 1, "456")
	second := start(t.Context(), "123", 1, "456")
	waitDanmuFlights(t, svc, func() bool {
		for key, flight := range svc.flights {
			if key.source == "123" && key.episode == 1 {
				return flight.waiters == 4
			}
		}
		return false
	})
	fallback, err := svc.Get(t.Context(), "125", 1, "459")
	if err != nil || !strings.Contains(string(fallback), ">history</d>") || calls.Load() != 3 {
		t.Fatal("saturation did not return history without fetching")
	}
	cancelLeader()
	cancelWaiter()
	if !errors.Is(read(leader).err, context.Canceled) || !errors.Is(read(waiter).err, context.Canceled) || canceled.Load() != 0 {
		t.Fatal("one caller canceled the shared fetch")
	}
	once.Do(func() { close(release) })
	a, b := read(first), read(second)
	if a.err != nil || b.err != nil || string(a.data) != string(b.data) || strings.Count(string(a.data), "<d p=") != 2 {
		t.Fatal("waiters did not receive merged XML")
	}
	a.data[0] = '!'
	if b.data[0] == '!' {
		t.Fatal("responses share mutable bytes")
	}
	for _, done := range []<-chan result{otherEpisode, otherSource} {
		output := read(done)
		if output.err != nil || strings.Count(string(output.data), "<d p=") != 1 {
			t.Fatal("source or episode isolation failed")
		}
	}
	svc.Close()
	if calls.Load() != 6 {
		t.Fatalf("expected three fetches, upstream calls=%d", calls.Load())
	}
	rows, err := repos.HongGuo.Danmus(t.Context(), "123", 1)
	if err != nil || len(rows) != 2 || len(svc.slots) != 0 || len(svc.flights) != 0 {
		t.Fatal("shared fetch was not persisted or cleaned up")
	}
}

func TestHongGuoDanmuSharedFetchCancellation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "last_waiter", true: "shutdown"}[shutdown], func(t *testing.T) {
			_, _, svc := newDanmuTestServices(t)
			entered, canceled := make(chan struct{}), make(chan struct{})
			svc.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-r.Context().Done()
				close(canceled)
				return nil, r.Context().Err()
			})})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := svc.Get(ctx, "123", 1, "456"); done <- err }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("fetch did not start")
			}
			if shutdown {
				svc.Close()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled request returned %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled request stuck")
			}
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("fetch was not canceled")
			}
			svc.Close()
			if len(svc.slots) != 0 || len(svc.flights) != 0 {
				t.Fatal("canceled fetch retained resources")
			}
		})
	}
}

func TestHongGuoDanmuFlightConfigurationIsolation(t *testing.T) {
	_, cfg, svc := newDanmuTestServices(t)
	if _, err := cfg.Update(t.Context(), "hongguo", APIConfigPatch{HongGuoApp: &hongguo.DanmuAppConfig{Cookie: "FAKE_OLD"}}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan string, 3)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	svc.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "commentapi") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"data_list":[],"extra":{"next_query_danmaku_list_time":30000}}}`))}, nil
		}
		entered <- r.Header.Get("Cookie")
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"video_model":{"video_duration":2}}}`))}, nil
	})})
	done := make(chan error, 3)
	start := func(video, cookie string) {
		t.Helper()
		go func() { _, err := svc.Get(t.Context(), "123", 1, video); done <- err }()
		select {
		case got := <-entered:
			if got != cookie {
				t.Fatal("wrong configuration snapshot")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("different configuration or video reused a flight")
		}
	}
	start("456", "FAKE_OLD")
	if _, err := cfg.Update(t.Context(), "hongguo", APIConfigPatch{HongGuoApp: &hongguo.DanmuAppConfig{Cookie: "FAKE_NEW"}}); err != nil {
		t.Fatal(err)
	}
	start("456", "FAKE_NEW")
	start("457", "FAKE_NEW")
	off := false
	if _, err := cfg.Update(t.Context(), "hongguo", APIConfigPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(t.Context(), "123", 1, "456"); err != nil {
		t.Fatal("disabled request joined pending work")
	}
	once.Do(func() { close(release) })
	for n := 0; n < 3; n++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("isolated request stuck")
		}
	}
}

func TestHongGuoDanmuCanceledFlightDoesNotRemoveReplacement(t *testing.T) {
	_, _, svc := newDanmuTestServices(t)
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	var oldOnce, newOnce sync.Once
	t.Cleanup(func() {
		oldOnce.Do(func() { close(oldRelease) })
		newOnce.Do(func() { close(newRelease) })
	})
	var calls atomic.Int32
	svc.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "commentapi") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"data_list":[],"extra":{"next_query_danmaku_list_time":30000}}}`))}, nil
		}
		if calls.Add(1) == 1 {
			<-r.Context().Done()
			<-oldRelease
			return nil, r.Context().Err()
		}
		select {
		case <-newRelease:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"video_model":{"video_duration":2}}}`))}, nil
	})})
	start := func(ctx context.Context) <-chan error {
		done := make(chan error, 1)
		go func() { _, err := svc.Get(ctx, "123", 1, "456"); done <- err }()
		return done
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	old := start(ctx)
	waitDanmuFlights(t, svc, func() bool { return calls.Load() == 1 })
	cancel()
	select {
	case err := <-old:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("old waiter did not cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old waiter stuck")
	}
	replacement := start(t.Context())
	waitDanmuFlights(t, svc, func() bool { return calls.Load() == 2 && len(svc.flights) == 1 })
	oldOnce.Do(func() { close(oldRelease) })
	waitDanmuFlights(t, svc, func() bool { return len(svc.slots) == 1 })
	waiter := start(t.Context())
	waitDanmuFlights(t, svc, func() bool {
		for _, flight := range svc.flights {
			return len(svc.flights) == 1 && flight.waiters == 2
		}
		return false
	})
	newOnce.Do(func() { close(newRelease) })
	for _, done := range []<-chan error{replacement, waiter} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("replacement waiter stuck")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("old flight removed its replacement")
	}
}
