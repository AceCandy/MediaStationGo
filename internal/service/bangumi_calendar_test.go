package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestExtensionCalendarPersistentRefresh(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.BangumiCalendarSnapshot{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db).BangumiCalendar
	var calls atomic.Int32
	entered := make(chan struct{}, 4)
	release := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if n > 1 {
			http.Error(w, "unavailable", 503)
			return
		}
		items := []map[string]any{}
		for i := 1; i <= 30; i++ {
			items = append(items, map[string]any{"id": i, "name_cn": fmt.Sprintf("作品%d", i), "air_date": "2026-10-01"})
		}
		days := []any{map[string]any{"weekday": map[string]int{"id": 1}, "items": items}}
		for day := 2; day <= 7; day++ {
			days = append(days, map[string]any{"weekday": map[string]int{"id": day}, "items": items[:1]})
		}
		_ = json.NewEncoder(w).Encode(days)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	makeProvider := func() *BangumiProvider {
		b := NewBangumiProvider(&config.Config{}, zap.NewNop())
		b.base = server.URL
		b.client = server.Client()
		b.calendar.repo = repo
		b.calendar.ctx = ctx
		return b
	}
	b := makeProvider()
	defer b.closeCalendar()
	const count = 12
	var wg sync.WaitGroup
	results := make(chan WeeklyCalendar, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := b.WeeklyCalendar(t.Context()); results <- v; errs <- e }()
	}
	<-entered
	release <- struct{}{}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for v := range results {
		if len(v.Items) != 36 || v.Stale || v.Items[0].Weekday != 1 || v.Items[0].BroadcastTime != nil || v.Items[0].Season != nil {
			t.Fatalf("bad calendar %+v", v)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("cold fetch calls=%d", calls.Load())
	}
	restarted := makeProvider()
	defer restarted.closeCalendar()
	if v, e := restarted.WeeklyCalendar(t.Context()); e != nil || len(v.Items) != 36 || calls.Load() != 1 {
		t.Fatalf("persistent reuse: %v %+v", e, v)
	}
	if rail, e := restarted.Calendar(t.Context()); e != nil || len(rail) != 24 {
		t.Fatalf("rail len=%d err=%v", len(rail), e)
	}
	old := time.Now().Add(-7 * time.Hour)
	if e := db.Model(&model.BangumiCalendarSnapshot{}).Where("key = ?", "weekly").Update("fetched_at", old).Error; e != nil {
		t.Fatal(e)
	}
	stale, e := b.WeeklyCalendar(t.Context())
	if e != nil || !stale.Stale || len(stale.Items) != 36 {
		t.Fatalf("stale read: %+v %v", stale, e)
	}
	<-entered
	for i := 0; i < 10; i++ {
		v, e := b.WeeklyCalendar(t.Context())
		if e != nil || !v.Stale {
			t.Fatal("stale read blocked or failed")
		}
	}
	b.calendar.mu.Lock()
	flight := b.calendar.flight
	b.calendar.mu.Unlock()
	release <- struct{}{}
	<-flight.done
	if flight.err == nil {
		t.Fatal("expected refresh failure")
	}
	row, e := repo.Load(t.Context())
	if e != nil || !row.FetchedAt.Before(time.Now().Add(-6*time.Hour)) {
		t.Fatal("failure overwrote snapshot")
	}
	if _, e := b.WeeklyCalendar(t.Context()); e != nil || calls.Load() != 2 {
		t.Fatalf("backoff calls=%d err=%v", calls.Load(), e)
	}
	// 更早启动的刷新不能覆盖数据库中较新的成功快照。
	if e := repo.Save(t.Context(), model.BangumiCalendarSnapshot{Payload: "[]", FetchedAt: old.Add(-time.Hour)}); e != nil {
		t.Fatal(e)
	}
	row, _ = repo.Load(t.Context())
	if row.Payload == "[]" {
		t.Fatal("older refresh replaced snapshot")
	}
}

func TestExtensionCalendarValidationAndCancellation(t *testing.T) {
	for _, payload := range []string{"null", `[{"weekday":{"id":0},"items":[]}]`, `[{"weekday":{"id":1},"items":[{"id":0,"name":"bad"}]}]`} {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
			defer server.Close()
			b := NewBangumiProvider(&config.Config{}, zap.NewNop())
			b.base = server.URL
			b.client = server.Client()
			defer b.closeCalendar()
			if _, err := b.WeeklyCalendar(t.Context()); err == nil {
				t.Fatal("invalid calendar accepted")
			}
		})
	}
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	runtime, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()
	b := NewBangumiProvider(&config.Config{}, zap.NewNop())
	b.base = server.URL
	b.client = server.Client()
	b.calendar.ctx = runtime
	caller, cancelCaller := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.WeeklyCalendar(caller); done <- err }()
	<-entered
	cancelCaller()
	if err := <-done; err != context.Canceled {
		t.Fatalf("caller cancellation: %v", err)
	}
	cancelRuntime()
	b.closeCalendar()
	if _, err := b.WeeklyCalendar(t.Context()); err != context.Canceled {
		t.Fatalf("shutdown accepted request: %v", err)
	}
}
