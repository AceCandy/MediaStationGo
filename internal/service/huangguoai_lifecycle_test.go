package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestHuangGuoAIRefreshWakeAndShutdown(t *testing.T) {
	db := newServiceTestDB(t, append(model.HuangGuoAIModels(), &model.Setting{})...)
	repos := repository.New(db)
	if err := repos.HuangGuoAI.RegisterSummaries(t.Context(), []huangguoai.Summary{{SourceID: "71", Title: "Synthetic", Category: "ai-duanju"}}); err != nil {
		t.Fatal(err)
	}
	s := NewHuangGuoAIService(repos, NewTaskTrackerService(nil, nil), nil, t.TempDir())
	defer s.Wait()
	started := make(chan struct{}, 1)
	s.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})})
	s.requestRefresh(t.Context())
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	s.requestRefresh(t.Context())
	s.mu.Lock()
	pending := s.refreshRequested
	s.mu.Unlock()
	if !pending {
		t.Fatal("wake during refresh was lost")
	}
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel active refresh")
	}
	s.requestRefresh(context.Background())
	if err := s.Run(t.Context(), TaskKindHuangGuoAIRefresh, "71"); err != context.Canceled {
		t.Fatal("closed source restarted", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshRequested || len(s.running) != 0 || s.autoRefreshCancel != nil {
		t.Fatal("shutdown retained active work")
	}
}
