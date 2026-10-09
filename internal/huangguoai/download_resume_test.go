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
)

func TestDownloadHLSResumeAcrossAttempts(t *testing.T) {
	for _, change := range []string{"unchanged", "playlist", "key", "map", "corrupt", "truncated", "missing_record", "symlink", "direct_failure"} {
		t.Run(change, func(t *testing.T) {
			installParallelHLSTools(t)
			old, next := t.TempDir(), t.TempDir()
			var mu sync.Mutex
			calls := map[string]int{}
			failing, sequence, keyByte, mapByte := true, 1, byte(1), byte(1)
			completed := make(chan struct{})
			var completedOnce sync.Once
			c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
				mu.Lock()
				calls[req.URL.Path]++
				mu.Unlock()
				if change == "direct_failure" && !failing && req.URL.Path == "/input.m3u8" {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("partial-mp4")), ContentLength: 20, Header: http.Header{}, Request: req}, nil
				}
				body := []byte("seg")
				status := 200
				switch req.URL.Path {
				case "/input.m3u8":
					body = []byte(fmt.Sprintf("#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:1,\n0.ts\n#EXTINF:1,\n1.ts\n#EXTINF:1,\n2.ts\n#EXTINF:1,\n3.ts\n#EXT-X-ENDLIST\n", sequence))
				case "/init.mp4":
					body = []byte{mapByte}
				case "/key.bin":
					body = bytes.Repeat([]byte{keyByte}, 16)
				case "/2.ts":
					if failing {
						select {
						case <-completed:
						case <-req.Context().Done():
							return nil, req.Context().Err()
						}
						status = 404
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
			})})
			media := Media{URL: "https://example.com/input.m3u8", ExpectedDuration: 4}
			_, _, err := c.DownloadResuming(t.Context(), media, old, "", func(n int64) {
				// map=1，前两个分片各3字节；失败请求等到它们的完成记录已写入。
				if n >= 7 {
					completedOnce.Do(func() { close(completed) })
				}
			}, nil)
			var resume HLSResumeError
			if !errors.As(err, &resume) {
				t.Fatalf("failed transfer not resumable: %v", err)
			}
			for _, name := range []string{"key-0000.bin", "map-0000.mp4", "input.m3u8", "output.mp4", "segment-000002.m4s.resume"} {
				if _, err := os.Stat(filepath.Join(old, name)); !os.IsNotExist(err) {
					t.Fatalf("failed transfer kept dependency or incomplete segment: %s", name)
				}
			}
			first := filepath.Join(old, "segment-000000.m4s")
			switch change {
			case "playlist":
				sequence++
			case "key":
				keyByte++
			case "map":
				mapByte++
			case "corrupt":
				if err := os.WriteFile(first, []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "truncated":
				if err := os.Truncate(first, 1); err != nil {
					t.Fatal(err)
				}
			case "missing_record":
				if err := os.Remove(first + ".resume"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("seg"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(first); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, first); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			failing = false
			var progress []int64
			output, duration, err := c.DownloadResuming(t.Context(), media, next, old, func(n int64) { progress = append(progress, n) }, nil)
			if change == "direct_failure" {
				if err == nil || errors.As(err, &resume) || calls["/0.ts"] != 1 {
					t.Fatal("changed direct source retained or fetched HLS cache")
				}
				return
			}
			if err != nil || duration != 4 || output == "" {
				t.Fatalf("resume failed: %v", err)
			}
			want0, want1 := 1, 1
			if change != "unchanged" {
				want0 = 2
			}
			if change == "playlist" || change == "key" || change == "map" {
				want1 = 2
			}
			if calls["/0.ts"] != want0 || calls["/1.ts"] != want1 || calls["/2.ts"] != 2 || calls["/key.bin"] != 2 || calls["/init.mp4"] != 2 {
				t.Fatalf("wrong requests after %s: %v", change, calls)
			}
			if len(progress) != 4 || progress[3] != 13 {
				t.Fatalf("cache not included exactly once in progress: %v", progress)
			}
			after, err := os.ReadFile(first)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("resume changed previous lease files")
			}
			for _, path := range []string{first, filepath.Join(next, "segment-000000.m4s")} {
				info, err := os.Stat(path)
				if err != nil || info.Size() == 0 {
					t.Fatal("missing lease file")
				}
			}
		})
	}
}

func TestDownloadHLSResumeCancellationAndMergeFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "merge"} {
		t.Run(mode, func(t *testing.T) {
			installParallelHLSTools(t)
			if mode == "merge" {
				// PATH首目录为测试工具，不启动服务，也不调用真实媒体源。
				path := filepath.Join(strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0], "ffmpeg")
				if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			readStarted, closed := make(chan struct{}), make(chan struct{})
			c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
				if mode == "cancel" && req.URL.Path == "/0.ts" {
					select {
					case <-readStarted:
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
				}
				if mode == "cancel" && req.URL.Path == "/1.ts" {
					return &http.Response{StatusCode: 200, Body: &parallelFailureBody{ctx: req.Context(), started: readStarted, onClose: func() error { close(closed); return nil }}, ContentLength: 2, Header: http.Header{}, Request: req}, nil
				}
				body := "ok"
				if req.URL.Path == "/input.m3u8" {
					body = "#EXTM3U\n#EXTINF:2,\n0.ts\n#EXTINF:2,\n1.ts\n#EXT-X-ENDLIST\n"
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
			})})
			dir := t.TempDir()
			_, _, err := c.DownloadResuming(ctx, Media{URL: "https://example.com/input.m3u8"}, dir, "", func(int64) {
				if mode == "cancel" {
					cancel()
				}
			}, nil)
			var resume HLSResumeError
			if mode == "cancel" && (!errors.Is(err, context.Canceled) || !errors.As(err, &resume)) {
				t.Fatalf("cancel lost completed segment or original error: %v", err)
			}
			if mode == "cancel" {
				select {
				case <-closed:
				default:
					t.Fatal("returned before sibling closed")
				}
				for _, name := range []string{"segment-000001.ts", "segment-000001.ts.resume"} {
					if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
						t.Fatal("cancelled sibling left partial cache")
					}
				}
			}
			if mode == "merge" && (err == nil || errors.As(err, &resume)) {
				t.Fatalf("merge failure reused suspect media: %v", err)
			}
		})
	}
}

func TestHLSResumeSnapshotCheckpoint(t *testing.T) {
	for _, failure := range []string{"checkpoint", "copy_cancel", "copy_conflict"} {
		t.Run(failure, func(t *testing.T) {
			old, next := t.TempDir(), t.TempDir()
			name := "segment-000000.ts"
			if err := os.WriteFile(filepath.Join(old, name), []byte("complete"), 0600); err != nil {
				t.Fatal(err)
			}
			n, hash, err := hlsFileDigest(filepath.Join(old, name), maxSegmentBytes)
			if err != nil {
				t.Fatal(err)
			}
			cache := &hlsResumeCache{dir: old}
			if err := cache.save(name, hlsSegmentRecord{Identity: strings.Repeat("0", 64), Size: n, SHA256: hash}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "copy_cancel" {
				cancel()
			}
			if failure == "copy_conflict" {
				if err := os.WriteFile(filepath.Join(next, name), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			checkpointCalled := false
			c := NewClient(&http.Client{Transport: downloadTestTransport(func(*http.Request) (*http.Response, error) {
				t.Error("network started before successful checkpoint")
				return nil, errors.New("unexpected request")
			})})
			_, _, err = c.DownloadResuming(ctx, Media{}, next, old, nil, func() error {
				checkpointCalled = true
				data, err := os.ReadFile(filepath.Join(next, name))
				if err != nil || string(data) != "complete" {
					t.Fatal("checkpoint preceded complete snapshot")
				}
				if _, err := os.Stat(filepath.Join(next, name+".resume")); err != nil {
					t.Fatal("checkpoint preceded durable completion record")
				}
				return errors.New("private database error")
			})
			if err == nil || strings.Contains(err.Error(), "private") || checkpointCalled != (failure == "checkpoint") {
				t.Fatalf("unsafe checkpoint: called=%v err=%v", checkpointCalled, err)
			}
			data, err := os.ReadFile(filepath.Join(old, name))
			if err != nil || string(data) != "complete" {
				t.Fatal("failed snapshot changed old cache")
			}
			if failure == "copy_conflict" {
				data, err := os.ReadFile(filepath.Join(next, name))
				if err != nil || string(data) != "keep" {
					t.Fatal("snapshot overwrote existing file")
				}
			}
		})
	}
}

func TestDownloadHLSDurationReviewHandoff(t *testing.T) {
	segment := filepath.Join(t.TempDir(), "synthetic.ts")
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=4", "-c:v", "mpeg2video", "-f", "mpegts", segment).Run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(segment)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
		body := data
		if req.URL.Path == "/input.m3u8" {
			body = []byte("#EXTM3U\n#EXTINF:20,\nsegment.ts\n#EXT-X-ENDLIST\n")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
	})})
	media := Media{URL: "https://example.com/input.m3u8"}
	output, duration, err := c.Download(t.Context(), media, t.TempDir(), nil)
	var mismatch HLSDurationMismatchError
	if output != "" || duration != 0 || !errors.As(err, &mismatch) {
		t.Fatalf("strict caller received candidate: %s %.3f %v", output, duration, err)
	}
	output, duration, err = c.DownloadResuming(t.Context(), media, t.TempDir(), "", nil, nil)
	var resume HLSResumeError
	if output == "" || duration != 20 || !errors.As(err, &mismatch) || errors.As(err, &resume) {
		t.Fatalf("review handoff lost candidate or duration: %s %.3f %v", output, duration, err)
	}
	assertHLSMergeTracks(t, output, 4, 0, 40)
}
