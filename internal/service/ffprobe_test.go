package service

import "testing"

func TestParseFFmpegProbeText(t *testing.T) {
	text := `Input #0, matroska,webm, from 'show.mkv':
  Duration: 00:23:42.11, start: 0.000000, bitrate: 5132 kb/s
  Stream #0:0: Video: h264 (Main), yuv420p(progressive), 1920x1080 [SAR 1:1 DAR 16:9], 23.98 fps
  Stream #0:1(jpn): Audio: eac3, 48000 Hz, stereo, fltp, 128 kb/s (default)`

	got := parseFFmpegProbeText(text)
	if got.Container != "matroska,webm" {
		t.Fatalf("container = %q", got.Container)
	}
	if got.DurationSec != 1422 {
		t.Fatalf("duration = %d", got.DurationSec)
	}
	if got.VideoCodec != "h264" || got.Width != 1920 || got.Height != 1080 {
		t.Fatalf("video = %#v", got)
	}
	if got.AudioCodec != "eac3" {
		t.Fatalf("audio = %q", got.AudioCodec)
	}
}
