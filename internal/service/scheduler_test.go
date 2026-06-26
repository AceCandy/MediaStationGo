package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSchedulerOrganizeSourceDisabledByDefault(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "国产剧", "狂飙.S01E01.2023.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organize.source_dir", src); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organize.target_dir", dest); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organize.transfer_mode", "copy"); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, organizer, nil, NewHub(zap.NewNop()), "")
	if err := scheduler.jobOrganizeSource(t.Context()); err != nil {
		t.Fatalf("disabled organize source job should be a no-op: %v", err)
	}

	want := filepath.Join(dest, "电视剧", "国产剧", "狂飙", "Season 01", "狂飙 - S01E01.mkv")
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("disabled job should not create %q, stat err=%v", want, err)
	}
}

func TestSchedulerOrganizeSourceUsesConfiguredSourceAndDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "国产剧", "狂飙.S01E01.2023.1080p.mkv"), "episode")

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organize.auto":          "true",
		"organize.source_dir":    src,
		"organize.target_dir":    dest,
		"organize.transfer_mode": "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, organizer, nil, NewHub(zap.NewNop()), "")
	if err := scheduler.jobOrganizeSource(t.Context()); err != nil {
		t.Fatalf("organize source job: %v", err)
	}

	want := filepath.Join(dest, "电视剧", "国产剧", "狂飙", "Season 01", "狂飙 - S01E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected scheduled organize output at %q: %v", want, err)
	}
}

func TestSchedulerRunNowOrganizeSourceBypassesDisabledSwitch(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "Dune 2021 1080p.mkv"), "movie")

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organize.source_dir":    src,
		"organize.target_dir":    dest,
		"organize.transfer_mode": "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, organizer, nil, NewHub(zap.NewNop()), "")
	scheduler.jobs = []*scheduledJob{{
		name:     "organize_source",
		interval: time.Minute,
		run:      scheduler.jobOrganizeSource,
	}}

	if err := scheduler.RunNow(t.Context(), "organize_source"); err != nil {
		t.Fatalf("run now organize source: %v", err)
	}

	want := filepath.Join(dest, "电影", "Dune (2021)", "Dune (2021).mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected run-now organize output at %q: %v", want, err)
	}
}

func TestSchedulerRunNowAsyncSurvivesCallerCancellation(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil, nil, nil, "")
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
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil, nil, nil, "")
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

func TestSchedulerStartDoesNotRegisterSubscriptionPullJob(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil, nil, nil, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	scheduler.Start(ctx)
	defer scheduler.Stop()

	for _, status := range scheduler.Status() {
		if strings.Contains(status.Name, "subscription") {
			t.Fatalf("scheduler registered subscription job %q; subscriptions must be owned by SubscriptionService only", status.Name)
		}
	}
}

func TestSchedulerOrganizeSourceSyncsVisibilityWhenTargetAlreadyExists(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "国产剧", "狂飙.S01E01.2023.1080p.mkv"), "episode")

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organize.source_dir":    src,
		"organize.target_dir":    dest,
		"organize.transfer_mode": "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	lib := model.Library{Name: "国产剧", Path: filepath.Join(dest, "电视剧", "国产剧"), Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	if _, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	}); err != nil {
		t.Fatalf("seed organize destination: %v", err)
	}
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	scheduler := NewSchedulerService(zap.NewNop(), repos, scanner, nil, organizer, nil, NewHub(zap.NewNop()), "")
	scheduler.jobs = []*scheduledJob{{
		name:     "organize_source",
		interval: time.Minute,
		run:      scheduler.jobOrganizeSource,
	}}

	if err := scheduler.RunNow(t.Context(), "organize_source"); err != nil {
		t.Fatalf("run now organize source: %v", err)
	}
	var count int64
	if err := repos.DB.Model(&model.Media{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("target already exists should still be scanned into DB, count=%d want 1", count)
	}
}

func TestSchedulerCloudSyncImportsMountedCloudLibrary(t *testing.T) {
	upstream := newOpenListAPIServer(t, func(path string, page, perPage int) ([]openListTestEntry, int) {
		if path != "/" {
			t.Fatalf("unexpected openlist path %q", path)
		}
		return []openListTestEntry{{Name: "Cloud.Movie.2026.mkv", Size: 1024}}, 1
	})
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	local := model.Library{Name: "电影", Path: "/media/电影", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &local); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList · 电影", Path: "cloud://openlist", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "cloud.auto_sync_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, storage, NewHub(log), "")
	scheduler.now = func() time.Time { return fixedNightlySyncTime() }

	if err := scheduler.jobSyncCloudLibraries(t.Context()); err != nil {
		t.Fatalf("cloud sync: %v", err)
	}
	var media model.Media
	if err := repos.DB.First(&media, "path = ?", "cloud://openlist/Cloud.Movie.2026.mkv").Error; err != nil {
		t.Fatalf("cloud media not imported: %v", err)
	}
	if media.STRMURL != "/api/cloud/play/openlist?ref=%2FCloud.Movie.2026.mkv" {
		t.Fatalf("strm url = %q", media.STRMURL)
	}
}

func TestSchedulerCloudSyncRunsOnlyOnceInsideNightlyWindow(t *testing.T) {
	var requests atomic.Int32
	upstream := newOpenListAPIServer(t, func(path string, page, perPage int) ([]openListTestEntry, int) {
		requests.Add(1)
		if path != "/" {
			t.Fatalf("unexpected openlist path %q", path)
		}
		return []openListTestEntry{{Name: "Nightly.Cloud.Movie.2026.mkv", Size: 1024}}, 1
	})
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), cloudAutoSyncEnabledKey, "true"); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, storage, NewHub(log), "")

	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 11, 22, 30, 0, 0, time.Local)
	}
	if err := scheduler.jobSyncCloudLibraries(t.Context()); err != nil {
		t.Fatalf("cloud sync outside window: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("outside nightly window made %d requests, want 0", got)
	}

	scheduler.now = func() time.Time { return fixedNightlySyncTime() }
	if err := scheduler.jobSyncCloudLibraries(t.Context()); err != nil {
		t.Fatalf("cloud sync inside window: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("inside nightly window requests = %d, want 1", got)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1", got)
	}

	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 12, 4, 15, 0, 0, time.Local)
	}
	if !cloudAutoSyncInWindow(scheduler.now()) {
		t.Fatalf("04:15 should still be inside overnight cloud sync window")
	}
	if got := cloudAutoSyncWindowDate(scheduler.now()); got != "2026-06-11" {
		t.Fatalf("04:15 should belong to previous nightly window, got %s", got)
	}
	if err := scheduler.jobSyncCloudLibraries(t.Context()); err != nil {
		t.Fatalf("second cloud sync same overnight window: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("same overnight auto sync should not rerun, requests = %d", got)
	}

	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 12, 5, 0, 0, 0, time.Local)
	}
	if cloudAutoSyncInWindow(scheduler.now()) {
		t.Fatalf("05:00 should be outside overnight cloud sync window")
	}
}

func TestSchedulerRunNowCloudSyncBypassesNightlyWindow(t *testing.T) {
	var requests atomic.Int32
	upstream := newOpenListAPIServer(t, func(path string, page, perPage int) ([]openListTestEntry, int) {
		requests.Add(1)
		return []openListTestEntry{{Name: "Manual.Cloud.Movie.2026.mkv", Size: 1024}}, 1
	})
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, storage, NewHub(log), "")
	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, time.Local)
	}
	scheduler.jobs = []*scheduledJob{{
		name:     "cloud_sync",
		interval: time.Minute,
		run:      scheduler.jobSyncCloudLibraries,
	}}

	if err := scheduler.RunNow(t.Context(), "cloud_sync"); err != nil {
		t.Fatalf("manual cloud sync: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("manual cloud sync requests = %d, want 1", got)
	}
}

func TestSchedulerPeriodicLocalScanRunsAtMostOncePerDay(t *testing.T) {
	root := t.TempDir()
	libraryPath := filepath.Join(root, "library")
	writeOrgFile(t, filepath.Join(libraryPath, "Daily.Show.S01E01.mkv"), "episode 1")

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "scan.periodic_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "本地剧集", Path: libraryPath, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, nil, NewHub(log), "")
	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	}

	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatalf("first periodic local scan: %v", err)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count after first scan = %d, want 1", got)
	}

	writeOrgFile(t, filepath.Join(libraryPath, "Daily.Show.S01E02.mkv"), "episode 2")
	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatalf("same-day periodic local scan: %v", err)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("same-day periodic scan should not import new file, media count = %d", got)
	}

	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 21, 10, 0, 0, 0, time.Local)
	}
	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatalf("next-day periodic local scan: %v", err)
	}
	if got := countMedia(t, repos); got != 2 {
		t.Fatalf("media count after next-day scan = %d, want 2", got)
	}
}

func TestSchedulerManualLocalScanBypassesDailyPeriodicLimit(t *testing.T) {
	root := t.TempDir()
	libraryPath := filepath.Join(root, "library")
	writeOrgFile(t, filepath.Join(libraryPath, "Manual.Show.S01E01.mkv"), "episode 1")

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "scan.periodic_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "手动本地剧集", Path: libraryPath, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, nil, NewHub(log), "")
	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	}
	scheduler.jobs = []*scheduledJob{{
		name:     "library_scan",
		interval: 24 * time.Hour,
		run:      scheduler.jobScanLibraries,
	}}

	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatalf("first periodic local scan: %v", err)
	}
	writeOrgFile(t, filepath.Join(libraryPath, "Manual.Show.S01E02.mkv"), "episode 2")
	if err := scheduler.RunNow(t.Context(), "library_scan"); err != nil {
		t.Fatalf("manual local scan: %v", err)
	}
	if got := countMedia(t, repos); got != 2 {
		t.Fatalf("manual scan should bypass daily periodic limit, media count = %d", got)
	}
}

func TestSchedulerCloudSyncDisabledByDefault(t *testing.T) {
	var requests atomic.Int32
	upstream := newOpenListAPIServer(t, func(path string, page, perPage int) ([]openListTestEntry, int) {
		requests.Add(1)
		return []openListTestEntry{{Name: "Cloud.Movie.2026.mkv", Size: 1024}}, 1
	})
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, storage, NewHub(log), "")

	if err := scheduler.jobSyncCloudLibraries(t.Context()); err != nil {
		t.Fatalf("disabled cloud sync should be a no-op: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("cloud sync made %d upstream requests while disabled by default", got)
	}
	if got := countMedia(t, repos); got != 0 {
		t.Fatalf("media count = %d, want 0 while cloud sync disabled by default", got)
	}
}

func fixedNightlySyncTime() time.Time {
	return time.Date(2026, 6, 11, 23, 30, 0, 0, time.Local)
}

func TestSchedulerLoopWaitsIntervalAfterSlowRun(t *testing.T) {
	scheduler := NewSchedulerService(zap.NewNop(), nil, nil, nil, nil, nil, nil, "")
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
