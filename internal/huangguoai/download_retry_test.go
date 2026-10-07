package huangguoai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 私密底层错误仅用于验证公开诊断不会保留原文。
type interruptedResourceReader struct{ err error }

func (r interruptedResourceReader) Read([]byte) (int, error) { return 0, r.err }

func TestFetchResourceRetry(t *testing.T) {
	for _, tt := range []struct {
		name, failure, want string
		attempts            int
		persistent          bool
	}{
		{name: "short_recovers", failure: "short", attempts: 2},
		{name: "read_recovers", failure: "read", attempts: 2},
		{name: "network_recovers", failure: "network", attempts: 2},
		{name: "http_recovers", failure: "http", attempts: 2},
		{name: "range_recovers", failure: "range", attempts: 2},
		{name: "short_exhausted", failure: "short", attempts: 3, persistent: true, want: "长度不足"},
		{name: "read_exhausted", failure: "read", attempts: 3, persistent: true, want: "读取中断"},
		{name: "http_exhausted", failure: "http", attempts: 3, persistent: true, want: "HTTP 503"},
		{name: "not_found", failure: "404", attempts: 1, want: "HTTP 404"},
		{name: "invalid_range", failure: "invalid_range", attempts: 1, want: "字节范围响应无效"},
		{name: "oversize_header", failure: "oversize_header", attempts: 1, want: "超出大小限制"},
		{name: "oversize_body", failure: "oversize_body", attempts: 1, want: "超出大小限制"},
		{name: "disk_error", failure: "disk", attempts: 1, want: "写盘失败"},
		{name: "private_network", failure: "private", attempts: 1, want: "网络请求失败"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "segment.ts")
			calls := 0
			resource := Resource{URL: "https://example.com/segment?token=private-token"}
			if tt.failure == "range" || tt.failure == "invalid_range" {
				resource.Offset, resource.Length = 4, 2
			}
			c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("Referer") != "https://example.com/" || req.Header.Get("User-Agent") != "Mozilla/5.0" {
					t.Fatal("resource headers changed")
				}
				if resource.Length > 0 && req.Header.Get("Range") != "bytes=4-5" {
					t.Fatal("range changed during retry")
				}
				resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), ContentLength: 2, Header: http.Header{}, Request: req}
				if resource.Length > 0 {
					resp.StatusCode = 206
					resp.Header.Set("Content-Range", "bytes 4-5/8")
				}
				if calls > 1 && !tt.persistent {
					return resp, nil
				}
				switch tt.failure {
				case "short", "range":
					resp.Body = io.NopCloser(strings.NewReader("o"))
				case "read":
					resp.Body = io.NopCloser(io.MultiReader(strings.NewReader("o"), interruptedResourceReader{errors.New("private-title private-token /private/path")}))
				case "network":
					return nil, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("private-title private-token /private/path")}
				case "http":
					resp.StatusCode = 503
				case "404":
					resp.StatusCode = 404
				case "invalid_range":
					resp.Header.Set("Content-Range", "bytes 0-1/8")
				case "oversize_header":
					resp.ContentLength = 5
				case "oversize_body":
					resp.ContentLength = -1
					resp.Body = io.NopCloser(strings.NewReader("oversize"))
				case "disk":
					resp.Body = io.NopCloser(interruptedResourceReader{&os.PathError{Op: "write", Path: "/private/path", Err: errors.New("private-token")}})
				case "private":
					return nil, errors.New("private-title private-token /private/path")
				}
				return resp, nil
			})})
			n, err := c.fetchResource(t.Context(), resource, "https://example.com/", path, 4)
			if calls != tt.attempts {
				t.Fatalf("attempts=%d want=%d", calls, tt.attempts)
			}
			if tt.want == "" {
				data, readErr := os.ReadFile(path)
				if err != nil || n != 2 || readErr != nil || string(data) != "ok" {
					t.Fatalf("retry did not replace partial data: n=%d err=%v read=%v", n, err, readErr)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.want) || n != 0 {
					t.Fatalf("unexpected safe error: n=%d err=%v", n, err)
				}
				if tt.attempts == 3 && !strings.Contains(err.Error(), "已尝试 3 次") {
					t.Fatal("missing retry count")
				}
				if _, e := os.Stat(path); !os.IsNotExist(e) {
					t.Fatal("failed resource left a partial file")
				}
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("diagnostic exposed private data")
			}
		})
	}
}

func TestFetchResourceCancellationAndExistingFile(t *testing.T) {
	t.Run("cancel_backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			time.AfterFunc(20*time.Millisecond, cancel)
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
		})})
		start := time.Now()
		_, err := c.fetchResource(ctx, Resource{URL: "https://example.com/segment"}, "", filepath.Join(t.TempDir(), "segment"), 4)
		if !errors.Is(err, context.Canceled) || calls != 1 || time.Since(start) > time.Second {
			t.Fatalf("cancellation did not stop backoff: calls=%d err=%v", calls, err)
		}
	})
	t.Run("cancel_read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		path := filepath.Join(t.TempDir(), "segment")
		calls := 0
		c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(resourceCancelReader{cancel}), ContentLength: 2, Header: http.Header{}, Request: req}, nil
		})})
		_, err := c.fetchResource(ctx, Resource{URL: "https://example.com/segment"}, "", path, 4)
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("read cancellation retried or lost: calls=%d err=%v", calls, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("cancelled read left partial data")
		}
	})
	t.Run("existing_file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "existing")
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		calls := 0
		c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), ContentLength: 2, Header: http.Header{}, Request: req}, nil
		})})
		_, err := c.fetchResource(t.Context(), Resource{URL: "https://example.com/segment"}, "", path, 4)
		data, readErr := os.ReadFile(path)
		if err == nil || calls != 1 || readErr != nil || string(data) != "keep" {
			t.Fatalf("existing file was changed: calls=%d err=%v read=%v", calls, err, readErr)
		}
	})
}

func TestDownloadHLSRetryPreservesCompletedSegments(t *testing.T) {
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "ffmpeg"), []byte("#!/bin/sh\nfor output; do :; done\nprintf synthetic > \"$output\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	calls := map[string]int{}
	c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
		calls[req.URL.Path]++
		body := []byte("ok")
		if req.URL.Path == "/input.m3u8" {
			body = []byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:1,\nfirst.ts\n#EXTINF:1,\nsecond.ts\n#EXT-X-ENDLIST\n")
		}
		if req.URL.Path == "/key.bin" {
			body = bytes.Repeat([]byte{1}, 16)
		}
		length := int64(len(body))
		if (req.URL.Path == "/second.ts" || req.URL.Path == "/key.bin" || req.URL.Path == "/init.mp4") && calls[req.URL.Path] == 1 {
			body = body[:len(body)-1]
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: length, Header: http.Header{}, Request: req}, nil
	})})
	var progress []int64
	_, duration, err := c.Download(t.Context(), Media{URL: "https://example.com/input.m3u8"}, t.TempDir(), func(n int64) { progress = append(progress, n) })
	if err != nil || duration != 2 || calls["/first.ts"] != 1 || calls["/second.ts"] != 2 || calls["/init.mp4"] != 2 || calls["/key.bin"] != 2 {
		t.Fatalf("completed segment was refetched or retry failed: calls=%v err=%v", calls, err)
	}
	if len(progress) != 2 || progress[0] != 4 || progress[1] != 6 {
		t.Fatalf("retry double counted progress: %v", progress)
	}
}

// 在响应体读取阶段触发取消，覆盖暂存文件已创建后的退出路径。
type resourceCancelReader struct{ cancel context.CancelFunc }

func (r resourceCancelReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, context.Canceled
}

func TestFetchResourceHTTPRetryClassification(t *testing.T) {
	for _, ranged := range []bool{false, true} {
		for _, status := range []int{408, 429, 500, 502, 503, 504, 403, 404} {
			c := NewClient(&http.Client{Transport: downloadTestTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
			})})
			resource := Resource{URL: "https://example.com/segment"}
			if ranged {
				resource.Offset, resource.Length = 4, 2
			}
			_, retry, err := c.fetchResourceOnce(t.Context(), resource, "", filepath.Join(t.TempDir(), "segment"), 4)
			if err == nil || retry != (status != 403 && status != 404) {
				t.Fatalf("HTTP %d range=%v misclassified: retry=%v err=%v", status, ranged, retry, err)
			}
		}
	}
}
