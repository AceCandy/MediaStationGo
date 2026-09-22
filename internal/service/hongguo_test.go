package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type hongGuoTestTransport func(*http.Request) (*http.Response, error)

func (f hongGuoTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/novel/player/video_detail/v1/" {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return nil, err
		}
		body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"video_data": map[string]string{"series_id_str": input["series_id"]}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: r}, nil
	}
	return f(r)
}

func TestHongGuoImportTaskAndIsolation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	s := NewHongGuoService(repos, tasks, nil, t.TempDir())
	requests := 0
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		body := `_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"9000000000000000001","series_name":"独立测试作品","episode_cnt":1,"episode_right_text":"全1集"}}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	ctx := context.Background()
	if err := s.Run(ctx, TaskKindHongGuoRefresh, "9000000000000000001"); err != nil {
		t.Fatal(err)
	}
	page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Trigger != TaskTriggerManual || page.Items[0].Status != TaskStatusCompleted {
		t.Fatalf("task=%+v err=%v", page, err)
	}
	legacy, err := tasks.ListSystem(model.TaskSystemCatalog, 1, 10)
	if err != nil || legacy.Total != 0 {
		t.Fatalf("source task leaked: %+v %v", legacy, err)
	}
	defs, err := tasks.DefinitionsForSystem(nil, model.TaskSystemHongGuo)
	if err != nil || len(defs) != 5 {
		t.Fatalf("definitions=%+v err=%v", defs, err)
	}
	for _, d := range defs {
		if d.System != model.TaskSystemHongGuo {
			t.Fatal("wrong task ownership")
		}
	}
	if err := s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, "9000000000000000001"); err != ErrHongGuoDisabled || requests != 1 {
		t.Fatalf("disabled source requested: %d %v", requests, err)
	}
	work, err := repos.HongGuo.FindBySourceID(ctx, "9000000000000000001")
	if err != nil || work.Kind != model.MetadataKindMovie {
		t.Fatal("source disable removed data")
	}
}

func TestHongGuoArtworkLifecycle(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repos := repository.New(db)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	requests := 0
	images := NewImageProxy(&config.Config{}, zap.NewNop())
	images.client = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(encoded.Bytes())), Header: http.Header{"Content-Type": []string{"image/png"}}, Request: r}, nil
	})}
	s := NewHongGuoService(repos, NewTaskTrackerService(zap.NewNop(), nil), images, t.TempDir())
	work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "93001", Title: "图片测试", EpisodeCount: 1, CoverURL: "https://example.com/poster.png", Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoArtwork, ""); err != nil {
		t.Fatal(err)
	}
	var artwork model.HongGuoArtwork
	if err := db.First(&artwork, "work_id = ?", work.ID).Error; err != nil || artwork.LocalKey == "" || artwork.NextAttemptAt != nil {
		t.Fatalf("image not persisted: %+v %v", artwork, err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/image", nil)
		rec := httptest.NewRecorder()
		if err := s.ServeArtwork(ctx, rec, req, artwork.ID); err != nil || rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/") {
			t.Fatalf("image HTTP %s: %d %v", method, rec.Code, err)
		}
		if method == http.MethodHead && rec.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
	path, err := storedImagePath(s.imageRoot, artwork.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.ServeArtwork(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/image", nil), artwork.ID); !errors.Is(err, ErrArtworkNotFound) {
		t.Fatalf("missing image must enqueue repair: %v", err)
	}
	if err := s.Run(ctx, TaskKindHongGuoArtwork, ""); err != nil || requests != 2 {
		t.Fatalf("missing local image not repaired: requests=%d %v", requests, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("repaired file missing")
	}
}

func TestHongGuoArtworkRunsAlongsideCollection(t *testing.T) {
	for _, stop := range []string{"cancel", "disable", "shutdown"} {
		t.Run(stop, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			repos := repository.New(db)
			if _, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "94001", Title: "并行测试", EpisodeCount: 1, CoverURL: "https://example.com/poster.png", Snapshot: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{}, 3)
			client := &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				started <- struct{}{}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			images := NewImageProxy(&config.Config{}, zap.NewNop())
			images.client = client
			tasks := NewTaskTrackerService(zap.NewNop(), nil)
			s := NewHongGuoService(repos, tasks, images, t.TempDir())
			s.client = hongguo.NewClient(client)
			t.Cleanup(s.Wait)
			finished := make(chan error, 3)
			for _, kind := range []string{TaskKindHongGuoSync, TaskKindHongGuoRefresh, TaskKindHongGuoArtwork} {
				id := ""
				if kind == TaskKindHongGuoRefresh {
					id = "94001"
				}
				go func() { finished <- s.Run(ctx, kind, id) }()
				select {
				case <-started:
				case err := <-finished:
					t.Fatalf("task did not reach HTTP while other task was active: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("task did not start")
				}
			}
			for _, kind := range []string{TaskKindHongGuoSync, TaskKindHongGuoRefresh, TaskKindHongGuoArtwork} {
				if err := s.Run(ctx, kind, ""); !errors.Is(err, ErrHongGuoRunning) {
					t.Fatalf("duplicate %s accepted: %v", kind, err)
				}
			}
			switch stop {
			case "cancel":
				s.Cancel()
			case "disable":
				if err := s.SetEnabled(ctx, false); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				s.Wait()
			}
			for range 3 {
				select {
				case err := <-finished:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("task not canceled: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("task did not stop")
				}
			}
			page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 10)
			if err != nil || len(page.Items) != 3 {
				t.Fatalf("parallel task history: %+v %v", page, err)
			}
			for _, task := range page.Items {
				if task.Status != TaskStatusInterrupted {
					t.Fatalf("task not interrupted: %+v", task)
				}
			}
			if stop == "disable" {
				if err := s.SetEnabled(ctx, true); err != nil {
					t.Fatal(err)
				}
			}
			// 已取消的上下文无需联网，也能验证三组锁在结束后都已释放。
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			for _, kind := range []string{TaskKindHongGuoSync, TaskKindHongGuoRefresh, TaskKindHongGuoArtwork} {
				err := s.Run(canceled, kind, "")
				if stop == "shutdown" {
					if err == nil || err.Error() != "红果服务已关闭" {
						t.Fatalf("closed service accepted %s: %v", kind, err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatalf("task slot not reusable: %s %v", kind, err)
				}
			}
		})
	}
}

func TestTaskSystemLegacyMemoryCompatibility(t *testing.T) {
	for _, tc := range []struct{ kind, system string }{{TaskKindScan, model.TaskSystemCommon}, {TaskKindScrape, model.TaskSystemCatalog}, {TaskKindHongGuoRefresh, model.TaskSystemHongGuo}} {
		task := BackgroundTask{Kind: tc.kind}
		for _, system := range []string{model.TaskSystemCommon, model.TaskSystemCatalog, model.TaskSystemHongGuo} {
			if got := taskMatchesFilter(task, repository.TaskExecutionFilter{System: system}); got != (system == tc.system) {
				t.Fatalf("kind=%s system=%s got=%v", tc.kind, system, got)
			}
		}
	}
}

func TestHongGuoRefreshRetriesAndShutdown(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repos := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	s := NewHongGuoService(repos, tasks, nil, t.TempDir())
	for _, id := range []string{"91001", "91002"} {
		if _, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: id, Title: id, EpisodeCount: 1, Snapshot: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	requests := map[string]int{}
	if err := db.Model(&model.HongGuoWork{}).Where("source_id IN ?", []string{"91001", "91002"}).Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	fail := true
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		id := r.URL.Query().Get("series_id")
		requests[id]++
		if fail && id == "91001" {
			return nil, errors.New("temporary failure")
		}
		body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":%q,"series_name":"刷新测试","episode_cnt":1}}}}`, id)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err == nil {
		t.Fatal("failed work must fail the task while retaining later work")
	}
	if requests["91001"] != 1 || requests["91002"] != 1 {
		t.Fatalf("batch stopped or repeated: %v", requests)
	}
	var failure model.HongGuoSyncFailure
	if err := db.First(&failure, "source_id = ?", "91001").Error; err != nil || failure.Attempts != 1 {
		t.Fatalf("failure checkpoint: %+v %v", failure, err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil || requests["91001"] != 1 {
		t.Fatalf("cooldown ignored: %v %v", requests, err)
	}
	fail = false
	if err := db.Model(&failure).Update("retry_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil || requests["91001"] != 2 {
		t.Fatalf("due retry duplicated: %v %v", requests, err)
	}
	var failures int64
	if err := db.Model(&model.HongGuoSyncFailure{}).Count(&failures).Error; err != nil || failures != 0 {
		t.Fatalf("successful retry retained failure: %d %v", failures, err)
	}
	s.Wait()
	if err := s.Run(ctx, TaskKindHongGuoRefresh, "91001"); err == nil || requests["91001"] != 2 {
		t.Fatal("closed source accepted new work")
	}

	active := NewHongGuoService(repos, tasks, nil, t.TempDir())
	started, finished := make(chan struct{}), make(chan error, 1)
	active.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	go func() { finished <- active.Run(ctx, TaskKindHongGuoRefresh, "91003") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("source request did not start")
	}
	if err := active.Run(ctx, TaskKindHongGuoRefresh, ""); !errors.Is(err, ErrHongGuoRunning) {
		t.Fatalf("source tasks overlapped: %v", err)
	}
	active.Wait()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown did not cancel active request: %v", err)
	}
	page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != TaskStatusInterrupted {
		t.Fatalf("cancellation must display interrupted: %+v %v", page, err)
	}
	if err := db.Model(&model.HongGuoSyncFailure{}).Count(&failures).Error; err != nil || failures != 0 {
		t.Fatal("cancellation became a source failure")
	}
}

func TestHongGuoRefreshStopsAtCompletionAndReportsChanges(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repos := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	s := NewHongGuoService(repos, tasks, nil, t.TempDir())
	t.Cleanup(s.Wait)
	page := func(id, title, status string) string {
		return fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":%q,"series_name":%q,"episode_cnt":2,"episode_right_text":%q}}}}`, id, title, status)
	}
	for _, seed := range []struct{ id, status string }{{"91101", "全2集"}, {"91102", "更新至2集"}} {
		work, err := hongguo.ParseDetail([]byte(page(seed.id, "旧资料", seed.status)), seed.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repos.HongGuo.SaveDetail(ctx, work); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.HongGuoWork{}).Where("source_id IN ?", []string{"91101", "91102"}).Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.RecordSyncFailure(ctx, "91101", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.SaveDiscoveryPage(ctx, []hongguo.Work{{SourceID: "91103", Title: "新摘要"}}, model.HongGuoSyncState{Category: "real-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
	}
	requests := map[string]int{}
	finished := false
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		id := r.URL.Query().Get("series_id")
		requests[id]++
		title, status := "旧资料", "更新至2集"
		if id == "91102" && finished {
			title, status = "更新后资料", "全2集"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(page(id, title, status))), Header: make(http.Header), Request: r}, nil
	})})
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil {
		t.Fatal(err)
	}
	result, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 1)
	if err != nil || len(result.Items) != 1 || result.Items[0].Metrics["new"] != 1 || result.Items[0].Metrics["unchanged"] != 1 || requests["91101"] != 0 || requests["91102"] != 1 || requests["91103"] != 1 {
		t.Fatalf("first batch: %+v requests=%v err=%v", result, requests, err)
	}
	finished = true
	if err := db.Model(&model.HongGuoWork{}).Where("source_id = ?", "91102").Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil {
		t.Fatal(err)
	}
	result, err = tasks.ListSystem(model.TaskSystemHongGuo, 1, 1)
	if err != nil || len(result.Items) != 1 || result.Items[0].Metrics["updated"] != 1 || requests["91102"] != 2 {
		t.Fatalf("completion update: %+v requests=%v err=%v", result, requests, err)
	}
	if err := db.Model(&model.HongGuoWork{}).Where("source_id = ?", "91102").Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil || requests["91102"] != 2 || requests["91101"] != 0 {
		t.Fatalf("completed works refreshed again: %v %v", requests, err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, "91102"); err != nil || requests["91102"] != 3 {
		t.Fatalf("manual refresh of completed work rejected: %v %v", requests, err)
	}
}

func TestHongGuoNotFoundIsDeferredWithoutFailingBatch(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repos := repository.New(db)
	if err := repos.HongGuo.SaveDiscoveryPage(ctx, []hongguo.Work{{SourceID: "92001", Title: "待补齐"}, {SourceID: "92002", Title: "可补齐"}}, model.HongGuoSyncState{Category: "real-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
	}
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	s := NewHongGuoService(repos, tasks, nil, t.TempDir())
	notFound := true
	requests := map[string]int{}
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		id := r.URL.Query().Get("series_id")
		requests[id]++
		if id == "92001" && notFound {
			return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header), Request: r}, nil
		}
		body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":%q,"series_name":"补齐成功","episode_cnt":1}}}}`, id)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	retryStarted := time.Now()
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil {
		t.Fatalf("404-only deferred item failed batch: %v", err)
	}
	if requests["92001"] != 1 || requests["92002"] != 1 {
		t.Fatalf("404 blocked later work: %v", requests)
	}
	var failure model.HongGuoSyncFailure
	if err := db.First(&failure, "source_id = ?", "92001").Error; err != nil || failure.RetryAt.Before(retryStarted.Add(72*time.Hour-time.Minute)) || failure.RetryAt.After(time.Now().Add(72*time.Hour+time.Minute)) {
		t.Fatalf("404 retry is not about 72 hours later: %+v %v", failure, err)
	}
	if _, err := repos.HongGuo.FindBySourceID(ctx, "92001"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("404 created a fake work: %v", err)
	}
	page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != TaskStatusCompleted || page.Items[0].Metrics["deferred"] != 1 || page.Items[0].Metrics["failed"] != 0 {
		t.Fatalf("deferred task result: %+v %v", page, err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil || requests["92001"] != 1 {
		t.Fatalf("72-hour cooldown ignored: %v %v", requests, err)
	}
	notFound = false
	if err := db.Model(&failure).Update("retry_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil || requests["92001"] != 2 {
		t.Fatalf("due 404 retry did not recover: %v %v", requests, err)
	}
	var failures int64
	if err := db.Model(&model.HongGuoSyncFailure{}).Count(&failures).Error; err != nil || failures != 0 {
		t.Fatalf("successful retry retained failure: %d %v", failures, err)
	}
}
