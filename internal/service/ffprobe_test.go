package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestFFprobeServiceDefaultsToSingleConcurrentProbe(t *testing.T) {
	svc := NewFFprobeService(&config.Config{}, zap.NewNop())
	if got := cap(svc.limiter); got != 1 {
		t.Fatalf("limiter capacity = %d, want 1", got)
	}
}

func TestFFprobeServiceClampsConfiguredConcurrency(t *testing.T) {
	cfg := &config.Config{}
	cfg.App.FFprobeMaxConcurrent = 3
	svc := NewFFprobeService(cfg, zap.NewNop())
	if got := cap(svc.limiter); got != 3 {
		t.Fatalf("limiter capacity = %d, want 3", got)
	}

	cfg.App.FFprobeMaxConcurrent = 99
	svc = NewFFprobeService(cfg, zap.NewNop())
	if got := cap(svc.limiter); got != 8 {
		t.Fatalf("limiter capacity = %d, want max clamp 8", got)
	}
}

func TestFFprobeAcquireHonorsContextWhenLimitReached(t *testing.T) {
	svc := &FFprobeService{limiter: make(chan struct{}, 1)}
	token, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := svc.acquire(ctx); err == nil {
		t.Fatal("second acquire should block until context deadline")
	}
	svc.release(token)
	token, err = svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	svc.release(token)
}

func TestFFprobeSetMaxConcurrentHotSwapsLimiter(t *testing.T) {
	svc := NewFFprobeService(&config.Config{}, zap.NewNop())
	firstToken, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	svc.SetMaxConcurrent(2)
	secondToken, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire after resize: %v", err)
	}
	thirdToken, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("second acquire after resize: %v", err)
	}
	svc.release(firstToken)
	svc.release(secondToken)
	svc.release(thirdToken)
}

func TestFFprobeFailureDoesNotStartFFmpeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell sentinel is POSIX-only")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ffmpeg-started")
	ffprobePath := filepath.Join(dir, "ffprobe")
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffprobePath, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ffmpegPath, []byte(fmt.Sprintf("#!/bin/sh\n: > %q\n", marker)), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{App: config.AppConfig{FFprobePath: ffprobePath}}
	if _, err := NewFFprobeService(cfg, zap.NewNop()).Probe(t.Context(), "movie.mkv"); err == nil {
		t.Fatal("ffprobe failure should be returned")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ffmpeg sentinel was executed: %v", err)
	}
}

func TestApplyRuntimeSettingFFprobeMaxConcurrent(t *testing.T) {
	cfg := &config.Config{}
	ApplyRuntimeSetting(cfg, "ffprobe.max_concurrent", "4")
	if cfg.App.FFprobeMaxConcurrent != 4 {
		t.Fatalf("FFprobeMaxConcurrent = %d, want 4", cfg.App.FFprobeMaxConcurrent)
	}
	ApplyRuntimeSetting(cfg, "ffprobe.max_concurrent", "99")
	if cfg.App.FFprobeMaxConcurrent != 8 {
		t.Fatalf("FFprobeMaxConcurrent = %d, want clamp 8", cfg.App.FFprobeMaxConcurrent)
	}
	ApplyRuntimeSetting(cfg, "ffprobe.max_concurrent", "0")
	if cfg.App.FFprobeMaxConcurrent != 1 {
		t.Fatalf("FFprobeMaxConcurrent = %d, want clamp 1", cfg.App.FFprobeMaxConcurrent)
	}
}

func TestParseProbeJSONExtractsPrimaryStreams(t *testing.T) {
	got, err := parseProbeJSON([]byte(`{
		"format": {"duration": "125.900000", "format_name": "matroska,webm"},
		"streams": [
			{"codec_type": "video", "codec_name": "hevc", "width": 3840, "height": 2160},
			{"codec_type": "audio", "codec_name": "eac3"},
			{"codec_type": "video", "codec_name": "h264", "width": 1920, "height": 1080}
		]
	}`))
	if err != nil {
		t.Fatalf("parseProbeJSON: %v", err)
	}
	if got.DurationSec != 125 || got.Container != "matroska,webm" || got.VideoCodec != "hevc" || got.AudioCodec != "eac3" || got.Width != 3840 || got.Height != 2160 {
		t.Fatalf("parsed probe = %+v", got)
	}
}

func TestParseProbeJSONDerivesBitDepthFromPixelFormat(t *testing.T) {
	for _, test := range []struct {
		format string
		want   int
	}{
		{format: "yuv420p10le", want: 10},
		{format: "yuv420p12le", want: 12},
		{format: "p010le", want: 10},
		{format: "p210le", want: 10},
		{format: "p410le", want: 10},
		{format: "p012le", want: 12},
		{format: "p212le", want: 12},
		{format: "p412le", want: 12},
		{format: "y210le", want: 10},
		{format: "x2rgb10le", want: 10},
		{format: "yuv420p", want: 0},
	} {
		t.Run(test.format, func(t *testing.T) {
			got, err := parseProbeJSON([]byte(fmt.Sprintf(`{"streams":[{"codec_type":"video","pix_fmt":"%s"}]}`, test.format)))
			if err != nil {
				t.Fatal(err)
			}
			if got.Document.Streams[0].BitDepth != test.want {
				t.Fatalf("bit depth = %d, want %d", got.Document.Streams[0].BitDepth, test.want)
			}
		})
	}
}

func TestParseProbeJSONPreservesSafeCompleteDocument(t *testing.T) {
	got, err := parseProbeJSON([]byte(`{
		"format": {
			"filename": "https://example.invalid/movie.mkv?token=secret",
			"format_name": "matroska,webm", "format_long_name": "Matroska / WebM",
			"duration": "125.9", "size": "4096", "bit_rate": "8000000", "probe_score": 100,
			"tags": {"title": "Movie", "authorization": "Bearer secret", "cookie": "sid=secret"}
		},
		"streams": [
			{"index": 0, "codec_type": "video", "codec_name": "hevc", "profile": "Main 10",
			 "width": 3840, "height": 2160, "pix_fmt": "yuv420p10le", "bits_per_raw_sample": "10",
			 "avg_frame_rate": "25/1", "color_range": "tv", "color_space": "bt2020nc",
			 "color_transfer": "smpte2084", "color_primaries": "bt2020",
			 "side_data_list": [{"side_data_type": "Content light level metadata", "max_content": 1000, "max_average": 400}]},
			{"index": 1, "codec_type": "audio", "codec_name": "aac", "sample_rate": "48000", "channels": 2,
			 "tags": {"language": "chi", "title": "Mandarin"}, "disposition": {"default": 1}},
			{"index": 2, "codec_type": "audio", "codec_name": "aac", "tags": {"language": "jpn"}},
			{"index": 3, "codec_type": "audio", "codec_name": "eac3", "channels": 6},
			{"index": 4, "codec_type": "subtitle", "codec_name": "ass", "tags": {"language": "chi", "title": "Signs"},
			 "disposition": {"forced": 1}},
			{"index": 5, "codec_type": "video", "codec_name": "mjpeg", "disposition": {"attached_pic": 1}},
			{"index": 6, "codec_type": "attachment", "codec_name": "ttf"}
		],
		"chapters": [{"id": 7, "time_base": "1/1000", "start_time": "0", "end_time": "60.5", "tags": {"title": "Intro", "url": "https://secret"}}]
	}`))
	if err != nil {
		t.Fatalf("parseProbeJSON: %v", err)
	}
	if got.Document == nil || len(got.Document.Streams) != 5 || len(got.Document.Chapters) != 1 {
		t.Fatalf("complete document missing: %#v", got.Document)
	}
	if got.Document.Streams[3].Index != 3 || got.Document.Streams[3].CodecName != "eac3" {
		t.Fatalf("third audio stream lost: %#v", got.Document.Streams)
	}
	video := got.Document.Streams[0]
	if video.Profile != "Main 10" || video.BitDepth != 10 || video.AverageFrameRate != "25/1" || video.ColorTransfer != "smpte2084" {
		t.Fatalf("video facts lost: %#v", video)
	}
	if !got.Document.Streams[1].Disposition.Default || !got.Document.Streams[4].Disposition.Forced {
		t.Fatalf("stream dispositions lost: %#v", got.Document.Streams)
	}
	persisted, err := MarshalProbeDocument(got.Document)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"filename", "example.invalid", "secret", "authorization", "cookie", "https://"} {
		if strings.Contains(strings.ToLower(persisted), forbidden) {
			t.Fatalf("persisted probe contains forbidden %q: %s", forbidden, persisted)
		}
	}
	roundTrip, err := UnmarshalProbeDocument(persisted, ProbeDocumentSchemaVersion)
	if err != nil || len(roundTrip.Streams) != 5 || roundTrip.Streams[4].Index != 4 {
		t.Fatalf("round trip failed: doc=%#v err=%v", roundTrip, err)
	}
}

func TestUnmarshalProbeDocumentRejectsInvalidStreams(t *testing.T) {
	for name, data := range map[string]string{
		"negative index":   `{"schema_version":1,"format":{},"streams":[{"index":-1,"codec_type":"audio"}]}`,
		"duplicate index":  `{"schema_version":1,"format":{},"streams":[{"index":1,"codec_type":"audio"},{"index":1,"codec_type":"subtitle"}]}`,
		"attachment":       `{"schema_version":1,"format":{},"streams":[{"index":2,"codec_type":"attachment"}]}`,
		"attached picture": `{"schema_version":1,"format":{},"streams":[{"index":3,"codec_type":"video","disposition":{"attached_pic":true}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := UnmarshalProbeDocument(data, ProbeDocumentSchemaVersion); err == nil {
				t.Fatalf("invalid document accepted: %s", data)
			}
		})
	}
}
