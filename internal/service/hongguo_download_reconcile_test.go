package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func newReconcileTestService(t *testing.T) (*HongGuoDownloadService, model.HongGuoDownload) {
	t.Helper()
	s := newDownloadTestService(t)
	if err := s.repo.DB.AutoMigrate(model.HongGuoModels()...); err != nil {
		t.Fatal(err)
	}
	seedDownload(t, s)
	row, err := s.repo.HongGuo.ClaimHongGuoDownload(t.Context())
	if err != nil || row == nil {
		t.Fatalf("claim: %v", err)
	}
	return s, *row
}

func TestHongGuoDownloadUnavailableConfirmation(t *testing.T) {
	for _, mode := range []string{"removed", "recovered", "search-found", "search-failed", "network", "partial", "raw", "published", "media", "stale-lease", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			s, row := newReconcileTestService(t)
			ctx := t.Context()
			work, err := s.repo.HongGuo.FindBySourceID(ctx, row.SourceID)
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range []any{
				&model.HongGuoDiscovery{SourceID: row.SourceID, Title: row.Title},
				&model.HongGuoRankEntry{RankKey: "hot-drama", SourceID: row.SourceID, Position: 1},
				&model.HongGuoSyncFailure{SourceID: row.SourceID},
				&model.HongGuoSnapshot{WorkID: work.ID, Payload: `{}`},
				&model.HongGuoArtwork{SourceID: &row.SourceID, WorkID: &work.ID, SourceURL: "https://example.invalid/poster"},
				&model.HongGuoWork{SourceID: "999", Title: "其他作品", Kind: "series"},
			} {
				if err := s.repo.DB.Create(record).Error; err != nil {
					t.Fatal(err)
				}
			}
			var protected model.HongGuoDownload
			if mode == "partial" || mode == "raw" || mode == "published" {
				protected = model.HongGuoDownload{SourceID: row.SourceID, Episode: 2, Status: "completed", Root: row.Root, RelativePath: "protected.mp4"}
				if mode == "raw" {
					protected.Status, protected.RawSize = "waiting_verify", 10
				}
				if mode == "published" {
					protected.Status, protected.SHA256 = "failed", "checkpoint"
				}
				if err := s.repo.DB.Create(&protected).Error; err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(row.Root, "completed", "protected.mp4"), []byte("保留"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "media" {
				media := model.Media{Path: "/retained.mp4", CatalogSource: "hongguo", LookupCatalogID: row.SourceID}
				if err := s.repo.DB.Create(&media).Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "stale-lease" {
				row.LeaseToken = "stale"
			}
			if mode == "rollback" {
				if err := s.repo.DB.Exec(`CREATE FUNCTION reject_hg_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test rollback'; END $$;
CREATE TRIGGER reject_hg_delete BEFORE DELETE ON hongguo_works FOR EACH ROW EXECUTE FUNCTION reject_hg_delete()`).Error; err != nil {
					t.Fatal(err)
				}
			}
			details := 0
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 404, ""
				if r.URL.Path == "/detail" {
					details++
					if mode == "network" {
						status = 503
					}
					if mode == "recovered" && details == 2 {
						status, body = 200, `_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"123","series_name":"测试剧","vid_list":["456"]}}}}`
					}
				} else if strings.HasPrefix(r.URL.Path, "/search/") {
					status, body = 200, `_ROUTER_DATA={"loaderData":{"search_(keyword)/page":{"searchList":[]}}}`
					if mode == "search-found" {
						body = `_ROUTER_DATA={"loaderData":{"search_(keyword)/page":{"searchList":[{"video_data":{"series_id":"123","series_title":"测试剧"}}]}}}`
					}
					if mode == "search-failed" {
						status = 503
					}
				} else {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			_, err = s.latestDownloadDetail(ctx, row)
			removed := mode == "removed" || mode == "partial" || mode == "raw" || mode == "published" || mode == "media"
			if errors.Is(err, errHongGuoDownloadRemoved) != removed {
				t.Fatalf("removed=%v err=%v", removed, err)
			}
			if mode == "recovered" && err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("id = ?", row.ID).Count(&count).Error; err != nil || (count == 0) != removed {
				t.Fatalf("task count=%d err=%v", count, err)
			}
			for _, record := range []any{&model.HongGuoWork{}, &model.HongGuoDiscovery{}, &model.HongGuoRankEntry{}, &model.HongGuoSyncFailure{}, &model.HongGuoDownloadWork{}} {
				if err := s.repo.DB.Model(record).Where("source_id = ?", row.SourceID).Count(&count).Error; err != nil || (count == 0) != (mode == "removed") {
					t.Fatalf("%T count=%d err=%v", record, count, err)
				}
			}
			if err := s.repo.DB.Model(&model.HongGuoWork{}).Where("source_id = ?", "999").Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("other work: %d %v", count, err)
			}
			if protected.ID != "" {
				if err := s.repo.DB.First(&model.HongGuoDownload{}, "id = ?", protected.ID).Error; err != nil {
					t.Fatal(err)
				}
				if content, err := os.ReadFile(filepath.Join(row.Root, "completed", "protected.mp4")); err != nil || string(content) != "保留" {
					t.Fatalf("protected file: %v", err)
				}
			}
		})
	}
}

func TestHongGuoDownloadEpisodeReconciliation(t *testing.T) {
	for _, mode := range []string{"tail", "partial-list", "unstable", "reordered", "same-count-reordered", "replaced", "completed-tail", "replaced-partial", "hole", "hole-unconfirmed", "growth"} {
		t.Run(mode, func(t *testing.T) {
			s, row := newReconcileTestService(t)
			work, err := s.repo.HongGuo.FindBySourceID(t.Context(), row.SourceID)
			if err != nil {
				t.Fatal(err)
			}
			tail := model.HongGuoDownload{SourceID: row.SourceID, Episode: 3, VideoID: "458", Status: "failed"}
			if mode == "completed-tail" {
				tail.Status = "completed"
			}
			if mode == "replaced-partial" {
				tail.Status = "completed"
			}
			if err := s.repo.DB.Create(&tail).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 3, SourceVideoID: "458"}).Error; err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/novel/player/video_model/v1/" {
					body := `{"code":101002}`
					if mode == "hole-unconfirmed" {
						body = `{"code":429}`
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
				}
				calls++
				ids, total := `["456","457"]`, 2
				switch mode {
				case "partial-list":
					total = 3
				case "unstable":
					if calls > 1 {
						ids = `["456","459"]`
					}
				case "reordered":
					ids = `["458","456"]`
				case "same-count-reordered":
					ids, total = `["458","457","456"]`, 3
				case "replaced", "replaced-partial":
					ids = `["777","778"]`
				case "hole", "hole-unconfirmed":
					ids, total = `["456","457",""]`, 3
				case "growth":
					ids, total = `["456","457","458","459"]`, 4
				}
				body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"123","series_name":"测试剧","vid_list":%s,"episode_cnt":%d,"episode_right_text":"全 %d 集"}}}}`, ids, total, total)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			_, err = s.latestDownloadDetail(context.Background(), row)
			replaced := mode == "reordered" || mode == "same-count-reordered" || mode == "replaced"
			if (mode == "unstable" || replaced || mode == "replaced-partial" || mode == "hole-unconfirmed") != (err != nil) {
				t.Fatalf("unexpected reconciliation: %v", err)
			}
			if replaced && !errors.Is(err, errHongGuoDownloadRemoved) {
				t.Fatalf("new version not queued: %v", err)
			}
			removed := mode == "tail" || replaced || mode == "hole"
			var count int64
			if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("id = ?", tail.ID).Count(&count).Error; err != nil || (count == 0) != removed {
				t.Fatalf("tail count=%d err=%v", count, err)
			}
			if err := s.repo.DB.Model(&model.HongGuoEpisode{}).Where("work_id = ? AND number = 3", work.ID).Count(&count).Error; err != nil || (count == 0) != (removed && mode != "same-count-reordered") {
				t.Fatalf("tail metadata count=%d err=%v", count, err)
			}
			if replaced || mode == "growth" {
				want := int64(2)
				if mode == "growth" {
					want = 4
				}
				if mode == "same-count-reordered" {
					want = 3
				}
				if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ?", row.SourceID).Count(&count).Error; err != nil || count != want {
					t.Fatalf("new queue count=%d want=%d err=%v", count, want, err)
				}
			}
			if mode == "hole" {
				if _, err := s.latestDownloadDetail(t.Context(), row); err != nil {
					t.Fatalf("removed hole blocks remaining episodes: %v", err)
				}
			}
		})
	}
}

func TestHongGuoDownloadConcurrentRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	s, first := newReconcileTestService(t)
	second := model.HongGuoDownload{SourceID: first.SourceID, Episode: 2, VideoID: "457", Title: first.Title, Root: first.Root, RelativePath: "second.mp4", Status: "queued"}
	if err := s.repo.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := s.repo.HongGuo.ClaimHongGuoDownload(t.Context())
	if err != nil || claimed == nil {
		t.Fatalf("claim second: %v", err)
	}
	ready := make(chan struct{}, 2)
	proceed := make(chan struct{})
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		status, body := 404, ""
		if strings.HasPrefix(r.URL.Path, "/search/") {
			ready <- struct{}{}
			select {
			case <-proceed:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			status, body = 200, `_ROUTER_DATA={"loaderData":{"search_(keyword)/page":{"searchList":[]}}}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	var workers sync.WaitGroup
	for _, row := range []model.HongGuoDownload{first, *claimed} {
		workers.Go(func() { s.run(ctx, row) })
	}
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("workers did not reach confirmation")
		}
	}
	close(proceed)
	workers.Wait()
	rows, count, err := s.List(t.Context(), 1)
	if err != nil || count != 0 || len(rows) != 0 {
		t.Fatalf("stale worker recreated removed downloads: %d %v", count, err)
	}
	files, err := os.ReadDir(filepath.Join(first.Root, "downloading"))
	if err != nil || len(files) != 0 {
		t.Fatalf("staging was not cleaned: %v %v", files, err)
	}
}
