package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func newLocalProbeQueueTestScanner(capacity int) *ScannerService {
	return &ScannerService{
		log:                  zap.NewNop(),
		probe:                &FFprobeService{},
		localMediaProbeQueue: make(chan localMediaProbeTask, capacity),
		localMediaProbing:    make(map[string]struct{}),
	}
}

func TestNewLocalMediaProbeTaskTrimsAndRejectsInvalidInput(t *testing.T) {
	scanner := newLocalProbeQueueTestScanner(1)
	task, ok := scanner.newLocalMediaProbeTask(" C:/library/movie.strm ", " C:/media/movie.mkv ")
	if !ok || task.path != "C:/library/movie.strm" || task.probePath != "C:/media/movie.mkv" {
		t.Fatalf("task = %#v ok=%v, want trimmed valid task", task, ok)
	}
	if _, ok := scanner.newLocalMediaProbeTask(" \t ", "C:/media/movie.mkv"); ok {
		t.Fatal("blank path should be rejected")
	}
	scanner.probe = nil
	if _, ok := scanner.newLocalMediaProbeTask("C:/library/movie.strm", "C:/media/movie.mkv"); ok {
		t.Fatal("scanner without probe should reject local probe task")
	}
}

func TestReserveLocalMediaProbeRejectsDuplicateAndReleaseAllowsRetry(t *testing.T) {
	scanner := newLocalProbeQueueTestScanner(1)
	if !scanner.reserveLocalMediaProbe("C:/media/movie.mkv") {
		t.Fatal("first reserve should succeed")
	}
	if scanner.reserveLocalMediaProbe("C:/media/movie.mkv") {
		t.Fatal("duplicate reserve should fail")
	}
	scanner.releaseLocalMediaProbe("C:/media/movie.mkv")
	if !scanner.reserveLocalMediaProbe("C:/media/movie.mkv") {
		t.Fatal("reserve should succeed after release")
	}
}

func TestEnqueueLocalMediaProbeWaitsForSpace(t *testing.T) {
	scanner := newLocalProbeQueueTestScanner(1)
	if !scanner.enqueueLocalMediaProbe(context.Background(), localMediaProbeTask{path: "C:/library/a.strm", probePath: "C:/media/a.mkv"}) {
		t.Fatal("first enqueue should fit buffer")
	}
	enqueued := make(chan bool, 1)
	go func() {
		enqueued <- scanner.enqueueLocalMediaProbe(context.Background(), localMediaProbeTask{path: "C:/library/b.strm", probePath: "C:/media/b.mkv"})
	}()
	select {
	case <-enqueued:
		t.Fatal("second enqueue should wait while the queue is full")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case task := <-scanner.localMediaProbeQueue:
		if task.path != "C:/library/a.strm" || task.probePath != "C:/media/a.mkv" {
			t.Fatalf("queued task = %#v, want first path", task)
		}
	case <-time.After(time.Second):
		t.Fatal("expected queued task")
	}
	select {
	case ok := <-enqueued:
		if !ok {
			t.Fatal("second enqueue should succeed after space is available")
		}
	case <-time.After(time.Second):
		t.Fatal("second enqueue did not resume")
	}
	if task := <-scanner.localMediaProbeQueue; task.path != "C:/library/b.strm" {
		t.Fatalf("queued task = %#v, want second path", task)
	}
}

func TestLocalProbeAfterUsesSTRMFileTarget(t *testing.T) {
	scanner := newLocalProbeQueueTestScanner(1)
	dir := t.TempDir()
	strmPath := filepath.Join(dir, "movie.strm")
	target := filepath.Join(dir, "movie.mkv")
	media := &model.Media{Path: strmPath, Container: "strm", STRMURL: target}

	if after := scanner.localProbeAfter(context.Background(), media, strmPath, ".strm"); after == nil {
		t.Fatal("local STRM target should schedule ffprobe after scan")
	}
}

func TestEnqueueLocalMediaProbeHonorsContextCancellation(t *testing.T) {
	scanner := newLocalProbeQueueTestScanner(1)
	if !scanner.enqueueLocalMediaProbe(context.Background(), localMediaProbeTask{path: "a", probePath: "a.mkv"}) {
		t.Fatal("first enqueue should fit buffer")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if scanner.enqueueLocalMediaProbe(ctx, localMediaProbeTask{path: "b", probePath: "b.mkv"}) {
		t.Fatal("canceled context should prevent waiting on a full queue")
	}
}
