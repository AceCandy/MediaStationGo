package handler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSTRMRefreshNFOUsesCommonScanWithoutScrape(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.LibraryRoot{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	for _, libraryType := range []string{model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		t.Run(libraryType, func(t *testing.T) {
			// 不可达目录让后台扫描快速结束；排队结果仍必须明确禁止网络刮削。
			lib := model.Library{Name: libraryType, Type: libraryType, Path: filepath.Join(t.TempDir(), "missing"), Enabled: true}
			if err := repos.Library.Create(t.Context(), &lib); err != nil {
				t.Fatal(err)
			}
			tracker := service.NewTaskTrackerService(log, nil)
			scanner := service.NewScannerService(&config.Config{}, log, repos, nil, nil, nil)
			svc := &service.Container{Repo: repos, Scan: scanner, Tasks: tracker, Scraper: &service.ScraperService{}}
			refresh := queueSTRMRefreshAfterChanges(t.Context(), svc, lib.Path, strmRefreshQueueOptions{TaskName: "STRM", Changed: true, ScrapeAfter: true})
			deadline := time.Now().Add(5 * time.Second)
			for {
				finish, idle := scanner.TryBeginLocalScan(lib.ID)
				if idle {
					finish()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("scan did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			if !refresh.Queued || refresh.ScrapeQueued {
				t.Fatalf("NFO refresh must scan without scraping: %+v", refresh)
			}
			history, err := tracker.List(1, 10)
			if err != nil || len(history.Items) != 1 || history.Items[0].Kind != service.TaskKindScan || history.Items[0].System != model.TaskSystemCommon {
				t.Fatalf("NFO refresh history=%+v err=%v", history, err)
			}
		})
	}
}

func TestSTRMRefreshTaskMetricsIncludesScanAndScrape(t *testing.T) {
	metrics := strmRefreshTaskMetrics(
		&service.ScanResult{Visited: 3, Added: 2, Updated: 1, ErrorCount: 1},
		service.EnrichLibraryResult{Matched: 4, Processed: 5, Candidates: 6, Failed: 1},
	)

	want := map[string]int64{
		"visited":           3,
		"added":             2,
		"updated":           1,
		"errors":            1,
		"scrape_matched":    4,
		"scrape_processed":  5,
		"scrape_candidates": 6,
		"scrape_failed":     1,
	}
	for key, value := range want {
		if metrics[key] != value {
			t.Fatalf("metrics[%q] = %d, want %d in %#v", key, metrics[key], value, metrics)
		}
	}
	if _, ok := metrics["scrape_reclassified"]; ok {
		t.Fatalf("scrape metrics must not report file reclassification: %#v", metrics)
	}
}

func TestSTRMRefreshScrapeSkipReasonRequiresScrapeRequest(t *testing.T) {
	if got := strmRefreshScrapeSkipReason(&service.STRMRefreshResult{Requested: true, Reason: "no strm changes"}); got != "" {
		t.Fatalf("skip reason without scrape request = %q, want empty", got)
	}

	got := strmRefreshScrapeSkipReason(&service.STRMRefreshResult{
		Requested:       true,
		ScrapeRequested: true,
		Reason:          "no matching local library",
	})
	if got != "refresh not queued: no matching local library" {
		t.Fatalf("skip reason = %q", got)
	}
}
