// Package service — periodic scheduled jobs.
//
// SchedulerService owns the configurable periodic timers shown in the task center.
//
// Each job runs at most once at a time (an in-flight run blocks the
// next tick). All work happens on a long-lived background context so
// the operator can keep clicking around the UI while the watchdog runs.
package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SchedulerService runs the periodic jobs.
type SchedulerService struct {
	log              *zap.Logger
	repo             *repository.Container
	scanner          *ScannerService
	organizer        *OrganizerService
	organizePipeline *OrganizePipelineService
	scraper          *ScraperService
	device           *DeviceService
	hub              *Hub
	tasks            *TaskTrackerService
	hongguo          *HongGuoService
	hongguoDownloads *HongGuoDownloadService
	supplementCtx    context.Context
	supplementCancel context.CancelFunc
	supplementWG     sync.WaitGroup
	now              func() time.Time

	mu         sync.Mutex
	scheduleMu sync.Mutex
	stopCh     chan struct{}
	jobs       []*scheduledJob
}

var (
	ErrSchedulerJobNotFound       = errors.New("scheduled job not found")
	ErrSchedulerJobAlreadyRunning = errors.New("scheduled job already running")
	ErrSchedulerConfigUnsupported = errors.New("scheduled job is not configurable")
	ErrSchedulerIntervalInvalid   = errors.New("scheduled job interval is invalid")
	ErrSchedulerCountInvalid      = errors.New("每轮补充数量须为 1–100 部")
)

func (s *SchedulerService) SetTaskTracker(tasks *TaskTrackerService) {
	s.tasks = tasks
}

// SetHongGuoDownloads 在启动调度前注入补充下载服务。
func (s *SchedulerService) SetHongGuoDownloads(downloads *HongGuoDownloadService) {
	s.hongguoDownloads = downloads
}

func (s *SchedulerService) SetOrganizePipeline(pipeline *OrganizePipelineService) {
	s.organizePipeline = pipeline
}

func (s *SchedulerService) SetPeriodicWorkers(scraper *ScraperService, device *DeviceService) {
	s.scraper = scraper
	s.device = device
}

// scheduledJob is one recurring task.
type scheduledJob struct {
	name          string
	interval      time.Duration
	run           func(ctx context.Context) error
	enabled       bool
	configurable  bool
	enabledKey    string
	intervalKey   string
	minInterval   time.Duration
	maxInterval   time.Duration
	reset         chan struct{}
	configVersion uint64
	count         int
	lastRun       time.Time
	lastErr       string
	running       bool
	started       time.Time
	nextRun       time.Time
}

type schedulerManualRunKey struct{}
type schedulerLibraryScanIDKey struct{}

const schedulerMaxInterval = 30 * 24 * time.Hour

// NewSchedulerService is the constructor.
func NewSchedulerService(
	log *zap.Logger,
	repo *repository.Container,
	scanner *ScannerService,
	organizer *OrganizerService,
	hub *Hub,
) *SchedulerService {
	return &SchedulerService{
		log:       log,
		repo:      repo,
		scanner:   scanner,
		organizer: organizer,
		hub:       hub,
		now:       time.Now,
		stopCh:    make(chan struct{}),
	}
}

// Start kicks off every job in its own goroutine and returns immediately.
func (s *SchedulerService) Start(ctx context.Context) {
	s.supplementCtx, s.supplementCancel = context.WithCancel(ctx)
	s.jobs = []*scheduledJob{
		s.configuredJob(ctx, "library_scan", "scan.periodic_enabled", "scan.interval_seconds", false, 24*time.Hour, s.jobScanLibraries),
		s.configuredJob(ctx, "organize_source", "organize.auto", "organize.interval_seconds", false, 5*time.Minute, s.jobOrganizeSource),
		s.configuredJob(ctx, "people_backfill_periodic", "people.backfill_periodic_enabled", "people.backfill_interval_seconds", true, 10*time.Minute, s.jobPeopleBackfill),
		s.configuredJob(ctx, "people_translation_periodic", "people.translation_periodic_enabled", "people.translation_interval_seconds", true, 10*time.Minute, s.jobPeopleTranslation),
		s.configuredJob(ctx, "tmdb_artwork_local_repair", "metadata.tmdb_artwork_local_repair_enabled", "metadata.tmdb_artwork_local_repair_interval_seconds", false, 24*time.Hour, s.jobTMDbArtworkLocalRepair),
		s.configuredJob(ctx, "tmdb_artwork_missing_recheck", "metadata.tmdb_artwork_missing_recheck_enabled", "metadata.tmdb_artwork_missing_recheck_interval_seconds", false, 24*time.Hour, s.jobTMDbArtworkMissingRecheck),
		s.configuredJob(ctx, "douban_artwork_local_repair", "metadata.douban_artwork_local_repair_enabled", "metadata.douban_artwork_local_repair_interval_seconds", false, 24*time.Hour, s.jobDoubanArtworkLocalRepair),
		s.configuredJob(ctx, "tmdb_episode_metadata_recheck", "metadata.tmdb_episode_metadata_recheck_enabled", "metadata.tmdb_episode_metadata_recheck_interval_seconds", false, 24*time.Hour, s.jobTMDbEpisodeMetadataRecheck),
		s.configuredJob(ctx, "douban_movie_enrichment", "metadata.douban_movie_enrichment_enabled", "metadata.douban_movie_enrichment_interval_seconds", false, 24*time.Hour, s.jobDoubanMovieEnrichment),
		s.configuredJob(ctx, "account_cleanup", SettingAccountCleanupEnabled, "device.account_cleanup_interval_seconds", false, 24*time.Hour, s.jobAccountCleanup),
	}
	if s.hongguo != nil {
		for _, kind := range []string{TaskKindHongGuoSync, TaskKindHongGuoRefresh, TaskKindHongGuoArtwork} {
			interval := 24 * time.Hour
			if kind == TaskKindHongGuoArtwork {
				interval = time.Hour
			}
			s.jobs = append(s.jobs, s.configuredJob(ctx, kind, "hongguo."+kind+".enabled", "hongguo."+kind+".interval_seconds", kind != TaskKindHongGuoSync, interval, func(ctx context.Context) error { return s.hongguo.Run(ctx, kind, "") }))
		}
	}
	if s.hongguoDownloads != nil {
		job := s.configuredJob(ctx, TaskKindHongGuoSupplement, "hongguo.download_supplement.enabled", "hongguo.download_supplement.interval_seconds", false, 24*time.Hour, s.jobHongGuoSupplement)
		job.count = 10
		if s.repo != nil && s.repo.Setting != nil {
			if value, err := s.repo.Setting.Get(ctx, hongGuoSupplementCountKey); err == nil {
				if count, err := strconv.Atoi(value); err == nil && count >= 1 && count <= 100 {
					job.count = count
				}
			}
		}
		s.jobs = append(s.jobs, job)
	}
	for _, j := range s.jobs {
		go s.loopWithInitialDelay(ctx, j, j.interval)
	}
}

func (s *SchedulerService) configuredJob(
	ctx context.Context,
	name, enabledKey, intervalKey string,
	defaultEnabled bool,
	defaultInterval time.Duration,
	run func(context.Context) error,
) *scheduledJob {
	enabled := defaultEnabled
	interval := defaultInterval
	if s.repo != nil && s.repo.Setting != nil {
		if value, err := s.repo.Setting.Get(ctx, enabledKey); err == nil && strings.TrimSpace(value) != "" {
			enabled = parseBoolSetting(value, defaultEnabled)
		}
		if value, err := s.repo.Setting.Get(ctx, intervalKey); err == nil {
			if seconds, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64); parseErr == nil {
				if seconds >= int64(time.Minute/time.Second) && seconds <= int64(schedulerMaxInterval/time.Second) {
					interval = time.Duration(seconds) * time.Second
				}
			}
		}
	}
	return &scheduledJob{
		name: name, interval: interval, run: run, enabled: enabled, configurable: true,
		enabledKey: enabledKey, intervalKey: intervalKey,
		minInterval: time.Minute, maxInterval: schedulerMaxInterval, reset: make(chan struct{}, 1),
	}
}

// Stop signals every job loop to exit on the next tick.
func (s *SchedulerService) Stop() {
	s.mu.Lock()
	select {
	case <-s.stopCh:
		// already closed
	default:
		close(s.stopCh)
	}
	if s.supplementCancel != nil {
		s.supplementCancel()
	}
	s.mu.Unlock()
	s.supplementWG.Wait()
}
