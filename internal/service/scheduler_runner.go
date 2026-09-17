package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// JobStatus is a snapshot suitable for the admin UI.
type JobStatus struct {
	Count              int       `json:"count,omitempty"`
	Name               string    `json:"name"`
	Interval           string    `json:"interval"`
	LastRun            time.Time `json:"last_run,omitempty"`
	NextRun            time.Time `json:"next_run,omitempty"`
	LastErr            string    `json:"last_err,omitempty"`
	Running            bool      `json:"running,omitempty"`
	Started            time.Time `json:"started_at,omitempty"`
	Enabled            bool      `json:"enabled"`
	Configurable       bool      `json:"configurable,omitempty"`
	IntervalSeconds    int64     `json:"interval_seconds"`
	MinIntervalSeconds int64     `json:"min_interval_seconds,omitempty"`
	MaxIntervalSeconds int64     `json:"max_interval_seconds,omitempty"`
}

// Status returns the current state of every registered job.
func (s *SchedulerService) Status() []JobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobStatus, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, JobStatus{
			Count:              j.count,
			Name:               j.name,
			Interval:           j.interval.String(),
			LastRun:            j.lastRun,
			NextRun:            j.nextRun,
			LastErr:            j.lastErr,
			Running:            j.running,
			Started:            j.started,
			Enabled:            !j.configurable || j.enabled,
			Configurable:       j.configurable,
			IntervalSeconds:    int64(j.interval / time.Second),
			MinIntervalSeconds: int64(j.minInterval / time.Second),
			MaxIntervalSeconds: int64(j.maxInterval / time.Second),
		})
	}
	return out
}

// UpdateSchedule persists and applies one allowlisted periodic job configuration.
func (s *SchedulerService) UpdateSchedule(ctx context.Context, name string, enabled bool, intervalSeconds int64, counts ...int) error {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	s.mu.Lock()
	j := s.jobByNameLocked(name)
	if j == nil {
		s.mu.Unlock()
		return ErrSchedulerJobNotFound
	}
	if !j.configurable {
		s.mu.Unlock()
		return ErrSchedulerConfigUnsupported
	}
	if intervalSeconds < int64(j.minInterval/time.Second) || intervalSeconds > int64(j.maxInterval/time.Second) {
		s.mu.Unlock()
		return ErrSchedulerIntervalInvalid
	}
	interval := time.Duration(intervalSeconds) * time.Second
	count := j.count
	if len(counts) > 0 {
		if name != TaskKindHongGuoSupplement || len(counts) != 1 || counts[0] < 1 || counts[0] > 100 {
			s.mu.Unlock()
			return ErrSchedulerCountInvalid
		}
		count = counts[0]
	}
	enabledKey, intervalKey := j.enabledKey, j.intervalKey
	s.mu.Unlock()

	if s.repo == nil || s.repo.DB == nil {
		return errors.New("settings repository unavailable")
	}
	now := time.Now()
	if err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]string{
			enabledKey:  strconv.FormatBool(enabled),
			intervalKey: strconv.FormatInt(intervalSeconds, 10),
		}
		if name == TaskKindHongGuoSupplement {
			values[hongGuoSupplementCountKey] = strconv.Itoa(count)
		}
		for key, value := range values {
			if err := tx.Save(&model.Setting{Key: key, Value: value, UpdatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	s.mu.Lock()
	j = s.jobByNameLocked(name)
	if j != nil {
		j.enabled = enabled
		j.interval = interval
		j.count = count
		j.configVersion++
		select {
		case j.reset <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	return nil
}

// RunNow triggers a single run of the named job synchronously.
func (s *SchedulerService) RunNow(ctx context.Context, name string) error {
	j := s.jobByName(name)
	if j == nil {
		return ErrSchedulerJobNotFound
	}
	return s.runOnce(context.WithValue(ctx, schedulerManualRunKey{}, true), j)
}

// RunNowAsync triggers a named job in the background and returns immediately.
// The job is detached from the HTTP request cancellation so a browser timeout,
// route change, or reverse-proxy disconnect cannot kill long organize/scan work.
func (s *SchedulerService) RunNowAsync(ctx context.Context, name string) error {
	return s.runNowAsync(ctx, name)
}

func (s *SchedulerService) RunLibraryScanNowAsync(ctx context.Context, libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return errors.New("library id required")
	}
	return s.runNowAsync(context.WithValue(ctx, schedulerLibraryScanIDKey{}, libraryID), "library_scan")
}

func (s *SchedulerService) runNowAsync(ctx context.Context, name string) error {
	j := s.jobByName(name)
	if j == nil {
		return ErrSchedulerJobNotFound
	}
	runCtx := context.Background()
	if ctx != nil {
		runCtx = context.WithoutCancel(ctx)
	}
	runCtx = context.WithValue(runCtx, schedulerManualRunKey{}, true)
	if err := s.beginRun(j); err != nil {
		return err
	}
	go func() {
		if err := s.runReserved(runCtx, j); err != nil && s.log != nil {
			s.log.Warn("manual scheduled job failed", zap.String("name", name), zap.Error(err))
		}
	}()
	return nil
}

func (s *SchedulerService) loop(ctx context.Context, j *scheduledJob) {
	s.loopWithInitialDelay(ctx, j, 15*time.Second)
}

func (s *SchedulerService) loopWithInitialDelay(ctx context.Context, j *scheduledJob, initialDelay time.Duration) {
	first := true
	for {
		s.mu.Lock()
		enabled := !j.configurable || j.enabled
		delay := j.interval
		if first {
			delay = initialDelay
			first = false
		}
		version := j.configVersion
		if delay < 0 {
			delay = 0
		}
		if enabled {
			j.nextRun = s.currentTime().Add(delay)
		} else {
			j.nextRun = time.Time{}
		}
		s.mu.Unlock()
		var timer *time.Timer
		var timerC <-chan time.Time
		if enabled {
			timer = time.NewTimer(delay)
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			stopSchedulerTimer(timer)
			return
		case <-s.stopCh:
			stopSchedulerTimer(timer)
			return
		case <-j.reset:
			stopSchedulerTimer(timer)
			continue
		case <-timerC:
		}
		s.mu.Lock()
		j.nextRun = time.Time{}
		stale := j.configVersion != version || (j.configurable && !j.enabled)
		s.mu.Unlock()
		if stale {
			continue
		}
		if err := s.runOnce(ctx, j); err != nil {
			if errors.Is(err, ErrSchedulerJobAlreadyRunning) {
				s.log.Debug("scheduled job skipped; previous run still active", zap.String("name", j.name))
				continue
			}
			s.log.Warn("scheduled job failed",
				zap.String("name", j.name), zap.Error(err))
		}
	}
}

func stopSchedulerTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (s *SchedulerService) runOnce(ctx context.Context, j *scheduledJob) error {
	if err := s.beginRun(j); err != nil {
		return err
	}
	return s.runReserved(ctx, j)
}

func (s *SchedulerService) jobByName(name string) *scheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobByNameLocked(name)
}

func (s *SchedulerService) jobByNameLocked(name string) *scheduledJob {
	for _, j := range s.jobs {
		if j.name == name {
			return j
		}
	}
	return nil
}

func (s *SchedulerService) beginRun(j *scheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.name == TaskKindHongGuoSupplement {
		select {
		case <-s.stopCh:
			return ErrSchedulerJobNotFound
		default:
		}
	}
	if j.running {
		return ErrSchedulerJobAlreadyRunning
	}
	j.running = true
	j.started = s.currentTime()
	if j.name == TaskKindHongGuoSupplement {
		s.supplementWG.Add(1)
	}
	return nil
}

func (s *SchedulerService) runReserved(ctx context.Context, j *scheduledJob) error {
	if j.name == TaskKindHongGuoSupplement {
		defer s.supplementWG.Done()
	}
	err := j.run(ctx)
	s.mu.Lock()
	j.lastRun = s.currentTime()
	if err != nil {
		j.lastErr = err.Error()
	} else {
		j.lastErr = ""
	}
	j.running = false
	j.started = time.Time{}
	lastErr := j.lastErr
	s.mu.Unlock()
	if s.hub != nil {
		s.hub.Publish("scheduler", map[string]any{
			"name":  j.name,
			"ok":    err == nil,
			"error": lastErr,
		})
	}
	return err
}

func (s *SchedulerService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}
