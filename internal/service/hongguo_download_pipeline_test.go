package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHongGuoDownloadResolvesLatestEpisode(t *testing.T) {
	for _, mode := range []string{"queued", "retry", "bulk", "missing", "detail-failed"} {
		t.Run(mode, func(t *testing.T) {
			s := newDownloadTestService(t)
			seed := seedDownload(t, s)
			ctx := t.Context()
			if err := s.repo.DB.Model(&model.HongGuoEpisode{}).Where("number = 1").Update("source_video_id", "").Error; err != nil {
				t.Fatal(err)
			}
			if mode == "retry" || mode == "bulk" {
				if err := s.repo.DB.Model(&seed).Update("status", "failed").Error; err != nil {
					t.Fatal(err)
				}
				if mode == "retry" {
					if err := s.Action(ctx, seed.ID, "retry"); err != nil {
						t.Fatal(err)
					}
				} else if added, skipped, err := s.RetryFailedWork(ctx, seed.SourceID); err != nil || added != 1 || skipped != 0 {
					t.Fatalf("bulk retry: %d %d %v", added, skipped, err)
				}
			}
			details, players := 0, 0
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				status := 200
				body := `_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":"789","video_player_info":{"main_url":"https://media.example/current","duration":1}}}}`
				if r.URL.Path == "/detail" {
					details++
					ids := `["789"]`
					if mode == "missing" {
						ids = `[]`
					}
					if mode == "detail-failed" {
						status = 503
					}
					body = fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"123","series_name":"测试剧","vid_list":%s}}}}`, ids)
				} else {
					players++
					if r.URL.Path != "/player/123/789" {
						t.Errorf("used stale video: %s", r.URL.Path)
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("media")), ContentLength: 5, Header: make(http.Header), Request: r}, nil
			})}
			row, err := s.repo.HongGuo.ClaimHongGuoDownload(ctx)
			if err != nil || row == nil {
				t.Fatalf("claim: %v", err)
			}
			var downloaded, total atomic.Int64
			err = s.executeDownload(ctx, row, &downloaded, &total, nil, "official")
			if details != 1 {
				t.Fatalf("detail calls: %d", details)
			}
			if mode == "missing" || mode == "detail-failed" {
				if err == nil || errors.Is(err, errHongGuoAwaitVerification) || players != 0 {
					t.Fatalf("stale fallback: %v players=%d", err, players)
				}
				return
			}
			if !errors.Is(err, errHongGuoAwaitVerification) || players != 1 {
				t.Fatalf("transfer: %v players=%d", err, players)
			}
			var saved model.HongGuoDownload
			if err := s.repo.DB.First(&saved, "id = ?", seed.ID).Error; err != nil || saved.VideoID != "789" || saved.RawSize != 5 {
				t.Fatalf("checkpoint: %+v %v", saved, err)
			}
		})
	}
}

func finishDownloadTest(t *testing.T, s *HongGuoDownloadService, ctx context.Context, row model.HongGuoDownload) {
	t.Helper()
	s.run(ctx, row)
	for i := 0; i < 12; i++ {
		next, err := s.repo.HongGuo.ClaimHongGuoVerification(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if next == nil {
			next, err = s.repo.HongGuo.ClaimHongGuoDownload(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		if next == nil {
			return
		}
		s.run(ctx, *next)
	}
	t.Fatal("pipeline did not settle")
}

func TestHongGuoDownloadEpisodePriorityBeforePagination(t *testing.T) {
	s := newDownloadTestService(t)
	rows := make([]model.HongGuoDownload, 60)
	for i := range rows {
		rows[i] = model.HongGuoDownload{SourceID: "123", Episode: i + 1, Status: "completed"}
	}
	for i, status := range []string{"cancelled", "queued", "failed", "waiting_verify", "publishing", "verifying", "downloading"} {
		rows[53+i].Status = status
	}
	if err := s.repo.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	page, total, err := s.ListEpisodes(t.Context(), "123", 1)
	if err != nil || total != 60 || len(page) != 50 {
		t.Fatalf("page: %d %d %v", len(page), total, err)
	}
	for i, status := range []string{"downloading", "publishing", "verifying", "waiting_verify", "failed", "queued", "cancelled", "completed"} {
		if page[i].Status != status {
			t.Fatalf("position %d: %s, want %s", i, page[i].Status, status)
		}
	}
	last, _, err := s.ListEpisodes(t.Context(), "123", 2)
	if err != nil || len(last) != 10 || last[9].Episode != 53 {
		t.Fatalf("last page: %+v %v", last, err)
	}
}

func TestHongGuoDownloadSeparateVerificationAndRecovery(t *testing.T) {
	s := newDownloadTestService(t)
	seed := seedDownload(t, s)
	fixture := downloadFixture(t)
	for i := 2; i <= 6; i++ {
		row := model.HongGuoDownload{SourceID: seed.SourceID, VideoID: fmt.Sprint(455 + i), Title: seed.Title, Episode: i, Root: seed.Root, RelativePath: fmt.Sprintf("work/S01E%03d.mp4", i), Status: "queued"}
		if i >= 4 {
			row.Status, row.RawSize, row.Source, row.SourceTries, row.Encrypted, row.Duration = "waiting_verify", int64(len(fixture)), "official", 1, true, 1
			stage := filepath.Join("downloading", fmt.Sprint(i))
			row.StagingPath = filepath.Join(stage, "ready.mp4")
			if err := os.MkdirAll(filepath.Join(seed.Root, stage), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(seed.Root, stage, "source.bin"), fixture, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.repo.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		var work model.HongGuoWork
		if err := s.repo.DB.First(&work, "source_id = ?", seed.SourceID).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: i, SourceVideoID: row.VideoID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	transfers, verifications := make(chan string, 6), make(chan string, 6)
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if response := downloadDetailTestResponse(r); response != nil {
			return response, nil
		}
		id := filepath.Base(r.URL.Path)
		if id >= "459" {
			verifications <- id
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":%q,"video_player_info":{"main_url":"https://media.example/%s","duration":1}}}}`, id, id)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		transfers <- r.URL.Path
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	t.Cleanup(func() { cancel(); s.Wait() })
	for channel, count := range map[chan string]int{transfers: 3, verifications: 2} {
		for i := 0; i < count; i++ {
			select {
			case <-channel:
			case <-time.After(10 * time.Second):
				t.Fatal("independent slot did not start")
			}
		}
	}
	select {
	case <-verifications:
		t.Fatal("more than two verifications")
	case <-time.After(100 * time.Millisecond):
	}
	var waiting int64
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("status = ?", "waiting_verify").Count(&waiting).Error; err != nil || waiting != 1 {
		t.Fatalf("waiting=%d err=%v", waiting, err)
	}
	// 缩容不能中断现有校验，也不能领取等待项；扩容无需重启即可填充名额。
	verification := 1
	if _, err := s.SaveConfig(t.Context(), seed.Root, HongGuoDownloadConfigPatch{VerificationConcurrency: &verification}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-verifications:
		t.Fatal("verification started above reduced limit")
	case <-time.After(2500 * time.Millisecond):
	}
	var running int64
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("status = ?", "verifying").Count(&running).Error; err != nil || running != 2 {
		t.Fatalf("shrink interrupted running verifiers: %d %v", running, err)
	}
	verification = 3
	if _, err := s.SaveConfig(t.Context(), seed.Root, HongGuoDownloadConfigPatch{VerificationConcurrency: &verification}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-verifications:
	case <-time.After(10 * time.Second):
		t.Fatal("verification increase required restart")
	}
	var old model.HongGuoDownload
	if err := s.repo.DB.Where("status = ?", "verifying").Order("episode").First(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Action(t.Context(), old.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&model.HongGuoEpisode{}).Where("number = ?", old.Episode).Update("source_video_id", "999").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Action(t.Context(), old.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-verifications:
		if id != old.VideoID {
			t.Fatal("retried verification changed identity")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("verification retry did not start")
	}
	var renewed model.HongGuoDownload
	if err := s.repo.DB.First(&renewed, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if renewed.Status != "verifying" || renewed.LeaseToken == old.LeaseToken || renewed.StagingPath == old.StagingPath {
		t.Fatal("verification reused old execution/stage")
	}
	if err := s.repo.HongGuo.UpdateHongGuoDownload(t.Context(), old.ID, old.LeaseToken, map[string]any{"status": "failed"}); err == nil {
		t.Fatal("stale verifier overwrote retry")
	}
	cancel()
	s.Wait()
	var rows []model.HongGuoDownload
	if err := s.repo.DB.Order("episode").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.LeaseToken != "" || row.Status != "queued" && row.Status != "waiting_verify" {
			t.Fatalf("shutdown: %+v", row)
		}
		if row.Episode >= 4 {
			if _, err := os.Stat(filepath.Join(row.Root, filepath.Dir(row.StagingPath), "source.bin")); err != nil {
				t.Fatal("staged bytes lost", err)
			}
		}
	}
	// 恢复使用持久化暂存信息；取消等待校验任务不能被领取或发布。
	if err := s.Action(t.Context(), rows[5].ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("episode < 4").Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("episode >= 4").Update("encrypted", false).Error; err != nil {
		t.Fatal(err)
	}
	restarted := NewHongGuoDownloadService(s.repo, s.catalog, s.tasks)
	restarted.http = &http.Client{Transport: hongGuoTestTransport(func(*http.Request) (*http.Response, error) {
		t.Error("recovery downloaded again")
		return nil, context.Canceled
	})}
	for i := 0; i < 2; i++ {
		row, err := restarted.repo.HongGuo.ClaimHongGuoVerification(t.Context())
		if err != nil || row == nil {
			t.Fatalf("reclaim: %v", err)
		}
		restarted.run(t.Context(), *row)
		var saved model.HongGuoDownload
		if err := s.repo.DB.First(&saved, "id = ?", row.ID).Error; err != nil || saved.Status != "completed" {
			t.Fatalf("recovered: %s %s %v", saved.Status, saved.Error, err)
		}
		data, err := os.ReadFile(filepath.Join(row.Root, "completed", row.RelativePath))
		if err != nil || len(data) == 0 || bytes.Equal(data, []byte("bad")) {
			t.Fatal("no valid output", err)
		}
	}
	if row, err := restarted.repo.HongGuo.ClaimHongGuoVerification(t.Context()); err != nil || row != nil {
		t.Fatalf("cancelled claimed: %v %v", row, err)
	}
}
