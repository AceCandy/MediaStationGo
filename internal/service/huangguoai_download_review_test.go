package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHuangGuoAIManualReview(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	s := NewHuangGuoAIDownloadService(repos, NewHuangGuoAIService(repos, tasks, nil, t.TempDir()), tasks)
	ctx := context.Background()
	root := t.TempDir()
	media := filepath.Join(t.TempDir(), "sample.mp4")
	if err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "2", "-c:v", "mpeg4", media).Run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	makeCandidate := func(id string) model.HuangGuoAIDownload {
		t.Helper()
		row := model.HuangGuoAIDownload{SourceID: id, Episode: 1, Root: root, RelativePath: id + ".mp4", Status: "waiting_verify", RawSize: int64(len(data)), Duration: 99}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if id == "9008" {
			row.Duration = 2
			row.Warning = "视频时长与来源不一致，未发布"
			if err := db.Model(&row).Updates(map[string]any{"duration": row.Duration, "warning": row.Warning}).Error; err != nil {
				t.Fatal(err)
			}
		}
		row.StagingPath = filepath.Join("downloading", row.ID+"-"+uuid.NewString(), "ready.mp4")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, row.StagingPath)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, row.StagingPath), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&row).Update("staging_path", row.StagingPath).Error; err != nil {
			t.Fatal(err)
		}
		claimed, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
		if err != nil || claimed == nil || claimed.ID != row.ID {
			t.Fatal("verification claim", err)
		}
		s.run(ctx, *claimed)
		if err := db.First(&row, "id=?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != "pending_review" || row.Warning == "" || row.Error != "" || row.SHA256 == "" || row.ReviewToken == "" {
			t.Fatal("candidate not retained", row.Status)
		}
		if _, err := os.Stat(filepath.Join(root, "completed", row.RelativePath)); !os.IsNotExist(err) {
			t.Fatal("published before acceptance")
		}
		for _, claim := range []func(context.Context) (*model.HuangGuoAIDownload, error){repos.HuangGuoAI.ClaimHuangGuoAIDownload, repos.HuangGuoAI.ClaimHuangGuoAIVerification} {
			if next, err := claim(ctx); err != nil || next != nil {
				t.Fatal("review auto-claimed", err)
			}
		}
		return row
	}
	if err := verifyDownloadMedia(t.Context(), media, 2, true, false, nil, nil, false); err != nil {
		t.Fatal("warning preservation fixture must pass independent validation", err)
	}
	preserved := makeCandidate("9008")
	if preserved.Warning != "视频时长与来源不一致，未发布" {
		t.Fatal("successful repeated probe erased merge warning")
	}
	if err := s.Action(ctx, preserved.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	row := makeCandidate("9001")
	if err := s.ConfirmReview(ctx, row.ID, "stale-token", "admin"); err == nil {
		t.Fatal("stale candidate accepted")
	}
	file, err := s.ReviewFile(ctx, row.ID, row.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	changed := append([]byte(nil), data...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(filepath.Join(root, row.StagingPath), changed, 0600); err != nil {
		t.Fatal(err)
	}
	// 同长度修改不阻塞确认请求；后台发布必须复核摘要并撤销失效的确认。
	oldToken := row.ReviewToken
	if err := s.ConfirmReview(ctx, row.ID, oldToken, "admin"); err != nil {
		t.Fatal("confirmation still reads candidate content", err)
	}
	changedClaim, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || changedClaim == nil || changedClaim.Status != "publishing" {
		t.Fatal("changed candidate publish claim", err)
	}
	s.run(ctx, *changedClaim)
	var rejected model.HuangGuoAIDownload
	if err := db.First(&rejected, "id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "failed" || !strings.Contains(rejected.Error, "内容已变更") || rejected.ConfirmedAt != nil || rejected.ConfirmedBy != "" || rejected.ReviewToken != "" || rejected.SHA256 != "" {
		t.Fatal("changed candidate kept acceptance", rejected.Status, rejected.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "completed", row.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("changed candidate published")
	}
	// 模拟重新下载的候选交接，原版本不能确认新文件。
	if err := os.WriteFile(filepath.Join(root, row.StagingPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&rejected).Updates(map[string]any{"status": "waiting_verify", "raw_size": len(data)}).Error; err != nil {
		t.Fatal(err)
	}
	verification, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || verification == nil || verification.Status != "verifying" {
		t.Fatal("replacement verification claim", err)
	}
	s.run(ctx, *verification)
	row = model.HuangGuoAIDownload{}
	if err := db.First(&row, "id=?", rejected.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending_review" || row.ReviewToken == oldToken || row.ConfirmedBy != "" || row.ConfirmedAt != nil {
		t.Fatal("replacement inherited acceptance", row.Status)
	}
	if err := s.ConfirmReview(ctx, row.ID, oldToken, "admin"); err == nil {
		t.Fatal("old trial accepted replacement")
	}
	if err := s.ConfirmReview(ctx, row.ID, row.ReviewToken, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmReview(ctx, row.ID, row.ReviewToken, "admin"); err == nil {
		t.Fatal("duplicate confirmation accepted")
	}
	if _, err := s.ReviewFile(ctx, row.ID, row.ReviewToken); err == nil {
		t.Fatal("accepted candidate still previewable")
	}
	publish, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || publish == nil || publish.Status != "publishing" {
		t.Fatal("accepted publish claim", err)
	}
	s.run(ctx, *publish)
	if err := db.First(&row, "id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "completed" || row.Warning == "" || row.ConfirmedAt == nil || row.ConfirmedBy != "admin" {
		t.Fatal("acceptance audit lost", row.Status)
	}
	for i, action := range []string{"retry", "cancel"} {
		candidate := makeCandidate([]string{"9002", "9003"}[i])
		if err := s.Action(ctx, candidate.ID, action); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, candidate.StagingPath)); !os.IsNotExist(err) {
			t.Fatal("retired candidate remains")
		}
		if err := db.First(&candidate, "id=?", candidate.ID).Error; err != nil {
			t.Fatal(err)
		}
		if candidate.RawSize != 0 || candidate.SHA256 != "" || candidate.ReviewToken != "" {
			t.Fatal("retired checkpoint remains")
		}
		// Stop the queued retry so the next isolated fixture is the only claimable row.
		if action == "retry" {
			if err := s.Action(ctx, candidate.ID, "cancel"); err != nil {
				t.Fatal(err)
			}
		}
	}
	cleanup := makeCandidate("9005")
	if err := db.Model(&cleanup).Update("root", filepath.Join(root, "unavailable")).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, cleanup.ID, "cancel"); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	if err := db.First(&cleanup, "id=?", cleanup.ID).Error; err != nil || cleanup.Status != "pending_review" || cleanup.StagingPath == "" {
		t.Fatal("cleanup checkpoint lost", err)
	}
	if err := db.Model(&cleanup).Update("root", root).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, cleanup.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	symlink := makeCandidate("9006")
	stagePath := filepath.Join(root, symlink.StagingPath)
	if err := os.Remove(stagePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.mp4"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "other.mp4"), stagePath); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviewFile(ctx, symlink.ID, symlink.ReviewToken); err == nil {
		t.Fatal("symlink preview allowed")
	}
	if err := s.ConfirmReview(ctx, symlink.ID, symlink.ReviewToken, "admin"); err == nil {
		t.Fatal("symlink acceptance allowed")
	}
	if err := s.Action(ctx, symlink.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "other.mp4")); err != nil {
		t.Fatal("symlink target deleted", err)
	}
	fenced := makeCandidate("9007")
	if err := s.ConfirmReview(ctx, fenced.ID, fenced.ReviewToken, "admin"); err != nil {
		t.Fatal(err)
	}
	oldClaim, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || oldClaim == nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, fenced.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	s.run(ctx, *oldClaim)
	if _, err := os.Stat(filepath.Join(root, "completed", fenced.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("cancelled acceptance published")
	}
	if err := db.First(&fenced, "id=?", fenced.ID).Error; err != nil || fenced.Status != "cancelled" {
		t.Fatal("cancel overwritten", err)
	}
	collision := makeCandidate("9004")
	target := filepath.Join(root, "completed", collision.RelativePath)
	if err := os.WriteFile(target, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmReview(ctx, collision.ID, collision.ReviewToken, "admin"); err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	s.run(ctx, *claimed)
	if actual, err := os.ReadFile(target); err != nil || string(actual) != "existing" {
		t.Fatal("collision overwritten")
	}
	if err := db.First(&collision, "id=?", collision.ID).Error; err != nil || collision.Status != "failed" || collision.Warning == "" || collision.ConfirmedBy != "admin" || collision.ConfirmedAt == nil || !strings.Contains(collision.Error, "完成凭据不符") {
		t.Fatal("collision not retained safely", err, collision.Error)
	}
	confirmedAt := *collision.ConfirmedAt
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, collision.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	retry, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || retry == nil || retry.Status != "publishing" {
		t.Fatal("publication retry requires download or review", err)
	}
	s.run(ctx, *retry)
	if err := db.First(&collision, "id=?", collision.ID).Error; err != nil || collision.Status != "completed" || collision.Error != "" || collision.ConfirmedBy != "admin" || collision.ConfirmedAt == nil || !collision.ConfirmedAt.Equal(confirmedAt) {
		t.Fatal("publication retry lost acceptance", err)
	}
}
