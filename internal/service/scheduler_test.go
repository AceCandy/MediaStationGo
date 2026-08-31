package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
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

func TestSchedulerRegistersDisabledTMDbArtworkJobs(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler.Start(ctx)
	defer func() {
		cancel()
		scheduler.Stop()
	}()
	want := map[string]bool{"tmdb_artwork_local_repair": true, "tmdb_artwork_missing_recheck": true, "douban_artwork_local_repair": true, "tmdb_episode_metadata_recheck": true}
	for _, status := range scheduler.Status() {
		if status.Name == "metadata_artwork_backfill" {
			t.Fatal("retired metadata artwork backfill scheduler job still exists")
		}
		if !want[status.Name] {
			continue
		}
		if status.Enabled || status.IntervalSeconds != int64((24*time.Hour)/time.Second) {
			t.Fatalf("artwork job status = %#v", status)
		}
		delete(want, status.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing artwork maintenance scheduler jobs: %v", want)
	}
}

func TestSchedulerRegistersDisabledDoubanMovieEnrichment(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler.Start(ctx)
	defer func() {
		cancel()
		scheduler.Stop()
	}()
	for _, status := range scheduler.Status() {
		if status.Name == "douban_movie_enrichment" {
			if status.Enabled || status.IntervalSeconds != int64((24*time.Hour)/time.Second) {
				t.Fatalf("douban enrichment status = %#v", status)
			}
			return
		}
	}
	t.Fatal("douban movie enrichment scheduler job is missing")
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

func TestSchedulerUpdateSchedulePersistsAndResetsRuntime(t *testing.T) {
	db := newServiceTestDB(t, &model.Setting{})
	repos := repository.New(db)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, nil)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	scheduler.now = func() time.Time { return now }
	job := scheduler.configuredJob(t.Context(), "library_scan", "scan.periodic_enabled", "scan.interval_seconds", false, time.Hour, func(context.Context) error { return nil })
	scheduler.jobs = []*scheduledJob{job}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		scheduler.loopWithInitialDelay(ctx, job, time.Hour)
		close(done)
	}()
	if err := scheduler.UpdateSchedule(t.Context(), "library_scan", true, 120); err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for scheduler.Status()[0].NextRun.IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	status := scheduler.Status()[0]
	if !status.Enabled || status.IntervalSeconds != 120 || !status.NextRun.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("status after update = %#v", status)
	}
	for key, want := range map[string]string{"scan.periodic_enabled": "true", "scan.interval_seconds": "120"} {
		if got, err := repos.Setting.Get(t.Context(), key); err != nil || got != want {
			t.Fatalf("setting %s = %q, err = %v, want %q", key, got, err, want)
		}
	}
	cancel()
	<-done
}

func TestSchedulerUpdateScheduleRejectsInvalidConfigWithoutMutation(t *testing.T) {
	db := newServiceTestDB(t, &model.Setting{})
	repos := repository.New(db)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, nil)
	job := scheduler.configuredJob(t.Context(), "library_scan", "scan.periodic_enabled", "scan.interval_seconds", false, time.Hour, func(context.Context) error { return nil })
	scheduler.jobs = []*scheduledJob{job, {name: "fixed", interval: time.Hour, run: func(context.Context) error { return nil }}}

	for _, tt := range []struct {
		name    string
		seconds int64
		wantErr error
	}{
		{name: "missing", seconds: 60, wantErr: ErrSchedulerJobNotFound},
		{name: "fixed", seconds: 60, wantErr: ErrSchedulerConfigUnsupported},
		{name: "library_scan", seconds: 59, wantErr: ErrSchedulerIntervalInvalid},
		{name: "library_scan", seconds: int64(schedulerMaxInterval/time.Second) + 1, wantErr: ErrSchedulerIntervalInvalid},
		{name: "library_scan", seconds: int64(^uint64(0) >> 1), wantErr: ErrSchedulerIntervalInvalid},
	} {
		if err := scheduler.UpdateSchedule(t.Context(), tt.name, true, tt.seconds); !errors.Is(err, tt.wantErr) {
			t.Fatalf("update %s error = %v, want %v", tt.name, err, tt.wantErr)
		}
	}
	status := scheduler.Status()[0]
	if status.Enabled || status.IntervalSeconds != int64(time.Hour/time.Second) {
		t.Fatalf("invalid update mutated status: %#v", status)
	}
	if got, err := repos.Setting.Get(t.Context(), "scan.periodic_enabled"); err != nil || got != "" {
		t.Fatalf("invalid update persisted setting = %q, err = %v", got, err)
	}
}

func TestSchedulerConfiguredJobIgnoresOverflowingStoredInterval(t *testing.T) {
	db := newServiceTestDB(t, &model.Setting{})
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "scan.interval_seconds", "9223372036854775807"); err != nil {
		t.Fatal(err)
	}
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, nil)
	job := scheduler.configuredJob(t.Context(), "library_scan", "scan.periodic_enabled", "scan.interval_seconds", false, 24*time.Hour, func(context.Context) error { return nil })
	if job.interval != 24*time.Hour {
		t.Fatalf("interval = %v, want fallback 24h", job.interval)
	}
}

func TestSchedulerManualRunBypassesDisabledSchedule(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	var runs atomic.Int32
	scheduler.jobs = []*scheduledJob{{
		name: "disabled", interval: time.Hour, enabled: false, configurable: true,
		run: func(context.Context) error { runs.Add(1); return nil },
	}}
	if err := scheduler.RunNow(t.Context(), "disabled"); err != nil {
		t.Fatalf("manual run: %v", err)
	}
	if runs.Load() != 1 {
		t.Fatalf("manual runs = %d, want 1", runs.Load())
	}
}

func TestSchedulerTaskTrigger(t *testing.T) {
	if got := schedulerTaskTrigger(t.Context()); got != TaskTriggerScheduled {
		t.Fatalf("default trigger = %q, want %q", got, TaskTriggerScheduled)
	}
	ctx := context.WithValue(t.Context(), schedulerManualRunKey{}, true)
	if got := schedulerTaskTrigger(ctx); got != TaskTriggerManual {
		t.Fatalf("manual trigger = %q, want %q", got, TaskTriggerManual)
	}
}
