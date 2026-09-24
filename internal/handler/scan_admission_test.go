package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestScanAdmissionHTTPAndBatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.LibraryRoot{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	base := t.TempDir()
	lib := model.Library{Name: "扫描测试", Path: filepath.Join(base, "a"), Type: "movie", Enabled: true}
	roots := []model.LibraryRoot{{Path: lib.Path, Enabled: true}, {Path: filepath.Join(base, "b"), Enabled: true}}
	for _, root := range roots {
		if err := os.MkdirAll(root.Path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root.Path, "Film.mkv"), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, roots); err != nil {
		t.Fatal(err)
	}
	roots, err = repos.Library.ListRoots(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	tracker := service.NewTaskTrackerService(log, nil)
	scanner := service.NewScannerService(&config.Config{}, log, repos, service.NewHub(log), nil, nil)
	scheduler := service.NewSchedulerService(log, repos, scanner, nil, nil)
	scheduler.SetTaskTracker(tracker)
	scheduler.Start(t.Context())
	defer scheduler.Stop()
	svc := &service.Container{Log: log, Repo: repos, Scan: scanner, Tasks: tracker, Scheduler: scheduler}

	finish, _ := scanner.TryBeginLocalScan()
	for _, request := range []struct {
		path    string
		body    string
		params  gin.Params
		handler gin.HandlerFunc
	}{
		{"/library/scan", "", gin.Params{{Key: "id", Value: lib.ID}}, scanLibraryHandler(svc)},
		{"/root/scan", "", gin.Params{{Key: "id", Value: lib.ID}, {Key: "root_id", Value: roots[0].ID}}, scanLibraryRootHandler(svc)},
		{"/task/run", `{"library_id":"` + lib.ID + `"}`, gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryScan}}, taskDefinitionRunHandler(svc)},
		{"/scheduler/run", "", gin.Params{{Key: "name", Value: "library_scan"}}, schedulerRunHandler(svc)},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = request.params
		c.Request = httptest.NewRequest(http.MethodPost, request.path, strings.NewReader(request.body))
		c.Request.Header.Set("Content-Type", "application/json")
		request.handler(c)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), service.ErrLocalScanAlreadyRunning.Error()) {
			t.Fatalf("busy %s: %d %s", request.path, w.Code, w.Body.String())
		}
	}
	refresh := queueSTRMRefreshAfterChanges(t.Context(), svc, base, strmRefreshQueueOptions{TaskName: "STRM", Changed: true})
	if refresh.Queued || refresh.Reason != service.ErrLocalScanAlreadyRunning.Error() {
		t.Fatalf("busy STRM refresh: %+v", refresh)
	}
	if history, err := tracker.List(1, 10); err != nil || len(history.Items) != 0 {
		t.Fatalf("busy request created executions: %+v %v", history, err)
	}
	finish()

	// 创建执行记录失败不能占住名额，也不能启动未记录的 STRM 扫描。
	svc.Tasks = nil
	if started, err := startLibraryScanTask(svc, &lib, service.TaskTriggerManual, "test"); started || !errors.Is(err, errCreateScanTask) {
		t.Fatalf("missing tracker admitted scan: %v %v", started, err)
	}
	if started, err := startLibraryRootScanTask(svc, lib.ID, roots[0].ID, lib.Name, roots[0].Path, service.TaskTriggerManual, "test"); started || !errors.Is(err, errCreateScanTask) {
		t.Fatalf("missing tracker admitted root scan: %v %v", started, err)
	}
	refresh = queueSTRMRefreshAfterChanges(t.Context(), svc, base, strmRefreshQueueOptions{TaskName: "STRM", Changed: true})
	if refresh.Queued || refresh.Reason != errCreateScanTask.Error() {
		t.Fatalf("missing tracker admitted STRM refresh: %+v", refresh)
	}
	svc.Tasks = tracker

	// 阻塞第一条写入，验证整批路径共享名额，且第二条路径没有被拒绝。
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	blocked := false
	if err := db.Callback().Create().Before("gorm:create").Register("test:block-scan", func(tx *gorm.DB) {
		if tx.Statement.Table != "media" || blocked {
			return
		}
		blocked = true
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	started, err := startLibraryRootScanTasks(svc, lib.ID, roots, lib.Name, service.TaskTriggerEvent, "新增路径自动扫描")
	if err != nil || !started {
		t.Fatalf("root batch: %v %v", started, err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("scan did not reach first write")
	}
	if finish, ok := scanner.TryBeginLocalScan(); ok {
		finish()
		t.Fatal("batch released slot before all targets finished")
	}
	close(release)
	waitScanIdle(t, scanner)
	var count int64
	if err := db.Model(&model.Media{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("root batch lost targets: count=%d err=%v", count, err)
	}
	history, _ := tracker.List(1, 10)
	if len(history.Items) != 2 {
		t.Fatalf("root executions=%d", len(history.Items))
	}
	// 不可达的第一条 STRM 目标失败后，仍处理第二条目标并最终释放名额。
	if err := repos.Library.UpdateRoot(t.Context(), &roots[0], map[string]any{"path": filepath.Join(base, "missing")}); err != nil {
		t.Fatal(err)
	}
	refresh = queueSTRMRefreshAfterChanges(t.Context(), svc, base, strmRefreshQueueOptions{TaskName: "STRM", Changed: true})
	if !refresh.Queued || len(refresh.Targets) != 2 {
		t.Fatalf("STRM targets=%+v", refresh)
	}
	waitScanIdle(t, scanner)
	history, _ = tracker.List(1, 10)
	if len(history.Items) != 4 || history.Items[0].Status != service.TaskStatusCompleted || history.Items[1].Status != service.TaskStatusFailed {
		t.Fatalf("STRM batch did not continue after error: %+v", history.Items)
	}
}

func waitScanIdle(t *testing.T, scanner *service.ScannerService) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if finish, idle := scanner.TryBeginLocalScan(); idle {
			finish()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not release slot")
		}
		time.Sleep(time.Millisecond)
	}
}
