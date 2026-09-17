package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestHongGuoDownloadHardwareLive(t *testing.T) {
	path := os.Getenv("MEDIASTATION_TEST_HONGGUO_VAAPI_FILE")
	if path == "" {
		t.Skip("set MEDIASTATION_TEST_HONGGUO_VAAPI_FILE to opt into read-only device verification")
	}
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	task := tracker.Start(TaskKindHongGuoDownload, "硬件校验测试", TaskUpdate{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := verifyHongGuoDownload(ctx, path, 0, true, task, nil)
	active := tracker.Snapshot().Active
	task.Finish(err, TaskUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Message != "正在使用 VAAPI 核显解码校验" {
		t.Fatal("hardware verification fell back to software")
	}
}

func TestHongGuoDownloadHardwareFallback(t *testing.T) {
	for _, tt := range []struct {
		name            string
		hardware        bool
		calls           int
		fail, cancelled bool
	}{
		{"software", false, 1, false, false},
		{"hardware", true, 1, false, false},
		{"fallback", true, 2, false, false},
		{"corrupt", true, 2, true, false},
		{"cancel", true, 1, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "calls")
			t.Setenv("PATH", dir)
			t.Setenv("VERIFY_TEST_CALLS", logPath)
			t.Setenv("VERIFY_TEST_MODE", tt.name)
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$VERIFY_TEST_CALLS"
case " $* " in
  *" -hwaccel vaapi "*)
    if [ "$VERIFY_TEST_MODE" = cancel ]; then exec /bin/sleep 30; fi
    if [ "$VERIFY_TEST_MODE" != hardware ]; then exit 1; fi
    ;;
  *) if [ "$VERIFY_TEST_MODE" = corrupt ]; then printf 'Invalid data found when processing input' >&2; exit 1; fi ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if tt.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 300*time.Millisecond)
				defer cancel()
			}
			err := decodeHongGuoDownload(ctx, "sample.mp4", tt.hardware, nil)
			if (err != nil) != tt.fail {
				t.Fatalf("error=%v", err)
			}
			if tt.cancelled && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if tt.name == "corrupt" && !errors.Is(err, errHongGuoDownloadSource) {
				t.Fatalf("lost media error: %v", err)
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			calls := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(calls) != tt.calls {
				t.Fatalf("calls=%q", calls)
			}
			if strings.Contains(calls[0], "-hwaccel vaapi") != tt.hardware {
				t.Fatalf("wrong decoder: %s", calls[0])
			}
			if tt.hardware && !strings.Contains(calls[0], "-hwaccel_device /dev/dri/renderD128 -hwaccel_output_format vaapi") {
				t.Fatal("missing device or hardware frame requirement")
			}
			if len(calls) == 2 && strings.Contains(calls[1], "-hwaccel") {
				t.Fatal("fallback still uses hardware")
			}
		})
	}
}
