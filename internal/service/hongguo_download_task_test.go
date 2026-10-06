package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

func enableDownloadTaskPersistence(t *testing.T, s *HongGuoDownloadService) {
	t.Helper()
	if err := s.repo.DB.AutoMigrate(&model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	s.tasks.ConfigurePersistence(s.repo.TaskExecution, t.TempDir())
}

func readDownloadWorkTask(t *testing.T, s *HongGuoDownloadService, sourceID string) BackgroundTask {
	t.Helper()
	var row model.TaskExecution
	if err := s.repo.DB.First(&row, "id = ?", repository.HongGuoDownloadTaskID(sourceID)).Error; err != nil {
		t.Fatal(err)
	}
	if row.SourcePath != "hongguo://"+sourceID {
		t.Fatalf("missing work association: %+v", row)
	}
	return backgroundFromTaskExecution(row)
}

func TestHongGuoDownloadWorkTaskLifecycle(t *testing.T) {
	s := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, s)
	seed := seedDownload(t, s)
	ctx := t.Context()
	first := readDownloadWorkTask(t, s, seed.SourceID)
	if first.Status != TaskStatusRunning || first.Metrics["total"] != 1 || first.FinishedAt != nil {
		t.Fatalf("queued summary: %+v", first)
	}
	var work model.HongGuoWork
	if err := s.repo.DB.First(&work, "source_id = ?", seed.SourceID).Error; err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 6; i++ {
		if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: i, SourceVideoID: "789"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Enqueue(ctx, seed.SourceID); err != nil || n != 5 {
		t.Fatalf("supplement: %d %v", n, err)
	}
	for i, state := range []string{"completed", "waiting_verify", "verifying", "publishing", "failed", "cancelled"} {
		if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ? AND episode = ?", seed.SourceID, i+1).Update("status", state).Error; err != nil {
			t.Fatal(err)
		}
	}
	s.refreshWorkTask(ctx, seed.SourceID)
	got := readDownloadWorkTask(t, s, seed.SourceID)
	if got.ID != first.ID || !got.StartedAt.Equal(first.StartedAt) || got.Status != TaskStatusRunning ||
		got.Metrics["total"] != 6 || got.Metrics["completed"] != 1 || got.Metrics["remaining"] != 3 ||
		got.Metrics["failed"] != 1 || got.Metrics["cancelled"] != 1 {
		t.Fatalf("mixed state: %+v", got)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ? AND status IN ?", seed.SourceID, []string{"waiting_verify", "verifying", "publishing"}).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(ctx, seed.SourceID)
	got = readDownloadWorkTask(t, s, seed.SourceID)
	if got.Status != TaskStatusFailed || got.FinishedAt == nil {
		t.Fatalf("terminal failure: %+v", got)
	}
	if n, skipped, err := s.RetryFailedWork(ctx, seed.SourceID); err != nil || n != 1 || skipped != 0 {
		t.Fatalf("retry: %d %d %v", n, skipped, err)
	}
	got = readDownloadWorkTask(t, s, seed.SourceID)
	if got.ID != first.ID || got.Status != TaskStatusRunning || got.FinishedAt != nil {
		t.Fatalf("resumed task: %+v", got)
	}
	var cancelled model.HongGuoDownload
	if err := s.repo.DB.First(&cancelled, "source_id = ? AND episode = 6", seed.SourceID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, cancelled.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, cancelled.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ? AND status = 'queued'", seed.SourceID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(ctx, seed.SourceID)
	if got := readDownloadWorkTask(t, s, seed.SourceID); got.Status != TaskStatusInterrupted || got.FinishedAt == nil {
		t.Fatalf("cancelled work: %+v", got)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ?", seed.SourceID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(ctx, seed.SourceID)
	if got := readDownloadWorkTask(t, s, seed.SourceID); got.Status != TaskStatusCompleted || got.Metrics["completed"] != 6 {
		t.Fatalf("completed work: %+v", got)
	}
	// 重启恢复只修复已有作品摘要，并清除等待队列的终态时间。
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("id = ?", seed.ID).Update("status", "queued").Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(ctx, seed.SourceID)
	if err := s.tasks.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.recoverWorkTasks(ctx)
	if got := readDownloadWorkTask(t, s, seed.SourceID); got.ID != first.ID || got.Status != TaskStatusRunning || got.FinishedAt != nil {
		t.Fatalf("restart: %+v", got)
	}
	var count int64
	if err := s.repo.DB.Model(&model.TaskExecution{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("executions: %d %v", count, err)
	}
}

func TestHongGuoDownloadWorkTaskConcurrentRefresh(t *testing.T) {
	s := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, s)
	seedDownload(t, s)
	if err := s.repo.DB.Create(&model.HongGuoDownload{SourceID: "999", Episode: 1, Title: "另一部", Status: "failed"}).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, id := range []string{"123", "999"} {
				if _, err := s.repo.HongGuo.RefreshHongGuoDownloadTask(t.Context(), id); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	var count int64
	if err := s.repo.DB.Model(&model.TaskExecution{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("unique summaries: %d %v", count, err)
	}
	if got := readDownloadWorkTask(t, s, "999"); got.Status != TaskStatusFailed || got.Metrics["failed"] != 1 {
		t.Fatalf("work isolation: %+v", got)
	}
	// 阻塞旧执行者的摘要写入，期间提交新的队列状态，解锁后必须读取新快照。
	locked := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- s.repo.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "hongguo_download:123").Error; err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	refreshed := make(chan error, 1)
	go func() { _, err := s.repo.HongGuo.RefreshHongGuoDownloadTask(t.Context(), "123"); refreshed <- err }()
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = '123'").Update("status", "completed").Error; err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	if err := <-refreshed; err != nil {
		t.Fatal(err)
	}
	if got := readDownloadWorkTask(t, s, "123"); got.Status != TaskStatusCompleted {
		t.Fatalf("stale state: %+v", got)
	}
}

func TestHongGuoDownloadWorkTaskRecoveryAfterQueueCommit(t *testing.T) {
	s := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, s)
	seed := seedDownload(t, s)
	// 模拟业务终态已提交、执行者还没来得及刷新摘要就退出。
	if err := s.repo.DB.Model(&seed).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if got := readDownloadWorkTask(t, s, seed.SourceID); got.Status != TaskStatusRunning {
		t.Fatal("fixture did not leave stale running summary")
	}
	restarted := NewHongGuoDownloadService(s.repo, s.catalog, s.tasks)
	restarted.recoverWorkTasks(t.Context())
	if got := readDownloadWorkTask(t, s, seed.SourceID); got.Status != TaskStatusCompleted || got.FinishedAt == nil {
		t.Fatalf("terminal queue was not reconciled on restart: %+v", got)
	}
}

func TestHongGuoDownloadSummaryFailureKeepsPublicationAndLogs(t *testing.T) {
	s := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, s)
	seed := seedDownload(t, s)
	data := downloadFixture(t)
	stage := filepath.Join("downloading", "verified.mp4")
	if err := os.WriteFile(filepath.Join(seed.Root, stage), data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := s.repo.DB.Model(&seed).Updates(map[string]any{"sha256": hex.EncodeToString(sum[:]), "verified_size": len(data), "staging_path": stage}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Migrator().DropTable(&model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	row, err := s.repo.HongGuo.ClaimHongGuoVerification(t.Context())
	if err != nil || row == nil {
		t.Fatalf("claim: %v", err)
	}
	s.run(t.Context(), *row)
	if err := s.repo.DB.First(&seed, "id = ?", seed.ID).Error; err != nil || seed.Status != "completed" {
		t.Fatalf("summary failure stopped publication: %s %v", seed.Status, err)
	}
	if _, err := os.Stat(filepath.Join(seed.Root, "completed", seed.RelativePath)); err != nil {
		t.Fatal(err)
	}
	log, err := s.tasks.ReadDefinitionLog(TaskKindHongGuoDownload, "", 10000)
	if err != nil || !strings.Contains(log.Content, "测试剧 E001") || !strings.Contains(log.Content, "文件已完成并发布") {
		t.Fatalf("missing episode diagnostics: %v %s", err, log.Content)
	}
	if snapshot := s.tasks.memorySnapshot(); len(snapshot.Active) != 0 || len(snapshot.Recent) != 0 {
		t.Fatalf("episode leaked into tracker: %+v", snapshot)
	}
}

func TestHongGuoDownloadRemovedWorkTask(t *testing.T) {
	s := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, s)
	seed := seedDownload(t, s)
	if err := s.repo.DB.Delete(&seed).Error; err != nil {
		t.Fatal(err)
	}
	s.refreshWorkTask(context.Background(), seed.SourceID)
	got := readDownloadWorkTask(t, s, seed.SourceID)
	if got.Status != TaskStatusInterrupted || got.Stage != "removed" || got.FinishedAt == nil {
		t.Fatalf("removed work: %+v", got)
	}
	if _, err := s.repo.HongGuo.RefreshHongGuoDownloadTask(t.Context(), "999"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := s.repo.DB.Model(&model.TaskExecution{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 || got.FinishedAt.After(time.Now()) {
		t.Fatalf("empty work created summary: %d", count)
	}
}

func TestHongGuoDownloadLegacyTaskCompaction(t *testing.T) {
	s := newDownloadTestService(t)
	enableDownloadTaskPersistence(t, s)
	seed := seedDownload(t, s)
	cutoff := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	old := cutoff.Add(-time.Hour)
	end := old.Add(time.Minute)
	makeLegacy := func(sourceID, status, stage string) model.TaskExecution {
		return model.TaskExecution{
			Base: model.Base{CreatedAt: old, UpdatedAt: end},
			Kind: TaskKindHongGuoDownload, Trigger: TaskTriggerManual,
			Name: "红果下载：历史剧 E001", Status: status, Stage: stage,
			DestPath:  "2026/历史剧 [hongguo-" + sourceID + "]/Season 01/S01E001.mp4",
			StartedAt: old, FinishedAt: &end,
		}
	}
	first := makeLegacy(seed.SourceID, TaskStatusCompleted, "waiting_verify")
	second := makeLegacy(seed.SourceID, TaskStatusFailed, "verifying")
	removedQueue := makeLegacy("999", TaskStatusCompleted, "waiting_verify")
	running := makeLegacy(seed.SourceID, TaskStatusRunning, "downloading")
	running.FinishedAt = nil
	newer := makeLegacy(seed.SourceID, TaskStatusCompleted, "completed")
	newer.CreatedAt = cutoff.Add(time.Minute)
	unrecognized := makeLegacy("888", TaskStatusCompleted, "completed")
	unrecognized.DestPath = "unrecognized/S01E001.mp4"
	other := makeLegacy(seed.SourceID, TaskStatusCompleted, "completed")
	other.Kind = TaskKindScrape
	finishedAfterCutoff := makeLegacy(seed.SourceID, TaskStatusCompleted, "completed")
	late := cutoff.Add(time.Minute)
	finishedAfterCutoff.FinishedAt = &late
	rows := []*model.TaskExecution{&first, &second, &removedQueue, &running, &newer, &unrecognized, &other, &finishedAfterCutoff}
	for _, row := range rows {
		if err := s.repo.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	groups, err := s.repo.HongGuo.LegacyHongGuoDownloadTaskGroups(t.Context(), cutoff)
	if err != nil || len(groups) != 2 || len(groups[seed.SourceID]) != 2 || len(groups["999"]) != 1 {
		t.Fatalf("legacy groups: %+v %v", groups, err)
	}
	var deleted int64
	for id, ids := range groups {
		n, err := s.repo.HongGuo.CompactHongGuoDownloadTasks(t.Context(), id, cutoff, ids)
		if err != nil {
			t.Fatal(err)
		}
		deleted += n
		if n, err := s.repo.HongGuo.CompactHongGuoDownloadTasks(t.Context(), id, cutoff, ids); err != nil || n != 0 {
			t.Fatalf("not repeatable: %d %v", n, err)
		}
	}
	if deleted != 3 {
		t.Fatalf("deleted %d, want 3", deleted)
	}
	got := readDownloadWorkTask(t, s, seed.SourceID)
	if got.Status != TaskStatusRunning || got.Metrics["remaining"] != 1 || got.Metrics["completed"] != 0 || !got.StartedAt.Equal(old) {
		t.Fatalf("history overrode queue: %+v", got)
	}
	got = readDownloadWorkTask(t, s, "999")
	if got.Status != TaskStatusInterrupted || got.Stage != "legacy" || strings.Contains(got.Name, "E001") {
		t.Fatalf("transfer-stage success fabricated completion: %+v", got)
	}
	for _, row := range rows[3:] {
		var saved model.TaskExecution
		if err := s.repo.DB.First(&saved, "id = ?", row.ID).Error; err != nil || saved.Status != row.Status {
			t.Fatalf("protected history changed: %s %v", row.ID, err)
		}
	}
	var after model.HongGuoDownload
	if err := s.repo.DB.First(&after, "id = ?", seed.ID).Error; err != nil || after.Status != seed.Status || after.RelativePath != seed.RelativePath {
		t.Fatalf("business queue changed: %+v %v", after, err)
	}
	// 删除失败须连同新作品摘要一起回滚。
	failing := makeLegacy("777", TaskStatusCompleted, "completed")
	if err := s.repo.DB.Create(&failing).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Exec(`CREATE FUNCTION reject_task_compaction() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN RAISE EXCEPTION 'test delete rejection'; END $$;
	CREATE TRIGGER reject_task_compaction BEFORE DELETE ON task_executions FOR EACH ROW EXECUTE FUNCTION reject_task_compaction()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.HongGuo.CompactHongGuoDownloadTasks(t.Context(), "777", cutoff, []string{failing.ID}); err == nil {
		t.Fatal("delete failure accepted")
	}
	var count int64
	if err := s.repo.DB.Model(&model.TaskExecution{}).Where("id = ?", repository.HongGuoDownloadTaskID("777")).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed compaction left summary: %d %v", count, err)
	}
	if err := s.repo.DB.First(&failing, "id = ?", failing.ID).Error; err != nil {
		t.Fatal("failed compaction deleted history")
	}
}
