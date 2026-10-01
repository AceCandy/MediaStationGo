package service

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestScannerProbeScopeKeepsAllCommittedChanges(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Type: "movie", Path: root, Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "old.mkv")
	writeOrgFile(t, oldPath, "old media")
	oldResult, err := scanner.IngestPathResult(t.Context(), lib.ID, oldPath)
	if err != nil || len(oldResult.probeMediaIDs) != 1 {
		t.Fatalf("old ingest: result=%+v error=%v", oldResult, err)
	}
	oldID := oldResult.probeMediaIDs[0]
	const files = maxScanChangeDetails + 3
	for i := range files {
		writeOrgFile(t, filepath.Join(root, fmt.Sprintf("movie-%03d.mkv", i)), "media")
	}
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.ErrorCount != 0 || res.Added != files || len(res.probeMediaIDs) != files || res.OmittedChanges != 3 || slices.Contains(res.probeMediaIDs, oldID) {
		t.Fatalf("scan: result=%+v error=%v", res, err)
	}
	probe := NewMediaProbeService(repos, &stubMediaProbeRunner{result: probeResultFixture()})
	pending, err := probe.hasPendingProbe(t.Context(), res.probeMediaIDs)
	if err != nil || !pending {
		t.Fatalf("scoped pending=%v error=%v", pending, err)
	}
	result, err := probe.backfill(t.Context(), "", 0, nil, true, res.probeMediaIDs)
	if err != nil || result.Total != files || result.Completed != files || result.Failed != 0 {
		t.Fatalf("scoped backfill=%+v error=%v", result, err)
	}
	if _, ok := probe.Load(t.Context(), oldID); ok {
		t.Fatal("event backfill included unrelated old pending media")
	}
	pending, err = probe.hasPendingProbe(t.Context(), res.probeMediaIDs)
	if err != nil || pending {
		t.Fatalf("completed scope pending=%v error=%v", pending, err)
	}
	pending, err = probe.hasPendingProbe(t.Context(), []string{})
	if err != nil || pending {
		t.Fatalf("empty scope must not include old pending media: pending=%v error=%v", pending, err)
	}
	// 全库批量更新与单文件立即更新都必须交接真实的既有 ID。
	path := filepath.Join(root, "movie-000.mkv")
	media := loadMediaByPath(t, repos, path)
	writeOrgFile(t, path, "changed video")
	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.Updated != 1 || !slices.Equal(res.probeMediaIDs, []string{media.ID}) {
		t.Fatalf("updated batch: result=%+v error=%v", res, err)
	}
	writeOrgFile(t, path, "changed video again")
	res, err = scanner.IngestPathResult(t.Context(), lib.ID, path)
	if err != nil || res.Updated != 1 || !slices.Equal(res.probeMediaIDs, []string{media.ID}) {
		t.Fatalf("updated event: result=%+v error=%v", res, err)
	}
}

func TestScannerEpisodicLibrariesSkipProbeAtIngest(t *testing.T) {
	for _, kind := range []string{" TV ", "anime", "variety", "show", "shows", model.LibraryTypeNFOTV} {
		t.Run(kind, func(t *testing.T) {
			scanner, repos := newScannerTestEnv(t)
			if err := repos.DB.AutoMigrate(&model.LibraryRoot{}, &model.NFOItem{}, &model.NFOMediaBinding{}); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			lib := model.Library{Name: "Episodes", Type: kind, Path: root, Enabled: true}
			if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "unnumbered.mkv")
			writeOrgFile(t, path, "video")
			if kind == model.LibraryTypeNFOTV {
				writeOrgFile(t, nfoPath(path), `<episodedetails><title>Episode</title><season>1</season><episode>1</episode></episodedetails>`)
			}
			tracker := NewTaskTrackerService(zap.NewNop(), nil)
			probe := NewMediaProbeService(repos, &stubMediaProbeRunner{result: probeResultFixture()}).SetTaskTracker(zap.NewNop(), tracker, t.Context())
			scanner.SetMediaProbe(probe)
			check := func(res *ScanResult, err error) {
				t.Helper()
				if err != nil || res == nil || res.ErrorCount != 0 || res.Added+res.Updated != 1 || len(res.probeMediaIDs) != 0 {
					t.Fatalf("episodic ingest: result=%+v error=%v", res, err)
				}
				scanner.WakeProbeBackfill(res)
				probe.autoMu.Lock()
				running := probe.autoRunning
				probe.autoMu.Unlock()
				if running || len(tracker.Snapshot().Recent) != 0 {
					t.Fatal("episodic ingest started automatic probe checks")
				}
			}
			res, err := scanner.IngestPathResult(t.Context(), lib.ID, path)
			check(res, err)
			writeOrgFile(t, path, "changed video")
			res, err = scanner.ScanLibrary(t.Context(), lib.ID)
			check(res, err)
			roots, err := repos.Library.ListRoots(t.Context(), lib.ID)
			if err != nil || len(roots) != 1 {
				t.Fatalf("roots=%d error=%v", len(roots), err)
			}
			writeOrgFile(t, path, "changed video again")
			res, err = scanner.ScanLibraryRoot(t.Context(), lib.ID, roots[0].ID)
			check(res, err)
		})
	}
}

func TestMediaProbeScopedWakeWaitsAndPreservesLaterEvents(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Type: "movie", Path: root, Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 3)
	for i := range ids {
		path := filepath.Join(root, fmt.Sprintf("movie-%d.mkv", i))
		writeOrgFile(t, path, "media")
		res, err := scanner.IngestPathResult(t.Context(), lib.ID, path)
		if err != nil || len(res.probeMediaIDs) != 1 {
			t.Fatalf("ingest: result=%+v error=%v", res, err)
		}
		ids[i] = res.probeMediaIDs[0]
	}
	started := make(chan string, 3)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	runner := &stubMediaProbeRunner{probeFunc: func(path string) (*ProbeResult, error) {
		started <- filepath.Base(path)
		if filepath.Base(path) == "movie-0.mkv" {
			<-release
		}
		return probeResultFixture(), nil
	}}
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	probe := NewMediaProbeService(repos, runner).SetTaskTracker(zap.NewNop(), tracker, t.Context())
	scanner.SetMediaProbe(probe)
	// 待办检查刚结束时插入手动任务，复现检查与任务占用之间的竞争。
	competing := make(chan *TaskHandle, 1)
	var raced atomic.Bool
	if err := repos.DB.Callback().Row().After("gorm:row").Register("test:probe-admission-race", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "current_probe") && raced.CompareAndSwap(false, true) {
			competing <- tracker.StartTriggeredIfKindIdle(TaskKindProbe, TaskTriggerManual, "手动竞争", TaskUpdate{})
		}
	}); err != nil {
		t.Fatal(err)
	}
	manual := tracker.StartTriggeredIfKindIdle(TaskKindProbe, TaskTriggerManual, "手动回填", TaskUpdate{})
	if manual == nil {
		t.Fatal("manual probe task did not start")
	}
	probe.WakeMediaBackfill(nil)
	probe.WakeMediaBackfill([]string{"", " "})
	probe.autoMu.Lock()
	emptyRunning := probe.autoRunning
	probe.autoMu.Unlock()
	if emptyRunning {
		t.Fatal("empty event scope started a full-library check")
	}
	scanner.WakeProbeBackfill(&ScanResult{probeMediaIDs: []string{ids[0], ids[0]}})
	select {
	case <-started:
		t.Fatal("automatic probe ran alongside a manual task")
	case <-time.After(150 * time.Millisecond):
	}
	manual.Finish(nil, TaskUpdate{})
	select {
	case task := <-competing:
		if task == nil {
			t.Fatal("could not reproduce task-admission race")
		}
		select {
		case <-started:
			t.Fatal("automatic probe overlapped the competing manual task")
		case <-time.After(150 * time.Millisecond):
		}
		task.Finish(nil, TaskUpdate{})
	case <-time.After(3 * time.Second):
		t.Fatal("automatic pending check did not resume after manual task")
	}
	select {
	case path := <-started:
		if path != "movie-0.mkv" {
			t.Fatalf("first event probed %q", path)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scoped event did not run after manual task")
	}
	scanner.WakeProbeBackfill(&ScanResult{probeMediaIDs: []string{ids[1], ids[1]}})
	release <- struct{}{}
	select {
	case path := <-started:
		if path != "movie-1.mkv" {
			t.Fatalf("later event probed %q", path)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event received during active probe was lost")
	}
	waitIdle := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			probe.autoMu.Lock()
			running := probe.autoRunning
			probe.autoMu.Unlock()
			if !running && len(tracker.Snapshot().Active) == 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("probe tasks did not settle")
	}
	waitIdle()
	snapshot := tracker.Snapshot()
	if len(snapshot.Active) != 0 || len(snapshot.Recent) != 4 {
		t.Fatalf("tasks did not settle: active=%d recent=%d", len(snapshot.Active), len(snapshot.Recent))
	}
	for _, task := range snapshot.Recent {
		if task.Trigger == TaskTriggerEvent && (task.Metrics["total"] != 1 || task.Metrics["completed"] != 1) {
			t.Fatalf("scoped metrics=%v", task.Metrics)
		}
	}
	if _, ok := probe.Load(t.Context(), ids[2]); ok {
		t.Fatal("events included unrelated pending media")
	}
	// 启动恢复仍可找到前述事件未涉及的历史待办。
	probe.WakeBackfill()
	select {
	case path := <-started:
		if path != "movie-2.mkv" {
			t.Fatalf("startup recovery probed %q", path)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup recovery missed old pending media")
	}
	waitIdle()
}

func TestMediaProbeWakeScopeMergesWithoutExpanding(t *testing.T) {
	probe := &MediaProbeService{tasks: NewTaskTrackerService(zap.NewNop(), nil), autoRunning: true}
	probe.WakeMediaBackfill([]string{"a", "a", "", " "})
	probe.WakeMediaBackfill([]string{"b"})
	ids, ok := probe.takeAutomaticWake()
	slices.Sort(ids)
	if !ok || !slices.Equal(ids, []string{"a", "b"}) {
		t.Fatalf("merged scope=%v wake=%v", ids, ok)
	}
	// 同类任务竞争时重新排队必须保留范围。
	probe.wakeBackfill(ids, ids == nil)
	ids, ok = probe.takeAutomaticWake()
	if !ok || len(ids) != 2 {
		t.Fatalf("requeued scope=%v wake=%v", ids, ok)
	}
	probe.WakeMediaBackfill([]string{"a"})
	probe.WakeBackfill()
	ids, ok = probe.takeAutomaticWake()
	if !ok || ids != nil {
		t.Fatalf("startup scope=%v wake=%v", ids, ok)
	}
	probe.WakeMediaBackfill([]string{"b"})
	ids, ok = probe.takeAutomaticWake()
	if !ok || !slices.Equal(ids, []string{"b"}) {
		t.Fatalf("event after startup scope=%v wake=%v", ids, ok)
	}
	if _, ok := probe.takeAutomaticWake(); ok {
		t.Fatal("coordinator retained consumed events")
	}
}
