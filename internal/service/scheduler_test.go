package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSchedulerRunNowAsyncSurvivesCallerCancellation(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	scheduler.jobs = []*scheduledJob{{
		name:     "organize_source",
		interval: time.Minute,
		run: func(ctx context.Context) error {
			close(started)
			defer close(finished)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		},
	}}

	ctx, cancel := context.WithCancel(t.Context())
	if err := scheduler.RunNowAsync(ctx, "organize_source"); err != nil {
		t.Fatalf("run now async: %v", err)
	}
	<-started
	cancel()
	select {
	case <-finished:
		t.Fatal("manual scheduled job was canceled with the HTTP caller context")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("manual scheduled job did not finish after release")
	}
	var status []JobStatus
	deadline := time.Now().Add(time.Second)
	for {
		status = scheduler.Status()
		if len(status) == 1 && !status[0].Running {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(status) != 1 || status[0].Running || status[0].LastErr != "" {
		t.Fatalf("unexpected status after async run: %+v", status)
	}
}

func TestSchedulerRunNowAsyncRejectsDuplicateRun(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	scheduler.jobs = []*scheduledJob{{
		name:     "organize_source",
		interval: time.Minute,
		run: func(ctx context.Context) error {
			close(started)
			<-release
			return nil
		},
	}}

	if err := scheduler.RunNowAsync(t.Context(), "organize_source"); err != nil {
		t.Fatalf("first run now async: %v", err)
	}
	<-started
	if err := scheduler.RunNowAsync(t.Context(), "organize_source"); !errors.Is(err, ErrSchedulerJobAlreadyRunning) {
		t.Fatalf("duplicate run error = %v, want %v", err, ErrSchedulerJobAlreadyRunning)
	}
	close(release)
}

func TestSchedulerLoopWaitsIntervalAfterSlowRun(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var runs atomic.Int32
	job := &scheduledJob{
		name:     "slow",
		interval: 25 * time.Millisecond,
		run: func(ctx context.Context) error {
			runs.Add(1)
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		scheduler.loopWithInitialDelay(ctx, job, time.Millisecond)
		close(done)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("scheduler loop did not stop")
	}
	if got := runs.Load(); got > 2 {
		t.Fatalf("slow job ran %d times; scheduler should not catch up missed ticks", got)
	}
}

func TestSchedulerStatusIncludesNextRun(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	scheduler.now = func() time.Time { return now }
	job := &scheduledJob{name: "scheduled", interval: time.Hour, run: func(context.Context) error { return nil }}
	scheduler.jobs = []*scheduledJob{job}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		scheduler.loopWithInitialDelay(ctx, job, time.Hour)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for scheduler.Status()[0].NextRun.IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	status := scheduler.Status()[0]
	if want := now.Add(time.Hour); !status.NextRun.Equal(want) {
		t.Fatalf("next run = %v, want %v", status.NextRun, want)
	}
	cancel()
	<-done
}

func TestSchedulerStatusClearsNextRunWhileScheduledJobRuns(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	job := &scheduledJob{name: "scheduled", interval: time.Hour, run: func(context.Context) error {
		close(started)
		<-release
		return nil
	}}
	scheduler.jobs = []*scheduledJob{job}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		scheduler.loopWithInitialDelay(ctx, job, time.Millisecond)
		close(done)
	}()

	<-started
	status := scheduler.Status()[0]
	if !status.Running || !status.NextRun.IsZero() {
		t.Fatalf("status while running = %#v", status)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for scheduler.Status()[0].NextRun.IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	status = scheduler.Status()[0]
	if status.Running || status.NextRun.IsZero() {
		t.Fatalf("status after run = %#v", status)
	}
	cancel()
	<-done
}
