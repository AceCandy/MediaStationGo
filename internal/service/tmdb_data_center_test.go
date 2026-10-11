package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"gorm.io/gorm/logger"
)

func tmdbCenterTest(t *testing.T, h http.HandlerFunc) (*TMDbDataCenter, *TMDbProvider) {
	t.Helper()
	db, e := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(model.AllModels()...); e != nil {
		t.Fatal(e)
	}
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = up.URL
	repo := repository.New(db)
	p := NewTMDbProvider(cfg, zap.NewNop(), nil)
	s := NewTMDbDataCenter(repo, p)
	p.catalog = s
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	t.Cleanup(func() { cancel(); s.Wait() })
	return s, p
}
func TestTMDbCenterRequestReuseAndBudget(t *testing.T) {
	var calls atomic.Int32
	s, p := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search/movie" {
			fmt.Fprint(w, `{"results":[{"id":9,"title":"查询摘要"}],"total_pages":1}`)
			return
		}
		fmt.Fprint(w, `{"id":9,"title":"电影","release_date":"2026-10-01","credits":{"cast":[],"crew":[]},"external_ids":{}}`)
	})
	ctx := context.Background()
	raw := p.base + "/movie/9?api_key=test-key&language=zh-CN&append_to_response=credits,external_ids"
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := p.getJSONRaw(ctx, raw, &json.RawMessage{}); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
	if _, e := p.getJSONRaw(ctx, p.base+"/movie/9?api_key=test-key&language=zh-CN", &json.RawMessage{}); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("base failed to reuse detail")
	}
	for i := 0; i < 2; i++ {
		if _, e := p.getJSONRaw(ctx, p.base+"/search/movie?query=test&api_key=test-key", &json.RawMessage{}); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("query cache miss")
	}
	budget := &tmdbBudget{limit: 1}
	ctx = context.WithValue(context.WithValue(ctx, tmdbBudgetKey{}, budget), tmdbForceKey{}, true)
	if _, e := p.getJSONRaw(ctx, raw, &json.RawMessage{}); e != nil {
		t.Fatal(e)
	}
	_, e := p.getJSONRaw(ctx, raw, &json.RawMessage{})
	if !errors.Is(e, errTMDbBudget) {
		t.Fatalf("budget %v", e)
	}
	if budget.used.Load() != 1 {
		t.Fatal("wrong request count")
	}
	var n int64
	s.repo.DB.Model(&model.TMDbRefreshJob{}).Where("status='running'").Count(&n)
	if n != 0 {
		t.Fatal("lease leaked")
	}
}
func TestTMDbCenterCooldownSurvivesRestart(t *testing.T) {
	var calls atomic.Int32
	s, p := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	})
	ctx := context.Background()
	_, e := p.getJSONRaw(ctx, p.base+"/movie/1?api_key=test-key", &json.RawMessage{})
	if !isTMDbHTTPStatus(e, 429) {
		t.Fatal(e)
	}
	restarted := NewTMDbDataCenter(s.repo, p)
	p.catalog = restarted
	_, e = p.getJSONRaw(ctx, p.base+"/movie/2?api_key=test-key", &json.RawMessage{})
	if !errors.Is(e, errTMDbDeferred) || calls.Load() != 1 {
		t.Fatalf("cooldown ignored %v %d", e, calls.Load())
	}
	restarted.Wait()
}
func TestTMDbCenterCanceledWaiterDoesNotCancelShared(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	_, p := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"id":7,"title":"电影"}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	url := p.base + "/movie/7?api_key=test-key"
	first := make(chan error, 1)
	go func() { _, e := p.getJSONRaw(ctx, url, &json.RawMessage{}); first <- e }()
	<-started
	second := make(chan error, 1)
	go func() { _, e := p.getJSONRaw(context.Background(), url, &json.RawMessage{}); second <- e }()
	cancel()
	if e := <-first; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	close(release)
	select {
	case e := <-second:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shared request stuck")
	}
}

func TestTMDbCenterPeriodicBudgetDoesNotLeakToWorker(t *testing.T) {
	var calls atomic.Int32
	s, p := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"id":9,"title":"电影"}`) })
	ctx := context.WithValue(context.Background(), tmdbBudgetKey{}, &tmdbBudget{limit: 0})
	ctx = context.WithValue(ctx, tmdbAutomaticKey{}, true)
	_, err := s.Fetch(ctx, p.base+"/movie/9?api_key=test-key&language=zh-CN", &json.RawMessage{})
	if !errors.Is(err, errTMDbBudget) {
		t.Fatalf("budget error %v", err)
	}
	rows, err := s.repo.TMDb.Due(context.Background(), false, 20)
	if err != nil || len(rows) != 0 {
		t.Fatalf("periodic work escaped to explicit worker %d %v", len(rows), err)
	}
	n, done, err := s.RunDue(context.Background(), true, 1)
	if err != nil || n != 1 || done != 1 || calls.Load() != 1 {
		t.Fatalf("next period %d %d %v calls=%d", n, done, err, calls.Load())
	}
}

func TestTMDbCenterOfflineMigrationAndMaintenance(t *testing.T) {
	var calls atomic.Int32
	s, _ := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("offline migration called upstream")
	})
	at := time.Now().Add(-120 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	for _, id := range []string{"10", "11"} {
		item := model.MetadataItem{Kind: "movie", Title: "本地标题", Source: "tmdb"}
		if e := s.repo.DB.Create(&item).Error; e != nil {
			t.Fatal(e)
		}
		ident := model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: id}
		if e := s.repo.DB.Create(&ident).Error; e != nil {
			t.Fatal(e)
		}
		if id == "10" {
			old := model.MetadataProviderSnapshot{MetadataID: item.ID, Provider: "tmdb", Payload: `{"id":10,"title":"旧来源","release_date":"2000-01-01","credits":{"cast":[],"crew":[]}}`, FetchedAt: at}
			if e := s.repo.DB.Create(&old).Error; e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := s.Migrate(context.Background(), nil, "test"); e != nil {
		t.Fatal(e)
	}
	subject, e := s.repo.TMDb.SubjectIdentity(context.Background(), "movie", 10)
	if e != nil || subject.FetchedAt == nil || !subject.FetchedAt.Equal(at) {
		t.Fatalf("timestamp changed %#v %v", subject, e)
	}
	skeleton, e := s.repo.TMDb.SubjectIdentity(context.Background(), "movie", 11)
	if e != nil || skeleton.Quality > 1 {
		t.Fatalf("identity missing %#v %v", skeleton, e)
	}
	rows, e := s.repo.TMDb.Due(context.Background(), true, 20)
	if e != nil || len(rows) != 2 {
		t.Fatalf("missing maintenance jobs %d %v", len(rows), e)
	}
	rows, e = s.repo.TMDb.Due(context.Background(), false, 20)
	if e != nil || len(rows) != 0 || calls.Load() != 0 {
		t.Fatal("migration queued network work")
	}
	if e = s.Migrate(context.Background(), nil, "test"); e != nil {
		t.Fatal(e)
	}
}

func TestTMDbCenterConfigurationChangeRetiresOldJob(t *testing.T) {
	var calls atomic.Int32
	s, p := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"id":9,"title":"电影"}`) })
	ctx := context.Background()
	req, e := s.parseRequest(ctx, p.base+"/movie/9?api_key=test-key&language=zh-CN")
	if e != nil {
		t.Fatal(e)
	}
	req.Key = "old-key"
	req.ConfigKey = "old-config"
	req.Automatic = true
	if _, e = s.repo.TMDb.Enqueue(ctx, req, "", false); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.RunDue(ctx, true, 1); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.RunDue(ctx, true, 1); e != nil {
		t.Fatal(e)
	}
	old, e := s.repo.TMDb.Job(ctx, "old-key")
	if e != nil || old.Status != "retired" || calls.Load() != 1 {
		t.Fatalf("old config repeated request: %d %#v %v", calls.Load(), old, e)
	}
}

func TestTMDbCenterFailedAvatarRefreshKeepsOldImage(t *testing.T) {
	s, _ := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected JSON request") })
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	calls := 0
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
	})}
	s.people = NewPeopleImageStore(cfg, s.repo.Person, proxy)
	source := filepath.Join(cfg.App.DataDir, "avatar.png")
	png := testArtworkPNG(t, 4, 4)
	if e := os.WriteFile(source, png, 0600); e != nil {
		t.Fatal(e)
	}
	key, e := s.people.Import(t.Context(), "tmdb", source)
	if e != nil {
		t.Fatal(e)
	}
	req := repository.TMDbSourceRequest{Kind: "person", TMDbID: 5, Component: "detail", Language: "zh-CN"}
	if e = s.repo.TMDb.SaveResponse(t.Context(), req, []byte(`{"id":5,"name":"演员","profile_path":"/avatar.png"}`), time.Now(), ""); e != nil {
		t.Fatal(e)
	}
	p, e := s.repo.TMDb.PersonIdentity(t.Context(), 5)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.repo.TMDb.SavePersonImage(t.Context(), p, key, false); e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	if e = s.ServeImage(t.Context(), w, httptest.NewRequest("GET", "/image", nil), "person", 5, "profile", true); e != nil {
		t.Fatal(e)
	}
	if calls == 0 || w.Header().Get("Cache-Control") != "private, no-store" || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("old image/cache semantics: calls=%d cache=%s", calls, w.Header().Get("Cache-Control"))
	}
}
