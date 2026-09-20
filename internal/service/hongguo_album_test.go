package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type hongGuoAlbumTransport func(*http.Request) (*http.Response, error)

func (f hongGuoAlbumTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHongGuoAlbumFailureAndResume(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	s := NewHongGuoService(repos, NewTaskTrackerService(zap.NewNop(), nil), nil, t.TempDir())
	t.Cleanup(s.Wait)
	for _, id := range []string{"91001", "91002", "91003"} {
		if _, err = repos.HongGuo.SaveDetail(t.Context(), hongguo.Work{SourceID: id, Title: id, EpisodeCount: 2, Snapshot: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err = repos.HongGuo.SaveAlbum(t.Context(), "91002", hongguo.Album{ID: "99999", Season: 2}); err != nil {
		t.Fatal(err)
	}
	if err = repos.HongGuo.RetryAlbum(t.Context(), "91002", time.Now()); err != nil {
		t.Fatal(err)
	}
	requests := []string{}
	fail := true
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoAlbumTransport(func(r *http.Request) (*http.Response, error) {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return nil, err
		}
		id := input["series_id"]
		requests = append(requests, id)
		if id == "91002" && fail {
			return nil, errors.New("upstream failed")
		}
		body := fmt.Sprintf(`{"code":0,"data":{"video_data":{"series_id_str":%q}}}`, id)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err = s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err == nil {
		t.Fatal("failure hidden")
	}
	w, err := repos.HongGuo.FindBySourceID(t.Context(), "91002")
	if err != nil || w.RelatedAlbumID != "99999" || w.SeasonIndex != 2 || w.AlbumRetryAt == nil {
		t.Fatalf("lost prior relation: %+v %v", w, err)
	}
	if len(requests) != 3 {
		t.Fatalf("failure stopped batch: %v", requests)
	}
	if err = s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err == nil || len(requests) != 4 || requests[3] != "91002" {
		t.Fatalf("next pass must retry only the failed work once: %v %v", requests, err)
	}
	fail = false
	if err = s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err != nil || len(requests) != 5 {
		t.Fatalf("resume: %v %v", requests, err)
	}
	w, err = repos.HongGuo.FindBySourceID(t.Context(), "91002")
	if err != nil || w.RelatedAlbumID != "" || w.SeasonIndex != 0 || w.AlbumCheckedAt == nil || w.AlbumRetryAt != nil {
		t.Fatalf("empty success: %+v %v", w, err)
	}

	if err = repos.HongGuo.RetryAlbum(t.Context(), "91003", time.Now()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoAlbumTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, TaskKindHongGuoAlbum, "") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request not started")
	}
	if err = s.Run(t.Context(), TaskKindHongGuoRefresh, "91003"); !errors.Is(err, ErrHongGuoRunning) {
		t.Fatalf("overlap allowed: %v", err)
	}
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel blocked")
	}
	pending, err := repos.HongGuo.PendingAlbums(t.Context(), "", time.Now())
	if err != nil || len(pending) != 1 || pending[0].SourceID != "91003" {
		t.Fatalf("cancel lost pending row: %+v %v", pending, err)
	}
}

func TestHongGuoRefreshDefersAlbumFailure(t *testing.T) {
	for _, sourceID := range []string{"91001", ""} {
		t.Run("source="+sourceID, func(t *testing.T) {
			db := newServiceTestDB(t, model.AllModels()...)
			repos := repository.New(db)
			tasks := NewTaskTrackerService(zap.NewNop(), nil)
			tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
			s := NewHongGuoService(repos, tasks, nil, t.TempDir())
			t.Cleanup(s.Wait)
			for _, id := range []string{"91001", "91002"} {
				if _, err := repos.HongGuo.SaveDetail(t.Context(), hongguo.Work{SourceID: id, Title: id, EpisodeCount: 2, Snapshot: []byte(`{}`)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := repos.HongGuo.SaveAlbum(t.Context(), "91001", hongguo.Album{ID: "99999", Season: 2}); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.HongGuoWork{}).Where("source_id = ?", "91001").Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			requests := map[string]int{}
			fail := true
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoAlbumTransport(func(r *http.Request) (*http.Response, error) {
				body := `_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"91001","series_name":"刷新成功","episode_cnt":2}}}}`
				if r.Method == http.MethodPost {
					var input map[string]string
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						return nil, err
					}
					id := input["series_id"]
					requests[id]++
					body = fmt.Sprintf(`{"code":0,"data":{"video_data":{"series_id_str":%q}}}`, id)
					if id == "91001" && fail {
						body = `{"code":429,"message":"private upstream context"}`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			before := time.Now()
			if err := s.Run(t.Context(), TaskKindHongGuoRefresh, sourceID); err != nil {
				t.Fatalf("album query failed the detail task: %v", err)
			}
			page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 10)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("tasks=%+v err=%v", page, err)
			}
			result := page.Items[0]
			if result.Status != TaskStatusCompleted || result.Error != "" || result.Metrics["failed"] != 0 || result.Metrics["succeeded"] != 1 || result.Metrics["album_warnings"] != 1 {
				t.Fatalf("unexpected detail outcome: %+v", result)
			}
			log, err := tasks.ReadDefinitionLog(TaskKindHongGuoRefresh, "", 0)
			if err != nil || !strings.Contains(log.Content, "code=429") || !strings.Contains(log.Content, "补充任务重试") || strings.Contains(log.Content, "private upstream context") {
				t.Fatalf("missing safe warning: %+v %v", log, err)
			}
			w, err := repos.HongGuo.FindBySourceID(t.Context(), "91001")
			if err != nil || w.Title != "刷新成功" || w.RelatedAlbumID != "99999" || w.SeasonIndex != 2 || w.AlbumRetryAt == nil || w.AlbumRetryAt.Before(before) || w.AlbumRetryAt.After(time.Now()) {
				t.Fatalf("lost detail/relation/retry: %+v %v", w, err)
			}
			var failures int64
			if err := db.Model(&model.HongGuoSyncFailure{}).Count(&failures).Error; err != nil || failures != 0 {
				t.Fatalf("album failure entered detail retries: %d %v", failures, err)
			}
			if requests["91001"] != 1 || requests["91002"] != 0 {
				t.Fatalf("detail refresh consumed historical albums: %v", requests)
			}
			if err := s.Run(t.Context(), TaskKindHongGuoRefresh, ""); err != nil || requests["91001"] != 1 || requests["91002"] != 0 {
				t.Fatalf("idle refresh retried albums: %v %v", requests, err)
			}
			if err := s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err == nil || requests["91001"] != 2 || requests["91002"] != 1 {
				t.Fatalf("supplement must attempt failed and unprocessed works once: %v %v", requests, err)
			}
			fail = false
			if err := s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err != nil || requests["91001"] != 3 || requests["91002"] != 1 {
				t.Fatalf("supplement did not resume: %v %v", requests, err)
			}
			w, err = repos.HongGuo.FindBySourceID(t.Context(), "91001")
			if err != nil || w.AlbumRetryAt != nil || w.RelatedAlbumID != "" || w.SeasonIndex != 0 || w.AlbumCheckedAt == nil {
				t.Fatalf("supplement result not saved: %+v %v", w, err)
			}
		})
	}
}

func TestHongGuoRefreshDoesNotIgnoreAlbumCheckpointOrCancellation(t *testing.T) {
	for _, mode := range []string{"save", "retry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db := newServiceTestDB(t, model.AllModels()...)
			s := NewHongGuoService(repository.New(db), NewTaskTrackerService(zap.NewNop(), nil), nil, t.TempDir())
			t.Cleanup(s.Wait)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			albumRequested := false
			if err := db.Callback().Update().Before("gorm:update").Register("test:album_save", func(tx *gorm.DB) {
				if updates, ok := tx.Statement.Dest.(map[string]any); ok && albumRequested {
					if (mode == "save" && updates["album_checked_at"] != nil) || (mode == "retry" && updates["album_retry_at"] != nil) {
						tx.AddError(errors.New("album checkpoint failed"))
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoAlbumTransport(func(r *http.Request) (*http.Response, error) {
				body := `_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"91001","series_name":"已保存资料","episode_cnt":2}}}}`
				if r.Method == http.MethodPost {
					albumRequested = true
					if mode == "cancel" {
						cancel()
						return nil, ctx.Err()
					}
					body = `{"code":0,"data":{"video_data":{"series_id_str":"91001"}}}`
					if mode == "retry" {
						body = `{"code":429}`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			wantErr, wantStatus := errHongGuoCheckpoint, TaskStatusFailed
			if mode == "cancel" {
				wantErr, wantStatus = context.Canceled, TaskStatusInterrupted
			}
			if err := s.Run(ctx, TaskKindHongGuoRefresh, "91001"); !errors.Is(err, wantErr) {
				t.Fatalf("error=%v want %v", err, wantErr)
			}
			result := s.tasks.Snapshot().Recent[0]
			if result.Status != wantStatus || result.Metrics["album_warnings"] != 0 {
				t.Fatalf("critical error hidden: %+v", result)
			}
		})
	}
}
