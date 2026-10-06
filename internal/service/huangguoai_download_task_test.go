package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/google/uuid"
)

func newHuangGuoDownloadTaskTestService(t *testing.T) *HuangGuoAIDownloadService {
	t.Helper()
	red := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, red)
	if err := red.repo.DB.AutoMigrate(&model.HuangGuoAIWork{}, &model.HuangGuoAIEpisode{}, &model.HuangGuoAIDownloadWork{}, &model.HuangGuoAIDownload{}); err != nil {
		t.Fatal(err)
	}
	catalog := NewHuangGuoAIService(red.repo, red.tasks, nil, t.TempDir())
	s := NewHuangGuoAIDownloadService(red.repo, catalog, red.tasks)
	if _, err := s.SaveConfig(t.Context(), HuangGuoAIDownloadConfig{Root: t.TempDir(), Concurrency: 1, VerificationConcurrency: 1}); err != nil {
		t.Fatal(err)
	}
	return s
}

func readHuangGuoDownloadTask(t *testing.T, s *HuangGuoAIDownloadService, id string) BackgroundTask {
	t.Helper()
	var row model.TaskExecution
	if err := s.repo.DB.First(&row, "id = ?", repository.HuangGuoAIDownloadTaskID(id)).Error; err != nil {
		t.Fatal(err)
	}
	if row.System != model.TaskSystemHuangGuoAI || row.SourcePath != "huangguoai://"+id {
		t.Fatalf("identity: %+v", row)
	}
	if strings.Contains(row.Name, "private-title") || row.DestPath != "" {
		t.Fatalf("unsafe summary: %+v", row)
	}
	return backgroundFromTaskExecution(row)
}

func TestHuangGuoAIDownloadWorkTaskLifecycle(t *testing.T) {
	s := newHuangGuoDownloadTaskTestService(t)
	db, ctx := s.repo.DB, t.Context()
	for _, w := range []model.HuangGuoAIWork{
		{SourceID: "123", SourceCategory: "ai-duanju", Kind: model.MetadataKindSeries, Title: "private-title"},
		{SourceID: "124", SourceCategory: "ai-mogai", Kind: model.MetadataKindMovie, Title: "private-title"},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: w.ID, Number: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if n, err := s.Enqueue(ctx, w.SourceID); err != nil || n != 1 {
			t.Fatalf("enqueue: %d %v", n, err)
		}
	}
	first := readHuangGuoDownloadTask(t, s, "123")
	if movie := readHuangGuoDownloadTask(t, s, "124"); movie.Metrics["total"] != 1 || movie.Status != TaskStatusRunning {
		t.Fatalf("movie summary: %+v", movie)
	}
	var work model.HuangGuoAIWork
	if err := db.First(&work, "source_id = ?", "123").Error; err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= 6; n++ {
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: work.ID, Number: n}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Enqueue(ctx, "123"); err != nil || n != 5 {
		t.Fatalf("supplement: %d %v", n, err)
	}
	for n, state := range []string{"completed", "waiting_verify", "verifying", "publishing", "failed", "cancelled"} {
		if err := db.Model(&model.HuangGuoAIDownload{}).Where("source_id = ? AND episode = ?", "123", n+1).Update("status", state).Error; err != nil {
			t.Fatal(err)
		}
	}
	s.refreshWorkTask(ctx, "123")
	got := readHuangGuoDownloadTask(t, s, "123")
	if got.ID != first.ID || !got.StartedAt.Equal(first.StartedAt) || got.Status != TaskStatusRunning || got.Metrics["total"] != 6 || got.Metrics["remaining"] != 3 || got.Metrics["failed"] != 1 {
		t.Fatalf("mixed: %+v", got)
	}
	if err := db.Model(&model.HuangGuoAIDownload{}).Where("source_id = ? AND status IN ?", "123", []string{"waiting_verify", "verifying", "publishing"}).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(ctx, "123")
	if got := readHuangGuoDownloadTask(t, s, "123"); got.Status != TaskStatusFailed || got.FinishedAt == nil {
		t.Fatalf("failure: %+v", got)
	}
	if n, err := s.WorkAction(ctx, "123", "retry"); err != nil || n != 2 {
		t.Fatalf("retry: %d %v", n, err)
	}
	got = readHuangGuoDownloadTask(t, s, "123")
	if got.ID != first.ID || got.FinishedAt != nil || got.Status != TaskStatusRunning {
		t.Fatalf("retry summary: %+v", got)
	}
	var episode model.HuangGuoAIDownload
	if err := db.First(&episode, "source_id = ? AND episode = 6", "123").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, episode.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.WorkAction(ctx, "123", "cancel"); err != nil || n != 2 {
		t.Fatalf("cancel: %d %v", n, err)
	}
	if got := readHuangGuoDownloadTask(t, s, "123"); got.Status != TaskStatusInterrupted {
		t.Fatalf("cancel summary: %+v", got)
	}
	if err := s.Action(ctx, episode.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	// 模拟队列完成事务已提交而摘要刷新前进程退出。
	if err := db.Model(&model.HuangGuoAIDownload{}).Where("source_id IN ?", []string{"123", "124"}).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.tasks.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.recoverWorkTasks(ctx)
	for _, id := range []string{"123", "124"} {
		if got := readHuangGuoDownloadTask(t, s, id); got.Status != TaskStatusCompleted || got.FinishedAt == nil {
			t.Fatalf("recovery: %+v", got)
		}
	}
	// 两套体系即使来源 ID 相同，也必须各有独立摘要。
	if err := db.Create(&model.HongGuoDownload{SourceID: "123", Episode: 1, Status: "queued"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.HongGuo.RefreshHongGuoDownloadTask(ctx, "123"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.repo.HuangGuoAI.RefreshHuangGuoAIDownloadTask(ctx, "123")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.TaskExecution{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("unique identity: %d %v", count, err)
	}
	if repository.HongGuoDownloadTaskID("123") == first.ID {
		t.Fatal("cross-system ID collision")
	}
	if err := db.Where("source_id = ?", "124").Delete(&model.HuangGuoAIDownload{}).Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(ctx, "124")
	if got := readHuangGuoDownloadTask(t, s, "124"); got.Stage != "removed" || got.Status != TaskStatusInterrupted {
		t.Fatalf("removed: %+v", got)
	}
}

func TestHuangGuoAIDownloadSummaryFailureKeepsPublicationAndLogs(t *testing.T) {
	s := newHuangGuoDownloadTaskTestService(t)
	cfg, err := s.Config(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("verified fixture")
	sum := sha256.Sum256(data)
	row := model.HuangGuoAIDownload{SourceID: "125", Episode: 1, Title: "private-title", Root: cfg.Root, RelativePath: "private-title/Season 01/[huangguoai-125] S01E001.mp4", Status: "queued", SHA256: hex.EncodeToString(sum[:]), VerifiedSize: int64(len(data)), RawSize: int64(len(data))}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join("downloading", row.ID+"-"+uuid.NewString(), "ready.mp4")
	if err := os.MkdirAll(filepath.Join(cfg.Root, filepath.Dir(stage)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Root, stage), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&row).Update("staging_path", stage).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Migrator().DropTable(&model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	lease, err := s.repo.HuangGuoAI.ClaimHuangGuoAIVerification(t.Context())
	if err != nil || lease == nil {
		t.Fatalf("claim: %v", err)
	}
	s.run(context.Background(), *lease)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil || row.Status != "completed" {
		t.Fatalf("publication: %s %v", row.Status, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Root, "completed", row.RelativePath)); err != nil {
		t.Fatal(err)
	}
	log, err := s.tasks.ReadDefinitionLog(TaskKindHuangGuoAIDownload, "", 10000)
	if err != nil || !strings.Contains(log.Content, "125 第 1 集") || !strings.Contains(log.Content, "文件已完成并发布") || strings.Contains(log.Content, "private-title") {
		t.Fatalf("safe logs: %v %s", err, log.Content)
	}
	// 上游异常可能带签名 URL 和敏感信息，日志只能保留固定安全错误。
	s.catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("private-title https://example.com/private?token=secret-value")
	})})
	failed := model.HuangGuoAIDownload{SourceID: "126", Episode: 1, Title: "private-title", Root: cfg.Root, RelativePath: "safe/Season 01/[huangguoai-126] S01E001.mp4", Status: "queued"}
	if err := s.repo.DB.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}
	transfer, err := s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(t.Context())
	if err != nil || transfer == nil {
		t.Fatalf("transfer claim: %v", err)
	}
	s.run(t.Context(), *transfer)
	if err := s.repo.DB.First(&failed, "id = ?", failed.ID).Error; err != nil || failed.Status != "failed" {
		t.Fatalf("failed transfer: %s %v", failed.Status, err)
	}
	log, err = s.tasks.ReadDefinitionLog(TaskKindHuangGuoAIDownload, "", 10000)
	if err != nil || !strings.Contains(log.Content, "黄果 AI 下载未完成") || strings.Contains(log.Content, "private-title") || strings.Contains(log.Content, "secret-value") || strings.Contains(log.Content, "https://") {
		t.Fatalf("upstream error leaked: %v %s", err, log.Content)
	}
	if snapshot := s.tasks.memorySnapshot(); len(snapshot.Active) != 0 || len(snapshot.Recent) != 0 {
		t.Fatalf("episode tracker entry: %+v", snapshot)
	}
}
