package huangguoai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func installParallelHLSTools(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for name, script := range map[string]string{
		"ffprobe": "#!/bin/sh\nprintf '{\"streams\":[{\"duration\":\"4\"}]}'\n",
		"ffmpeg":  "#!/bin/sh\nfor output; do :; done\nprintf synthetic > \"$output\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type parallelHLSBody struct {
	io.Reader
	ctx     context.Context
	gate    <-chan struct{}
	onClose func()
}

func (r *parallelHLSBody) Read(p []byte) (int, error) {
	if r.gate != nil {
		select {
		case <-r.gate:
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}
	}
	return r.Reader.Read(p)
}

func (r *parallelHLSBody) Close() error {
	if r.onClose != nil {
		r.onClose()
	}
	return nil
}

func TestDownloadHLSParallelOrderAndProgress(t *testing.T) {
	installParallelHLSTools(t)
	playlist := "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:7\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:1,\n0.ts\n#EXTINF:1,\n1.ts\n#EXT-X-DISCONTINUITY\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=0x2\n#EXTINF:1,\n2.ts\n#EXT-X-KEY:METHOD=NONE\n#EXTINF:1,\n3.ts\n#EXT-X-ENDLIST\n"
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	calls := map[string]int{}
	active, peak := 0, 0
	c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Path]++
		attempt := calls[req.URL.Path]
		mu.Unlock()
		body := []byte("map")
		resp := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
		switch req.URL.Path {
		case "/input.m3u8":
			body = []byte(playlist)
		case "/key.bin":
			body = bytes.Repeat([]byte{byte(attempt)}, 16)
		case "/init.mp4":
		default:
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			mu.Unlock()
			gate := started
			if req.URL.Path == "/0.ts" {
				close(started)
				gate = release
			}
			body = bytes.Repeat([]byte(req.URL.Path[1:2]), int(req.URL.Path[1]-'0')+1)
			resp.ContentLength = int64(len(body))
			resp.Body = &parallelHLSBody{Reader: bytes.NewReader(body), ctx: req.Context(), gate: gate, onClose: func() {
				mu.Lock()
				active--
				mu.Unlock()
				if req.URL.Path == "/3.ts" {
					close(release)
				}
			}}
			return resp, nil
		}
		resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
		return resp, nil
	})})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	dir := t.TempDir()
	var progress []int64
	_, duration, err := c.Download(ctx, Media{URL: "https://example.com/input.m3u8"}, dir, func(n int64) { progress = append(progress, n) })
	if err != nil || duration != 4 || peak != 2 || active != 0 {
		t.Fatalf("parallel transfer: peak=%d active=%d duration=%v err=%v", peak, active, duration, err)
	}
	if calls["/key.bin"] != 2 || calls["/init.mp4"] != 1 {
		t.Fatalf("key generations or shared map changed: %v", calls)
	}
	for i := 0; i < 4; i++ {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("segment-%06d.m4s", i)))
		if err != nil || string(data) != strings.Repeat(string(rune('0'+i)), i+1) {
			t.Fatalf("segment %d contents/order changed: %v", i, err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "input.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for i := 0; i < 4; i++ {
		pos := strings.Index(string(manifest), fmt.Sprintf("segment-%06d.m4s", i))
		if pos <= last {
			t.Fatalf("manifest lost original order: segment %d", i)
		}
		last = pos
	}
	for _, value := range []string{"IV=0x00000000000000000000000000000007", "IV=0x00000000000000000000000000000008", "IV=0x00000000000000000000000000000002", "key-0001.bin", "#EXT-X-DISCONTINUITY", "METHOD=NONE"} {
		if !strings.Contains(string(manifest), value) {
			t.Fatalf("manifest lost encryption/discontinuity: %s", value)
		}
	}
	if len(progress) != 4 || progress[len(progress)-1] != 13 {
		t.Fatalf("incorrect cumulative progress: %v", progress)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] <= progress[i-1] {
			t.Fatalf("non-monotonic progress: %v", progress)
		}
	}
}

func TestDownloadHLSParallelFailureJoinsWorkers(t *testing.T) {
	for _, failure := range []string{"http", "oversize", "existing", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			dir := t.TempDir()
			sentinel := filepath.Join(dir, "segment-000002.ts")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if failure == "existing" {
				if err := os.WriteFile(filepath.Join(dir, "segment-000001.ts"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			readStarted, closeStarted, releaseClose := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(releaseClose)
			c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
				switch req.URL.Path {
				case "/input.m3u8":
					body := "#EXTM3U\n#EXTINF:1,\nfirst.ts\n#EXTINF:1,\nsecond.ts\n#EXTINF:1,\nthird.ts\n#EXT-X-ENDLIST\n"
					resp.Body, resp.ContentLength = io.NopCloser(strings.NewReader(body)), int64(len(body))
				case "/first.ts":
					resp.ContentLength = 2
					// 等到fetchResource创建文件并开始读后，另一分片才触发失败。
					resp.Body = &parallelFailureBody{ctx: req.Context(), started: readStarted, onClose: func() error {
						close(closeStarted)
						<-releaseClose
						return nil
					}}
				case "/second.ts":
					select {
					case <-readStarted:
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
					resp.Body, resp.ContentLength = io.NopCloser(strings.NewReader("ok")), 2
					if failure == "http" {
						resp.StatusCode = 404
					} else if failure == "oversize" {
						resp.ContentLength = maxSegmentBytes + 1
					} else if failure == "cancel" {
						cancel()
					}
				default:
					return nil, errors.New("unexpected request after failure")
				}
				return resp, nil
			})})
			done := make(chan error, 1)
			go func() {
				_, _, err := c.Download(ctx, Media{URL: "https://example.com/input.m3u8"}, dir, nil)
				done <- err
			}()
			select {
			case <-closeStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("sibling transfer was not cancelled")
			}
			select {
			case err := <-done:
				t.Fatalf("Download returned before sibling closed: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			releaseClose <- struct{}{}
			err := <-done
			if err == nil || (failure == "cancel" && !errors.Is(err, context.Canceled)) || (failure == "http" && !strings.Contains(err.Error(), "HTTP 404")) || (failure == "oversize" && !strings.Contains(err.Error(), "超出大小限制")) {
				t.Fatalf("original failure lost: %v", err)
			}
			for _, name := range []string{"segment-000000.ts", "input.m3u8", "output.mp4"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("failure left partial or merged file: %s", name)
				}
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "keep" {
				t.Fatal("untouched existing file was removed")
			}
			if failure == "existing" {
				data, err := os.ReadFile(filepath.Join(dir, "segment-000001.ts"))
				if err != nil || string(data) != "keep" {
					t.Fatal("existing segment was replaced or removed")
				}
			}
		})
	}
}

// 首次读返回一个字节，后续读一直等到同组取消。
type parallelFailureBody struct {
	ctx     context.Context
	started chan struct{}
	onClose func() error
	read    bool
}

func (r *parallelFailureBody) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		p[0] = 'x'
		close(r.started)
		return 1, nil
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *parallelFailureBody) Close() error { return r.onClose() }

func TestDownloadHLSParallelRealMerge(t *testing.T) {
	source := t.TempDir()
	playlistPath := filepath.Join(source, "input.m3u8")
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-c:v", "mpeg2video", "-g", "10", "-c:a", "aac", "-f", "hls", "-hls_time", "1", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(source, "part-%03d.ts"), playlistPath).Run(); err != nil {
		t.Fatal("generate multi-segment media:", err)
	}
	body, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePlaylist(body, "https://example.com/input.m3u8")
	if err != nil || len(p.Segments) < 3 {
		t.Fatalf("synthetic playlist is not multi-segment: %v", err)
	}
	c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
		body, err := os.ReadFile(filepath.Join(source, filepath.Base(req.URL.Path)))
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
	})})
	output, duration, err := c.Download(t.Context(), Media{URL: "https://example.com/input.m3u8"}, t.TempDir(), nil)
	if err != nil || duration != p.Duration {
		t.Fatalf("parallel real merge: duration=%v err=%v", duration, err)
	}
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode", "-i", output, "-map", "0:v:0", "-map", "0:a", "-f", "null", "-").Run(); err != nil {
		t.Fatal("parallel output full decode:", err)
	}
}
