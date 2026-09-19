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
	if err = s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err != nil || len(requests) != 3 {
		t.Fatalf("cooldown/empty checkpoint: %v %v", requests, err)
	}
	fail = false
	if err = repos.HongGuo.RetryAlbum(t.Context(), "91002", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.Run(t.Context(), TaskKindHongGuoAlbum, ""); err != nil || len(requests) != 4 {
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
