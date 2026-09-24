package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestStartupProgressDuringBlockedStep(t *testing.T) {
	c := &Container{Startup: NewStartupState(), stopCtx: t.Context()}
	c.Startup.started = time.Now().Add(-10 * time.Second)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = c.startupStep("建立媒体库目录监听", func() error {
			c.Startup.updateDirectories(200, 128)
			close(entered)
			<-release
			return errors.New("private/path must not enter startup status")
		})
	}()
	<-entered
	status := c.StartupStatus()
	close(release)
	<-done
	if status.State != "starting" || status.Stage != "建立媒体库目录监听" || status.ElapsedSeconds < 10 || status.DirectoriesFound != 200 || status.DirectoriesWatched != 128 {
		t.Fatalf("startup progress: %+v", status)
	}
	c.Startup.finish("ready")
	status = c.StartupStatus()
	if status.State != "ready" || len(status.Warnings) != 1 || strings.Contains(status.Warnings[0], "private") {
		t.Fatalf("startup warning: %+v", status)
	}
	status.Warnings[0] = "changed"
	if c.StartupStatus().Warnings[0] == "changed" {
		t.Fatal("snapshot aliases mutable warnings")
	}
}

func TestWatchDirectoryTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "a/nested", ".hidden/child"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "z.mp4")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var dirs []string
	err := walkDirsForWatch(t.Context(), root, func(path string) {
		dirs = append(dirs, path)
		// 父目录的条目已经读出；文件此时消失也不应使目录监听失败。
		if path == filepath.Join(root, "a") {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil || !reflect.DeepEqual(dirs, []string{root, filepath.Join(root, "a"), filepath.Join(root, "a/nested")}) {
		t.Fatalf("directories: %v, %v", dirs, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := walkDirsForWatch(ctx, root, func(string) { t.Fatal("visited after cancellation") }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := walkDirsForWatch(t.Context(), filepath.Join(root, "missing"), func(string) {}); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	dirs = nil
	hiddenRoot := filepath.Join(root, ".hidden")
	if err := walkDirsForWatch(t.Context(), hiddenRoot, func(path string) { dirs = append(dirs, path) }); err != nil || !reflect.DeepEqual(dirs, []string{hiddenRoot}) {
		t.Fatalf("explicit hidden root: %v, %v", dirs, err)
	}
	linkedRoot := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(root, linkedRoot); err != nil {
		t.Fatal(err)
	}
	dirs = nil
	if err := walkDirsForWatch(t.Context(), linkedRoot, func(path string) { dirs = append(dirs, path) }); err != nil || !reflect.DeepEqual(dirs, []string{linkedRoot}) {
		t.Fatalf("explicit symlink root: %v, %v", dirs, err)
	}
}

func TestBootRegistersSchedulerLast(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "ready"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			db := newServiceTestDB(t, &model.Library{}, &model.APIConfig{}, &model.Setting{})
			repo := repository.New(db)
			// 本用例只验证启动顺序，不启动外部搜索预热。
			repo.MediaView = nil
			log := zap.NewNop()
			ctx, cancel := context.WithCancel(t.Context())
			c := &Container{Repo: repo, Log: log, Startup: NewStartupState(), stopCtx: ctx, stopCancel: cancel}
			c.Watcher = NewWatcherService(log, repo, nil, nil)
			c.APIConfig = NewAPIConfigService(log, repo, nil)
			c.Scheduler = NewSchedulerService(log, repo, nil, nil, nil)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			c.Watcher.progress = func(found, watched int) {
				c.Startup.updateDirectories(found, watched)
				close(entered)
				<-release
			}
			go func() { c.Boot(); close(done) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				cancel()
				t.Fatal("watcher initialization not reached")
			}
			status, jobs := c.StartupStatus(), c.Scheduler.Status()
			closed := make(chan struct{})
			if shutdown {
				go func() { c.Close(); close(closed) }()
				<-ctx.Done()
				// 取消已经送达，但 Boot 仍阻塞时不能释放 watcher。
				select {
				case <-closed:
					t.Error("Close returned before initialization stopped")
				case <-c.Watcher.stop:
					t.Error("watcher closed before initialization stopped")
				case <-time.After(30 * time.Millisecond):
				}
			}
			close(release)
			<-done
			if shutdown {
				select {
				case <-closed:
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not finish after Boot exited")
				}
			} else {
				defer c.Close()
			}
			if status.State != "starting" || status.Stage != "建立媒体库目录监听" || len(jobs) != 0 {
				t.Fatalf("premature scheduler: %+v %+v", status, jobs)
			}
			if shutdown {
				if c.StartupStatus().State != "failed" || len(c.Scheduler.Status()) != 0 {
					t.Fatal("shutdown registered scheduler or marked startup ready")
				}
			} else if c.StartupStatus().State != "ready" || len(c.Scheduler.Status()) == 0 {
				t.Fatal("scheduler not registered after startup")
			}
		})
	}
}

func TestBootCanceledDoesNotRegisterScheduler(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := &Container{Startup: NewStartupState(), Tasks: NewTaskTrackerService(zap.NewNop(), nil), stopCtx: ctx}
	c.Scheduler = NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	defer c.Scheduler.Stop()
	c.Boot()
	if c.StartupStatus().State != "failed" || len(c.Scheduler.Status()) != 0 {
		t.Fatal("canceled startup became ready")
	}
}
