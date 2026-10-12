package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHongGuoDownloadOriginalVideoRetry(t *testing.T) {
	for _, mode := range []string{"failed", "queued", "downloading", "waiting_verify", "verifying", "publishing", "completed", "cancelled", "missing-id", "invalid-id", "missing-episode", "retry", "bulk"} {
		t.Run(mode, func(t *testing.T) {
			s := newDownloadTestService(t)
			row := seedDownload(t, s)
			status, id := "failed", row.VideoID
			if mode == "queued" || mode == "downloading" || mode == "waiting_verify" || mode == "verifying" || mode == "publishing" || mode == "completed" || mode == "cancelled" {
				status = mode
			}
			if mode == "missing-id" {
				id = ""
			} else if mode == "invalid-id" {
				id = "invalid"
			}
			if mode == "missing-episode" {
				if err := s.repo.DB.Where("number = 1").Delete(&model.HongGuoEpisode{}).Error; err != nil {
					t.Fatal(err)
				}
			}
			original := mode == "retry" || mode == "bulk"
			if err := s.repo.DB.Model(&row).Updates(map[string]any{"status": status, "video_id": id, "error": "集号待确认", "raw_size": 5, "sha256": "checkpoint", "staging_path": "downloading/existing/ready.mp4", "use_original_video_id": original}).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			if mode == "bulk" {
				_, _, err = s.RetryFailedWork(t.Context(), row.SourceID)
			} else {
				action := "retry-original"
				if mode == "retry" {
					action = "retry"
				}
				err = s.Action(t.Context(), row.ID, action)
			}
			allowed := mode == "failed" || mode == "retry" || mode == "bulk"
			if (err == nil) != allowed {
				t.Fatalf("allowed=%v err=%v", allowed, err)
			}
			var saved model.HongGuoDownload
			if err := s.repo.DB.First(&saved, "id = ?", row.ID).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus := status
			if allowed {
				wantStatus = "queued"
			}
			if saved.Status != wantStatus || saved.VideoID != id || saved.UseOriginalVideoID != (mode == "failed") || saved.RelativePath != row.RelativePath || saved.RawSize != 5 || saved.SHA256 != "checkpoint" || saved.StagingPath != "downloading/existing/ready.mp4" || (!allowed && saved.Error != "集号待确认") {
				t.Fatalf("unexpected task mutation: %+v", saved)
			}
			encoded, err := json.Marshal(saved)
			if err != nil || strings.Contains(string(encoded), "video_id\":\"") {
				t.Fatalf("video ID leaked: %v", err)
			}
			if mode == "failed" {
				s = NewHongGuoDownloadService(s.repo, s.catalog, s.tasks)
				// 租约回收从数据库恢复手动选择，而不是依赖请求期间的内存状态。
				claimed, err := s.repo.HongGuo.ClaimHongGuoVerification(t.Context())
				if err != nil || claimed == nil || !claimed.UseOriginalVideoID || claimed.VideoID != id {
					t.Fatalf("claim lost original identity: %v", err)
				}
				if err := s.repo.DB.Model(&saved).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				reclaimed, err := s.repo.HongGuo.ClaimHongGuoVerification(t.Context())
				if err != nil || reclaimed == nil || !reclaimed.UseOriginalVideoID || reclaimed.VideoID != id || reclaimed.LeaseToken == claimed.LeaseToken {
					t.Fatalf("reclaim lost original identity: %v", err)
				}
			}
		})
	}
}
