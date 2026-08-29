package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSchedulerPeriodicLocalScanRunsEveryInvocation(t *testing.T) {
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
	scheduler := NewSchedulerService(log, repos, scanner, nil, NewHub(log))
	tracker := NewTaskTrackerService(log, nil)
	scheduler.SetTaskTracker(tracker)
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
	if got := countMedia(t, repos); got != 2 {
		t.Fatalf("media count after second scan = %d, want 2", got)
	}
	if got := tracker.Snapshot().Recent[0].Metrics["skipped"]; got != 1 {
		t.Fatalf("second scan skipped metric = %d, want 1", got)
	}
}

func TestSchedulerManualLocalScanBypassesDisabledSchedule(t *testing.T) {
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
	scheduler := NewSchedulerService(log, repos, scanner, nil, NewHub(log))
	scheduler.now = func() time.Time {
		return time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	}
	scheduler.jobs = []*scheduledJob{{
		name: "library_scan", interval: 24 * time.Hour, run: scheduler.jobScanLibraries,
		configurable: true, enabled: false,
	}}

	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatalf("first periodic local scan: %v", err)
	}
	writeOrgFile(t, filepath.Join(libraryPath, "Manual.Show.S01E02.mkv"), "episode 2")
	if err := scheduler.RunNow(t.Context(), "library_scan"); err != nil {
		t.Fatalf("manual local scan: %v", err)
	}
	if got := countMedia(t, repos); got != 2 {
		t.Fatalf("manual scan should bypass disabled schedule, media count = %d", got)
	}
}

func TestSchedulerLibraryScanTargetsOneLibraryWhilePeriodicScansAll(t *testing.T) {
	root := t.TempDir()
	pathA := filepath.Join(root, "library-a")
	pathB := filepath.Join(root, "library-b")
	writeOrgFile(t, filepath.Join(pathA, "Movie.A.mkv"), "a")
	writeOrgFile(t, filepath.Join(pathB, "Movie.B.mkv"), "b")

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	libA := model.Library{Name: "媒体库 A", Path: pathA, Type: "movie", Enabled: true}
	libB := model.Library{Name: "媒体库 B", Path: pathB, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &libA); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &libB); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scheduler := NewSchedulerService(log, repos, scanner, nil, NewHub(log))

	manualCtx := context.WithValue(t.Context(), schedulerLibraryScanIDKey{}, libA.ID)
	if err := scheduler.jobScanLibraries(manualCtx); err != nil {
		t.Fatal(err)
	}
	var countA, countB int64
	if err := db.Model(&model.Media{}).Where("library_id = ?", libA.ID).Count(&countA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Media{}).Where("library_id = ?", libB.ID).Count(&countB).Error; err != nil {
		t.Fatal(err)
	}
	if countA != 1 || countB != 0 {
		t.Fatalf("manual counts = (%d, %d), want (1, 0)", countA, countB)
	}

	if err := scheduler.jobScanLibraries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Media{}).Where("library_id = ?", libB.ID).Count(&countB).Error; err != nil {
		t.Fatal(err)
	}
	if countB != 1 {
		t.Fatalf("periodic library B count = %d, want 1", countB)
	}
}
