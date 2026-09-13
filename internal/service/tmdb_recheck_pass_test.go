package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbRecheckUsesDedicatedDetailsTimeout(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), tmdbRecheckDetailsTimeout)
	defer cancel()
	deadline, ok := ctx.Deadline()
	got := deadline.Sub(started)
	if !ok || got < 30*time.Second || got > 31*time.Second || tmdbDetailsTimeout != 8*time.Second {
		t.Fatalf("deadline=%v timeout=%s shared_timeout=%s", ok, got, tmdbDetailsTimeout)
	}
}

func TestTMDbRecheckPassReusesScatteredSeasons(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{},
		&model.TMDbRecheckJob{}, &model.TMDbRecheckSeasonLease{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckScan{}, &model.TMDbRecheckAssetChange{})
	if err := database.EnsureTMDbRecheckTriggers(db); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "测试剧"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "42"})
	const seasonCount = 36 // 超过整季缓存容量，旧的全局到期排序会再次请求第二集所在季。
	past := time.Now().Add(-time.Hour)
	for seasonNumber := 1; seasonNumber <= seasonCount; seasonNumber++ {
		season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: seasonNumber})
		for episodeNumber := 1; episodeNumber <= 2; episodeNumber++ {
			episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: episodeNumber})
			if err := db.Create(&model.Media{MetadataID: episode.ID, Path: fmt.Sprintf("/test/%d-%d.mkv", seasonNumber, episodeNumber)}).Error; err != nil {
				t.Fatal(err)
			}
			due := past.Add(time.Duration(episodeNumber*seasonCount+seasonNumber) * time.Second)
			if err := db.Create(&model.TMDbRecheckJob{MetadataID: episode.ID, DueAt: &due}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// 该用例聚焦已建队列；跳过文件核对与父级补建，保存仍使用真实触发器和事务。
	if err := db.Model(&model.TMDbRecheckChange{}).Where("pending").Update("pending", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TMDbRecheckScan{ID: 1, NextAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var season int
		if _, err := fmt.Sscanf(r.URL.Path, "/tv/42/season/%d", &season); err != nil || strings.Contains(r.URL.Path, "/episode/") {
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"id":%d,"season_number":%d,"episodes":[{"id":%d,"season_number":%d,"episode_number":1,"name":"上篇","overview":"中文简介","air_date":"2026-01-01"},{"id":%d,"season_number":%d,"episode_number":2,"name":"下篇","overview":"中文简介","air_date":"2026-01-02"}]}`, season+100, season, season*10+1, season, season*10+2, season)
	}))
	defer server.Close()
	repos := repository.New(db)
	s := &ScraperService{repo: repos, tmdb: newTMDbTestProvider(server.URL), artwork: NewArtworkStore(&config.Config{}, repos.Artwork, nil)}
	metrics := map[string]int64{}
	if err := s.runTMDbRecheckQueue(t.Context(), metrics, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != seasonCount || metrics["seasons_scanned"] != seasonCount || metrics["requests"] != int64(calls.Load()) || metrics["scanned"] != seasonCount*2 || metrics["checked"] != seasonCount*2 || metrics["remaining"] != 0 || metrics["failed"] != 0 {
		t.Fatalf("calls=%d metrics=%v", calls.Load(), metrics)
	}
	if _, ok := metrics["details_ms"]; !ok {
		t.Fatal("detail timing missing")
	}
	if _, ok := metrics["save_ms"]; !ok {
		t.Fatal("save timing missing")
	}
}

func TestTMDbRecheckSeasonSharesFailuresAndReleasesCancellation(t *testing.T) {
	for _, mode := range []string{"http_error", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckSeasonLease{}, &model.TMDbRecheckChange{})
			series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "42"})
			season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1})
			past := time.Now().Add(-time.Hour)
			for number := 1; number <= 3; number++ {
				episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: number})
				if err := db.Create(&model.Media{MetadataID: episode.ID, Path: fmt.Sprintf("/failure/%d.mkv", number)}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.TMDbRecheckJob{MetadataID: episode.ID, DueAt: &past}).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "cancel" {
					cancel()
				}
				if mode != "http_error" {
					<-r.Context().Done()
					return
				}
				http.Error(w, "failed", http.StatusInternalServerError)
			}))
			defer server.Close()
			provider := newTMDbTestProvider(server.URL)
			if mode == "timeout" {
				provider.client.Timeout = 50 * time.Millisecond
			}
			repos := repository.New(db)
			s := &ScraperService{repo: repos, tmdb: provider}
			cutoff := time.Now()
			lease, err := repos.Metadata.ClaimTMDbRecheckSeason(ctx, cutoff)
			if err != nil || lease == nil {
				t.Fatalf("lease=%+v err=%v", lease, err)
			}
			metrics := map[string]int64{}
			err = s.processTMDbRecheckSeason(ctx, lease, cutoff, func(local map[string]int64, _ []string) {
				for k, v := range local {
					metrics[k] += v
				}
			})
			if mode == "cancel" && !errors.Is(err, context.Canceled) || mode != "cancel" && err != nil {
				t.Fatal(err)
			}
			var jobs []model.TMDbRecheckJob
			if err := db.Find(&jobs).Error; err != nil {
				t.Fatal(err)
			}
			for _, job := range jobs {
				wantStatus, wantAttempts := "retry", 1
				if mode == "cancel" {
					wantStatus, wantAttempts = "pending", 0
				}
				if job.Status != wantStatus || job.Attempts != wantAttempts || job.LeaseToken != "" || job.DueAt == nil || !job.DueAt.After(cutoff) {
					t.Fatalf("job=%+v", job)
				}
			}
			if calls.Load() != 1 || mode != "cancel" && metrics["failed"] != 3 {
				t.Fatalf("calls=%d metrics=%v", calls.Load(), metrics)
			}
			var leases int64
			if err := db.Model(&model.TMDbRecheckSeasonLease{}).Count(&leases).Error; err != nil || leases != 0 {
				t.Fatalf("leases=%d err=%v", leases, err)
			}
		})
	}
}

func TestTMDbRecheckStagePreservesClassificationAndRedaction(t *testing.T) {
	for _, stage := range []struct{ key, label string }{{"details", "详情请求"}, {"image", "图片下载及落盘"}, {"save", "数据库保存"}} {
		metrics := map[string]int64{}
		cause := fmt.Errorf("https://example.test/private?api_key=secret: %w", context.DeadlineExceeded)
		err := recordTMDbRecheckStage(metrics, stage.key, stage.label, time.Now().Add(-time.Second), cause)
		text := sanitizeTaskLogError(err).Error()
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(text, stage.label) || !strings.Contains(text, "耗时") || strings.Contains(text, "secret") || metrics[stage.key+"_failed"] != 1 || metrics[stage.key+"_ms"] < 1000 {
			t.Fatalf("err=%s metrics=%v", text, metrics)
		}
		for _, cause := range []error{context.Canceled, repository.ErrTMDbRecheckChanged} {
			err := recordTMDbRecheckStage(metrics, stage.key, stage.label, time.Now(), cause)
			if !errors.Is(err, cause) || metrics[stage.key+"_failed"] != 1 {
				t.Fatalf("cancellation/contention counted as failure: %v %v", err, metrics)
			}
		}
	}
}

func TestTMDbRecheckFailuresReportActualStage(t *testing.T) {
	for _, stage := range []struct{ key, label string }{{"details", "详情请求"}, {"image", "图片下载及落盘"}, {"save", "数据库保存"}} {
		t.Run(stage.key, func(t *testing.T) {
			db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckChange{})
			repos := repository.New(db)
			series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "测试剧"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "42"})
			season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1})
			episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1})
			if err := db.Create(&model.Media{MetadataID: episode.ID, Path: "/test/episode.mkv"}).Error; err != nil {
				t.Fatal(err)
			}
			past := time.Now().Add(-time.Hour)
			if err := db.Create(&model.TMDbRecheckJob{MetadataID: episode.ID, DueAt: &past}).Error; err != nil {
				t.Fatal(err)
			}
			if stage.key == "save" {
				if err := db.Exec(`CREATE FUNCTION reject_recheck_snapshot() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'snapshot write failed'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER reject_recheck_snapshot BEFORE INSERT ON metadata_provider_snapshots FOR EACH ROW EXECUTE FUNCTION reject_recheck_snapshot()`).Error; err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage.key == "details" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				still := ""
				if stage.key == "image" {
					still = "/still.jpg"
				}
				fmt.Fprintf(w, `{"id":100,"season_number":1,"episodes":[{"id":101,"season_number":1,"episode_number":1,"name":"上篇","overview":"中文简介","still_path":%q}]}`, still)
			}))
			defer server.Close()
			s := &ScraperService{repo: repos, tmdb: newTMDbTestProvider(server.URL), artwork: NewArtworkStore(&config.Config{}, repos.Artwork, nil)}
			job, err := repos.Metadata.ClaimTMDbRecheck(t.Context())
			if err != nil || job == nil {
				t.Fatalf("claim=%+v err=%v", job, err)
			}
			metrics := map[string]int64{}
			details, err := s.processTMDbRecheck(withTMDbSeasonBatch(t.Context()), job, metrics)
			if err != nil || metrics["failed"] != 1 || metrics[stage.key+"_failed"] != 1 || !strings.Contains(strings.Join(details, " "), stage.label+"（耗时") {
				t.Fatalf("details=%v metrics=%v err=%v", details, metrics, err)
			}
			var stored model.TMDbRecheckJob
			if err := db.First(&stored, "metadata_id=?", job.MetadataID).Error; err != nil || stored.Status != "retry" || stored.Attempts != 1 || stored.DueAt == nil || !stored.DueAt.After(time.Now()) {
				t.Fatalf("retry state=%+v err=%v", stored, err)
			}
		})
	}
}
