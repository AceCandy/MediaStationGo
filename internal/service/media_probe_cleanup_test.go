package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestProbeQuarantineRechecksAndRestoresWithoutOverwrite(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe cleanup requires Linux no-replace rename")
	}
	for _, mode := range []string{"delete", "replace", "restore collision", "cancel before", "cancel after", "rename failure", "ambiguous rename failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dir := t.TempDir()
			path := filepath.Join(dir, "movie.mp4")
			writeTestFile(t, path, "old")
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel before" {
				cancel()
			}
			var quarantined string
			rename := func(src, dst string) error {
				if src != path {
					return renameNoReplace(src, dst)
				}
				quarantined = dst
				if mode == "rename failure" {
					return os.ErrPermission
				}
				if mode == "replace" || mode == "restore collision" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					writeTestFile(t, path, "new")
					if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
						t.Fatal(err)
					}
				}
				if err := renameNoReplace(src, dst); err != nil {
					return err
				}
				if mode == "cancel after" {
					cancel()
				}
				if mode == "restore collision" {
					writeTestFile(t, path, "occupied")
				}
				// 即使隔离文件有媒体扩展名，常规遍历也不能将它入库。
				writeTestFile(t, filepath.Join(filepath.Dir(dst), "hidden.mp4"), "test")
				if err := walk(dir, func(p string, _ walkInfo) error {
					if strings.Contains(p, ".probe-cleanup-") {
						t.Errorf("walker exposed quarantine: %s", p)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := walkDirsForWatch(ctx, dir, func(p string) {
					if strings.Contains(p, ".probe-cleanup-") {
						t.Errorf("watcher exposed quarantine: %s", p)
					}
				}); err != nil && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(filepath.Dir(dst), "hidden.mp4")); err != nil {
					t.Fatal(err)
				}
				if mode == "ambiguous rename failure" {
					return os.ErrPermission
				}
				return nil
			}
			deleted, err := removeQuarantinedProbeSource(ctx, mediaProbeSource{path: path, file: info}, rename)
			if deleted != (mode == "delete") || (err == nil) != deleted {
				t.Fatalf("cleanup: deleted=%v err=%v", deleted, err)
			}
			if deleted {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("damaged source still exists: %v", err)
				}
			} else {
				want := "old"
				if mode == "replace" {
					want = "new"
				} else if mode == "restore collision" {
					want = "occupied"
					data, readErr := os.ReadFile(quarantined)
					if readErr != nil || string(data) != "new" || !strings.Contains(err.Error(), filepath.Base(filepath.Dir(quarantined))) || strings.Contains(err.Error(), dir) {
						t.Fatalf("manual recovery: data=%q err=%v cleanup=%v", data, readErr, err)
					}
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != want {
					t.Fatalf("source = %q, %v; want %q", data, err, want)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.IsDir() && mode != "restore collision" {
					t.Fatalf("unexpected leftover quarantine: %s", entry.Name())
				}
			}
		})
	}
}

func TestScannerPruningWaitsForProbeRestoration(t *testing.T) {
	for _, mode := range []string{"watcher", "library", "root"} {
		t.Run(mode, func(t *testing.T) {
			scanner, repos := newScannerTestEnv(t)
			probe := NewMediaProbeService(repos, nil)
			scanner.SetMediaProbe(probe)
			dir := t.TempDir()
			lib := model.Library{Name: "Movies", Path: dir, Type: "movie", Enabled: true}
			if err := repos.Library.Create(t.Context(), &lib); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "movie.mp4")
			writeTestFile(t, path, "video")
			if _, err := scanner.IngestPath(t.Context(), lib.ID, path); err != nil {
				t.Fatal(err)
			}
			prune := func() error {
				switch mode {
				case "watcher":
					watcher := &WatcherService{scanner: scanner, log: zap.NewNop()}
					details, key, _ := watcher.processPath(t.Context(), duePath{path: path, libraryID: lib.ID})
					if key == "failed" {
						return errors.New(strings.Join(details, "; "))
					}
					return nil
				case "library":
					_, err := scanner.pruneMissingMedia(t.Context(), lib.ID, nil)
					return err
				default:
					_, _, err := scanner.pruneMissingMediaForRoot(t.Context(), lib.ID, "", dir, nil)
					return err
				}
			}
			done := make(chan error, 1)
			func() {
				probe.cleanupMu.Lock()
				defer probe.cleanupMu.Unlock()
				if err := os.Rename(path, path+".held"); err != nil {
					t.Fatal(err)
				}
				started := make(chan struct{})
				go func() { close(started); done <- prune() }()
				<-started
				select {
				case err := <-done:
					done <- err
					t.Fatalf("prune observed temporary disappearance: %v", err)
				case <-time.After(40 * time.Millisecond):
				}
				if err := os.Rename(path+".held", path); err != nil {
					t.Fatal(err)
				}
			}()
			select {
			case err := <-done:
				if err != nil || countMedia(t, repos) != 1 {
					t.Fatalf("restored media was pruned: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("prune did not finish after restoration")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := prune(); err != nil || countMedia(t, repos) != 0 {
				t.Fatalf("real deletion was not pruned: %v", err)
			}
		})
	}
}

func TestMediaProbeCleanupWaitsForScanner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("safe cleanup requires Linux no-replace rename")
	}
	db := newServiceTestDB(t, &model.Media{})
	path := filepath.Join(t.TempDir(), "movie.mp4")
	writeTestFile(t, path, "broken")
	media := model.Media{LibraryID: "library", Path: path}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	runner := &stubMediaProbeRunner{
		onProbe: func() { close(started) },
		err:     &exec.ExitError{Stderr: []byte("moov atom not found")},
	}
	probe := NewMediaProbeService(repository.New(db), runner)
	done := make(chan error, 1)
	func() {
		probe.cleanupMu.RLock()
		defer probe.cleanupMu.RUnlock()
		go func() {
			_, err := probe.BackfillLibrary(t.Context(), "library", 0, nil)
			done <- err
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("probe did not start")
		}
		select {
		case err := <-done:
			t.Fatalf("cleanup did not wait for scanner: %v", err)
		case <-time.After(40 * time.Millisecond):
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cleanup moved file while scanner held read lock: %v", err)
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not finish after scanner released lock")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("automatic cleanup was lost: %v", err)
	}
}
