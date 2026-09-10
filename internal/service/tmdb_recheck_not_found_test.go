package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestTMDbRecheckNotFoundCooldownAndIdentity(t *testing.T) {
	for _, source := range []string{"season", "episode", "season_inventory"} {
		t.Run(source, func(t *testing.T) {
			kind := source
			if source == "season_inventory" {
				kind = "episode"
			}
			db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{})
			if err := db.AutoMigrate(&model.TMDbRecheckJob{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckScan{}, &model.TMDbRecheckAssetChange{}); err != nil {
				t.Fatal(err)
			}
			if err := database.EnsureTMDbRecheckTriggers(db); err != nil {
				t.Fatal(err)
			}
			repos := repository.New(db)
			series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "测试剧"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "42"})
			season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 0})
			target := season
			if kind == "episode" {
				target = createServiceTestMetadata(t, db, model.MetadataItem{Kind: kind, ParentID: &season.ID, EpisodeNum: 88})
			}
			if err := db.Create(&model.Media{MetadataID: target.ID, Path: "/test/404.strm"}).Error; err != nil {
				t.Fatal(err)
			}
			past := time.Now().Add(-time.Hour)
			if err := db.Create(&model.TMDbRecheckJob{MetadataID: target.ID, DueAt: &past}).Error; err != nil {
				t.Fatal(err)
			}
			changed := false
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if changed {
					if err := db.Model(target).Update("overview", "请求中发生变更").Error; err != nil {
						t.Error(err)
					}
				}
				if source == "season_inventory" {
					var seriesID, seasonNumber int
					if strings.Contains(r.URL.Path, "/episode/") {
						t.Errorf("unexpected single request: %s", r.URL.Path)
					}
					_, _ = fmt.Sscanf(r.URL.Path, "/tv/%d/season/%d", &seriesID, &seasonNumber)
					fmt.Fprintf(w, `{"id":200,"season_number":%d,"episodes":[]}`, seasonNumber)
					return
				}
				http.NotFound(w, r)
			}))
			defer upstream.Close()
			cfg := &config.Config{}
			cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
			s := &ScraperService{repo: repos, tmdb: NewTMDbProvider(cfg, zap.NewNop(), nil)}
			process := func() (model.TMDbRecheckJob, map[string]int64, []string) {
				t.Helper()
				job, err := repos.Metadata.ClaimTMDbRecheck(t.Context())
				if err != nil || job == nil {
					t.Fatalf("claim=%+v %v", job, err)
				}
				metrics := map[string]int64{}
				ctx := t.Context()
				if source == "season_inventory" {
					ctx = withTMDbSeasonBatch(ctx)
				}
				details, err := s.processTMDbRecheck(ctx, job, metrics)
				if err != nil {
					t.Fatal(err)
				}
				var stored model.TMDbRecheckJob
				if err := db.First(&stored, "metadata_id=?", target.ID).Error; err != nil {
					t.Fatal(err)
				}
				return stored, metrics, details
			}
			before := time.Now()
			stored, metrics, details := process()
			if stored.Status != "not_found" || stored.NotFoundIdentity == "" || stored.DueAt == nil || stored.DueAt.Before(before.Add(72*time.Hour)) || stored.DueAt.After(time.Now().Add(72*time.Hour)) || metrics["not_found"] != 1 || metrics["failed"] != 0 {
				t.Fatalf("404 classification: %+v %v", stored, metrics)
			}
			for _, part := range []string{"测试剧", "S0", "TMDb 42", "3 天"} {
				if !strings.Contains(strings.Join(details, " "), part) {
					t.Fatalf("missing %q in %v", part, details)
				}
			}
			if job, err := repos.Metadata.ClaimTMDbRecheck(t.Context()); err != nil || job != nil {
				t.Fatalf("cooldown bypassed %+v %v", job, err)
			}
			if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", target.ID).Update("due_at", past).Error; err != nil {
				t.Fatal(err)
			}
			stored, metrics, _ = process()
			if stored.Status != "not_found" || stored.Attempts != 2 || metrics["not_found"] != 1 {
				t.Fatalf("due 404 not rechecked: %+v %v", stored, metrics)
			}
			// 普通变更与初始化归并保留 404 冷却。
			if err := db.Model(target).Update("overview", "普通资料变更").Error; err != nil {
				t.Fatal(err)
			}
			for {
				more, err := repos.Metadata.ExpandTMDbRecheckChange(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if !more {
					break
				}
			}
			var preserved model.TMDbRecheckJob
			if err := db.First(&preserved, "metadata_id=?", target.ID).Error; err != nil {
				t.Fatal(err)
			}
			if preserved.Status != "not_found" || !preserved.DueAt.Equal(*stored.DueAt) {
				t.Fatalf("ordinary edit bypassed cooldown: %+v", preserved)
			}
			// 身份改变只唤醒本地核对；实际详情请求仍由任务领取。
			if err := db.Model(season).Update("season_num", 1).Error; err != nil {
				t.Fatal(err)
			}
			for {
				more, err := repos.Metadata.ExpandTMDbRecheckChange(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if !more {
					break
				}
			}
			if err := db.First(&preserved, "metadata_id=?", target.ID).Error; err != nil {
				t.Fatal(err)
			}
			if preserved.Status != "pending" || preserved.DueAt.After(time.Now()) {
				t.Fatalf("identity not awakened: %+v", preserved)
			}
			// 排除由父级归并补建的其它季任务，单独验证当前目标的过期响应。
			if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id<>?", target.ID).Update("due_at", nil).Error; err != nil {
				t.Fatal(err)
			}
			changed = true
			stored, metrics, _ = process()
			if stored.Status != "pending" || metrics["not_found"] != 0 {
				t.Fatalf("stale 404 committed: %+v %v", stored, metrics)
			}
		})
	}
}

func TestTMDbRecheckOtherFailuresRemainRetry(t *testing.T) {
	// 图片阶段与保存阶段直接进入普通重试，即使其错误携带 HTTP 404。
	db := newServiceTestDB(t, &model.TMDbRecheckJob{})
	repos := repository.New(db)
	s := &ScraperService{repo: repos}
	for i, cause := range []error{&tmdbHTTPStatusError{Path: "/image", StatusCode: 404}, &tmdbHTTPStatusError{Path: "/tv", StatusCode: 429}, context.DeadlineExceeded} {
		past := time.Now().Add(-time.Hour)
		if err := db.Create(&model.TMDbRecheckJob{MetadataID: fmt.Sprint(i), DueAt: &past}).Error; err != nil {
			t.Fatal(err)
		}
		job, err := repos.Metadata.ClaimTMDbRecheck(t.Context())
		if err != nil || job == nil {
			t.Fatal(err)
		}
		metrics := map[string]int64{}
		if _, err := s.retryTMDbRecheck(t.Context(), job, metrics, cause); err != nil {
			t.Fatal(err)
		}
		var stored model.TMDbRecheckJob
		if err := db.First(&stored, "metadata_id=?", job.MetadataID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != "retry" || metrics["failed"] != 1 || stored.DueAt.After(time.Now().Add(6*time.Minute)) {
			t.Fatalf("ordinary retry changed: %+v %v", stored, metrics)
		}
	}
}

func TestFileManagerRevalidatesSTRMTargetAfterPreview(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	svc, repos := newFileManagerTestServiceWithRepo(t, root)
	target, rejected := filepath.Join(root, "allowed.mkv"), filepath.Join(outside, "outside.mkv")
	sidecar := filepath.Join(root, "episode.strm")
	for _, path := range []string{target, rejected} {
		if err := os.WriteFile(path, []byte("video"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(sidecar, []byte(target), 0600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{Path: sidecar}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar, []byte(rejected), 0600); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []bool{false, true} {
		if _, err := svc.DeleteSTRMTarget(t.Context(), media.ID, parent); err == nil {
			t.Fatal("changed unsafe target accepted")
		}
	}
	for _, path := range []string{target, rejected, sidecar} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file removed: %s %v", path, err)
		}
	}
}
