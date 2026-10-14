package service

import (
	"errors"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHuangGuoAIManualRankSelectionAndTimestamp(t *testing.T) {
	db := newServiceTestDB(t, append(model.HuangGuoAIModels(), &model.Setting{})...)
	repo := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	s := NewHuangGuoAIService(repo, tasks, nil, t.TempDir())
	t.Cleanup(s.Wait)
	calls := 0
	fail := false
	s.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/ranks/hot/" {
			t.Errorf("unexpected rank request %s", r.URL.Path)
		}
		if fail {
			return nil, errors.New("unavailable")
		}
		body := `<div class="hg-rank-item" data-rank-item data-track-id="22" data-track-title="测试作品" data-track-type-name="漫剧"><a href="/video/22/">测试作品</a></div>`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	if err := s.RefreshRank(t.Context(), "hot"); err != nil {
		t.Fatal(err)
	}
	before, err := repo.HuangGuoAI.RankUpdatedAt(t.Context(), "hot")
	if err != nil || before == nil || calls != 1 {
		t.Fatal("missing successful timestamp", before, calls, err)
	}
	if at, err := repo.HuangGuoAI.RankUpdatedAt(t.Context(), "recommend"); err != nil || at != nil {
		t.Fatal("other ranking changed", at, err)
	}
	fail = true
	if err := s.RefreshRank(t.Context(), "hot"); err == nil {
		t.Fatal("failure accepted")
	}
	after, err := repo.HuangGuoAI.RankUpdatedAt(t.Context(), "hot")
	if err != nil || after == nil || !after.Equal(*before) {
		t.Fatal("failure changed timestamp", after, err)
	}
	s.running[TaskKindHuangGuoAIRank] = func() {}
	if err := s.RefreshRank(t.Context(), "hot"); !errors.Is(err, ErrHuangGuoAIRunning) {
		t.Fatal("manual refresh bypassed shared exclusion", err)
	}
	delete(s.running, TaskKindHuangGuoAIRank)
	if err := s.RefreshRank(t.Context(), "unknown"); err == nil {
		t.Fatal("invalid ranking accepted")
	}
	history, err := tasks.DefinitionHistory(TaskKindHuangGuoAIRank, 1, 10)
	if err != nil || history.Total != 2 || history.Items[0].Trigger != TaskTriggerManual {
		t.Fatal("manual task history missing", history, err)
	}
}
