package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"go.uber.org/zap"
)

func TestBuildFFmpegArgs(t *testing.T) {
	base := &config.Config{}
	base.Transcoder.MaxHeight = 720
	base.Transcoder.SegmentSeconds = 4
	base.Transcoder.Realtime = true
	base.Transcoder.Threads = 2
	base.App.VAAPIDevice = "/dev/dri/renderD128"

	cases := []struct {
		name                   string
		encoder                string
		expectVCodec           string
		expectInArgs           []string
		expectNotPresetIfBlank bool
	}{
		{"software", "", "libx264", []string{"-re", "-preset", "veryfast", "-c:v", "libx264", "-threads", "2"}, false},
		{"nvenc", "nvenc", "h264_nvenc", []string{"-hwaccel", "cuda", "-c:v", "h264_nvenc", "-preset", "p4"}, false},
		{"qsv", "qsv", "h264_qsv", []string{"-hwaccel", "qsv", "-c:v", "h264_qsv"}, false},
		{"vaapi", "vaapi", "h264_vaapi", []string{"-hwaccel", "vaapi", "-vaapi_device", "/dev/dri/renderD128", "-c:v", "h264_vaapi"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := *base
			cfg.Transcoder.Encoder = tc.encoder
			cfg.Transcoder.HardwareAccel = tc.encoder != ""
			args := buildFFmpegArgs(&cfg, "/x.mkv", "/o/x.m3u8", "/o/seg_%05d.ts", -1)
			joined := strings.Join(args, " ")
			for _, frag := range tc.expectInArgs {
				if !strings.Contains(joined, frag) {
					t.Errorf("expected %q in args, got: %s", frag, joined)
				}
			}
			// vaapi has no -preset flag.
			if tc.expectNotPresetIfBlank && strings.Contains(joined, "-preset") {
				t.Errorf("vaapi should not include -preset, got: %s", joined)
			}
		})
	}
}

func TestBuildFFmpegArgsIgnoresEncoderWhenHardwareAccelDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Transcoder.Encoder = "nvenc"
	cfg.Transcoder.HardwareAccel = false
	cfg.Transcoder.MaxHeight = 720
	cfg.Transcoder.SegmentSeconds = 4
	cfg.Transcoder.Realtime = true
	cfg.Transcoder.Threads = 2

	args := buildFFmpegArgs(cfg, "/x.mkv", "/o/x.m3u8", "/o/seg_%05d.ts", -1)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "h264_nvenc") {
		t.Fatalf("hardware disabled should not use nvenc, got: %s", joined)
	}
	if !strings.Contains(joined, "libx264") {
		t.Fatalf("hardware disabled should fall back to libx264, got: %s", joined)
	}
}

func TestBuildFFmpegArgsCanDisableRealtimeAndThreadCap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Transcoder.MaxHeight = 720
	cfg.Transcoder.SegmentSeconds = 4
	cfg.Transcoder.Realtime = false
	cfg.Transcoder.Threads = 0

	args := buildFFmpegArgs(cfg, "/x.mkv", "/o/x.m3u8", "/o/seg_%05d.ts", -1)
	joined := " " + strings.Join(args, " ") + " "
	if strings.Contains(joined, " -re ") {
		t.Fatalf("realtime=false should not include -re, got: %s", joined)
	}
	if strings.Contains(joined, " -threads ") {
		t.Fatalf("threads=0 should not include -threads, got: %s", joined)
	}
}

func TestBuildFFmpegArgsUsesAbsoluteAudioStreamIndex(t *testing.T) {
	cfg := &config.Config{}
	args := buildFFmpegArgs(cfg, "/x.mkv", "/o/x.m3u8", "/o/seg_%05d.ts", 3)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-map 0:3?") || strings.Contains(joined, "-map 0:a:0?") {
		t.Fatalf("audio map = %s", joined)
	}
}

func TestRequiredVideoEncoder(t *testing.T) {
	cases := map[string]string{
		"":      "libx264",
		"nvenc": "h264_nvenc",
		"qsv":   "h264_qsv",
		"vaapi": "h264_vaapi",
	}
	for encoder, want := range cases {
		if got := requiredVideoEncoder(encoder); got != want {
			t.Fatalf("requiredVideoEncoder(%q) = %q, want %q", encoder, got, want)
		}
	}
}

func TestTranscoderJobLifecycleIsIsolatedByAudioSelection(t *testing.T) {
	cfg := &config.Config{}
	cfg.Cache.CacheDir = t.TempDir()
	transcoder := NewTranscoderService(cfg, zap.NewNop(), nil, nil)
	first := TranscodeKey{MediaID: "media", AudioStreamIndex: 2}
	second := TranscodeKey{MediaID: "media", AudioStreamIndex: 3}
	firstCtx, firstCancel := context.WithCancel(context.Background())
	_, secondCancel := context.WithCancel(context.Background())
	for _, key := range []TranscodeKey{first, second} {
		if err := os.MkdirAll(transcoder.HLSDir(key), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	firstJob := &hlsJob{key: first, mediaID: first.MediaID, outputDir: transcoder.HLSDir(first), cancel: firstCancel}
	transcoder.jobs[first.String()] = firstJob
	transcoder.jobs[second.String()] = &hlsJob{key: second, mediaID: second.MediaID, outputDir: transcoder.HLSDir(second), cancel: secondCancel}

	transcoder.StopJob(first)
	select {
	case <-firstCtx.Done():
	default:
		t.Fatal("selected job was not canceled")
	}
	active := transcoder.Active()
	if len(active) != 1 || active[0].JobID != second.String() {
		t.Fatalf("active jobs = %#v", active)
	}
	if _, err := os.Stat(transcoder.HLSDir(first)); !os.IsNotExist(err) {
		t.Fatalf("first output directory still exists: %v", err)
	}
	if _, err := os.Stat(transcoder.HLSDir(second)); err != nil {
		t.Fatalf("second output directory was removed: %v", err)
	}
	restarted := &hlsJob{key: first, mediaID: first.MediaID, cancel: func() {}}
	transcoder.jobs[first.String()] = restarted
	if transcoder.removeJobIfCurrent(firstJob) {
		t.Fatal("superseded job was allowed to publish a terminal event")
	}
	if transcoder.jobs[first.String()] != restarted {
		t.Fatal("old job cleanup removed the restarted job")
	}
	delete(transcoder.jobs, first.String())
	transcoder.StopJob(second)
}

func TestTranscoderWaitReadyTracksPlaylistAndCancellation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Cache.CacheDir = t.TempDir()
	transcoder := NewTranscoderService(cfg, zap.NewNop(), nil, nil)
	key := TranscodeKey{MediaID: "media", AudioStreamIndex: 2}
	_, cancel := context.WithCancel(context.Background())
	transcoder.jobs[key.String()] = &hlsJob{key: key, cancel: cancel}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.MkdirAll(transcoder.HLSDir(key), 0o750)
		_ = os.WriteFile(filepath.Join(transcoder.HLSDir(key), "index.m3u8"), []byte("#EXTM3U"), 0o600)
	}()
	if !transcoder.WaitReady(t.Context(), key, time.Second) {
		t.Fatal("playlist did not become ready")
	}
	if active := transcoder.Active(); len(active) != 1 || !active[0].PlaylistOK {
		t.Fatalf("active jobs = %#v", active)
	}
	canceled, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if transcoder.WaitReady(canceled, TranscodeKey{MediaID: "other", AudioStreamIndex: 2}, time.Second) {
		t.Fatal("canceled wait reported ready")
	}
	transcoder.StopJob(key)
}

func TestHasFFmpegListEntry(t *testing.T) {
	out := " V..... libx264              libx264 H.264 / AVC\n A..... aac"
	if !hasFFmpegListEntry(out, "libx264") {
		t.Fatal("expected libx264 entry")
	}
	if hasFFmpegListEntry(out, "x264") {
		t.Fatal("must match whole ffmpeg list entries only")
	}
}
