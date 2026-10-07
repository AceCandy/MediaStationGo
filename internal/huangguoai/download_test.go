package huangguoai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadHLSDelayedVideoParameters(t *testing.T) {
	dir := t.TempDir()
	segment := filepath.Join(dir, "synthetic.ts")
	// 视频晚于音频出现，默认探测在取得画面尺寸前就结束。
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-itsoffset", "8", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=2", "-f", "lavfi", "-i", "sine=frequency=440:duration=10", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "mpegts", segment).Run(); err != nil {
		t.Fatal("generate synthetic media:", err)
	}
	data, err := os.ReadFile(segment)
	if err != nil {
		t.Fatal(err)
	}
	playlist := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:2,\nsegment.ts\n#EXT-X-ENDLIST\n")
	c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
		body := playlist
		if req.URL.Path == "/segment.ts" {
			body = data
		} else if req.URL.Path != "/input.m3u8" {
			t.Fatalf("unexpected resource path: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
	})})
	output, duration, err := c.Download(t.Context(), Media{URL: "https://example.com/input.m3u8"}, dir, nil)
	if err != nil || duration != 2 {
		t.Fatalf("delayed video merge: duration=%v error=%v", duration, err)
	}
	probe, err := exec.CommandContext(t.Context(), "ffprobe", "-v", "error", "-show_entries", "stream=codec_type,width,height", "-of", "json", output).Output()
	if err != nil {
		t.Fatal("probe output:", err)
	}
	var info struct {
		Streams []struct {
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(probe, &info); err != nil {
		t.Fatal(err)
	}
	video, audio := false, false
	for _, stream := range info.Streams {
		video = video || stream.Type == "video" && stream.Width == 64 && stream.Height == 64
		audio = audio || stream.Type == "audio"
	}
	if !video || !audio {
		t.Fatal("merged output lost video parameters or audio")
	}
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode", "-i", output, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-").Run(); err != nil {
		t.Fatal("complete decode:", err)
	}
	// 在真实下载入口验证 stderr 被捕获并分类，而不是仅测试分类器本身。
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "ffmpeg"), []byte("#!/bin/sh\nprintf 'private-title /private/path token=secret-value dimensions not set' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, _, err := c.Download(t.Context(), Media{URL: "https://example.com/input.m3u8"}, t.TempDir(), nil); err == nil || err.Error() != "HLS 合并失败：未识别到视频尺寸" {
		t.Fatalf("unsafe or missing merge diagnostic: %v", err)
	}
}

type downloadTestTransport func(*http.Request) (*http.Response, error)

func (f downloadTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHLSMergeErrorKeepsDiagnosticsPrivate(t *testing.T) {
	err := exec.CommandContext(t.Context(), "sh", "-c", "exit 1").Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	for _, tt := range []struct{ stderr, want string }{
		{"dimensions not set", "未识别到视频尺寸"},
		{"sample rate not set", "音轨缺少有效采样率"},
		{"No space left on device", "存储空间不足"},
		{"Permission denied", "暂存文件权限不足"},
		{"Invalid data found when processing input", "分片或密钥无法解析"},
		{"unknown tool error", "FFmpeg 退出码 1"},
	} {
		exit.Stderr = []byte("private-title /private/path https://example.com/?token=secret-value " + tt.stderr)
		message := hlsMergeError(exit).Error()
		if !strings.Contains(message, tt.want) {
			t.Fatalf("missing diagnostic: %s", message)
		}
		for _, private := range []string{"private-title", "/private/path", "https://", "secret-value", tt.stderr} {
			if strings.Contains(message, private) {
				t.Fatalf("private diagnostic leaked: %s", message)
			}
		}
	}
	if got := hlsMergeError(exec.ErrNotFound).Error(); !strings.Contains(got, "未安装或不可执行") {
		t.Fatal(got)
	}
	if got := hlsMergeError(errors.New("private-title secret-value")).Error(); got != "HLS 合并失败：FFmpeg 无法启动" {
		t.Fatal(got)
	}
}
