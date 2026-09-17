package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoSupplementScheduleAndManual(t *testing.T) {
	downloads := newDownloadTestService(t)
	for i := 1; i <= 3; i++ {
		work := model.HongGuoWork{SourceID: fmt.Sprint(i), Title: "定时测试", Kind: "series"}
		if err := downloads.repo.DB.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
		if err := downloads.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 1, SourceVideoID: "123"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	newScheduler := func() *SchedulerService {
		s := NewSchedulerService(zap.NewNop(), downloads.repo, nil, nil, nil)
		s.SetTaskTracker(downloads.tasks)
		s.SetHongGuoDownloads(downloads)
		s.Start(t.Context())
		t.Cleanup(s.Stop)
		return s
	}
	s := newScheduler()
	j := s.jobByName(TaskKindHongGuoSupplement)
	if j == nil || j.enabled || j.count != 10 || j.interval != 24*time.Hour {
		t.Fatalf("defaults=%+v", j)
	}
	for _, count := range []int{0, 101} {
		if err := s.UpdateSchedule(t.Context(), j.name, true, 60, count); !errors.Is(err, ErrSchedulerCountInvalid) {
			t.Fatal(err)
		}
	}
	if err := s.UpdateSchedule(t.Context(), j.name, false, 3600, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(t.Context(), j.name, true, 1, 3); !errors.Is(err, ErrSchedulerIntervalInvalid) {
		t.Fatal(err)
	}
	s.mu.Lock()
	count, enabled, interval := j.count, j.enabled, j.interval
	s.mu.Unlock()
	if count != 2 || enabled || interval != time.Hour {
		t.Fatal("invalid update changed schedule")
	}
	// 手动执行在关闭定时时仍可运行，且同一定时运行槽不能重复领取。
	entered, release := make(chan struct{}), make(chan struct{})
	j.run = func(ctx context.Context) error { close(entered); <-release; return s.jobHongGuoSupplement(ctx) }
	if err := s.RunHongGuoSupplementNowAsync(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := s.runOnce(t.Context(), j); !errors.Is(err, ErrSchedulerJobAlreadyRunning) {
		t.Fatal(err)
	}
	if err := s.RunHongGuoSupplementNowAsync(t.Context(), 1); !errors.Is(err, ErrSchedulerJobAlreadyRunning) {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		running := j.running
		s.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("manual run did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	history, err := downloads.tasks.DefinitionHistory(j.name, 1, 20)
	if err != nil || history.Total != 1 || history.Items[0].Trigger != TaskTriggerManual || history.Items[0].Metrics["works"] != 1 {
		t.Fatalf("manual=%+v %v", history, err)
	}
	s.Stop()
	s = newScheduler()
	j = s.jobByName(TaskKindHongGuoSupplement)
	if j.count != 2 || j.enabled || j.interval != time.Hour {
		t.Fatalf("restored=%+v", j)
	}
	if err := s.UpdateSchedule(t.Context(), j.name, true, 3600, 2); err != nil {
		t.Fatal(err)
	}
	// 仅缩短测试的首次等待；仍通过真实定时循环触发，而不是直接调用业务方法。
	timerCtx, cancelTimer := context.WithCancel(t.Context())
	timerDone := make(chan struct{})
	go func() { s.loopWithInitialDelay(timerCtx, j, 0); close(timerDone) }()
	deadline = time.Now().Add(5 * time.Second)
	for {
		history, err = downloads.tasks.DefinitionHistory(j.name, 1, 20)
		if err == nil && history.Total == 2 && history.Items[0].Status == TaskStatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			cancelTimer()
			<-timerDone
			t.Fatal("timer did not finish", err)
		}
		time.Sleep(time.Millisecond)
	}
	cancelTimer()
	<-timerDone
	history, err = downloads.tasks.DefinitionHistory(j.name, 1, 20)
	if err != nil || history.Total != 2 || history.Items[0].Trigger != TaskTriggerScheduled || history.Items[0].Metrics["works"] != 2 || history.Items[0].Metrics["requested"] != 2 {
		t.Fatalf("scheduled=%+v %v", history, err)
	}
	if err := s.runOnce(t.Context(), j); err != nil {
		t.Fatal(err)
	}
	history, _ = downloads.tasks.DefinitionHistory(j.name, 1, 20)
	if history.Items[0].Metrics["works"] != 0 {
		t.Fatal("repeated downloads")
	}
	if err := downloads.repo.Setting.Set(t.Context(), hongGuoDownloadRootKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.runOnce(t.Context(), j); err == nil {
		t.Fatal("missing root accepted")
	}
	history, _ = downloads.tasks.DefinitionHistory(j.name, 1, 20)
	if history.Items[0].Status != TaskStatusFailed {
		t.Fatal("missing root not recorded")
	}
	if err := downloads.repo.Setting.Set(t.Context(), "hongguo.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if err := s.runOnce(t.Context(), j); !errors.Is(err, ErrHongGuoDisabled) {
		t.Fatal(err)
	}
}

func TestHongGuoSupplementStopCancelsAndJoins(t *testing.T) {
	downloads := newDownloadTestService(t)
	s := NewSchedulerService(zap.NewNop(), downloads.repo, nil, nil, nil)
	s.SetTaskTracker(downloads.tasks)
	s.SetHongGuoDownloads(downloads)
	s.Start(t.Context())
	t.Cleanup(s.Stop)
	entered := make(chan struct{})
	if err := downloads.repo.DB.Callback().Query().Before("gorm:query").Register("test:supplement-block", func(tx *gorm.DB) {
		if tx.Statement.Table == "settings" {
			close(entered)
			<-tx.Statement.Context.Done()
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunHongGuoSupplementNowAsync(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("not started")
	}
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not join")
	}
	if err := s.RunHongGuoSupplementNowAsync(t.Context(), 1); !errors.Is(err, ErrSchedulerJobNotFound) {
		t.Fatal(err)
	}
}
