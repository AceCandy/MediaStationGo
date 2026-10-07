package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHuangGuoAIDownloadResumeRetryAndPublish(t *testing.T) {
	s := newHuangGuoDownloadTaskTestService(t)
	cfg, err := s.Config(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-c:v", "mpeg2video", "-g", "10", "-c:a", "aac", "-f", "hls", "-hls_time", "1", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(source, "part-%03d.ts"), filepath.Join(source, "index.m3u8")).Run(); err != nil {
		t.Fatal("generate synthetic HLS:", err)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	failing, resolveFail := true, false
	var firstStage string
	s.catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Path]++
		mu.Unlock()
		var body []byte
		status := 200
		if req.URL.Path == "/video/71/" {
			if resolveFail {
				return nil, errors.New("synthetic unavailable source")
			}
			body = []byte(`<script id="videoInitialData">{"id":"71","title":"Synthetic","ep":1,"videoSrc":"https://example.com/index.m3u8"}</script><script type="application/ld+json">{"@type":"VideoObject","duration":"PT4S"}</script>`)
		} else if req.URL.Path == "/part-002.ts" && failing {
			// 等到前两片的完成记录已落盘，再模拟终止故障。
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for {
				_, first := os.Stat(filepath.Join(firstStage, "segment-000000.ts.resume"))
				_, second := os.Stat(filepath.Join(firstStage, "segment-000001.ts.resume"))
				if first == nil && second == nil {
					break
				}
				select {
				case <-tick.C:
				case <-timer.C:
					return nil, errors.New("synthetic cache checkpoint timeout")
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			status = 404
		} else {
			var e error
			body, e = os.ReadFile(filepath.Join(source, filepath.Base(req.URL.Path)))
			if e != nil {
				t.Error("unexpected synthetic request")
				status = 404
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
	})})
	row := model.HuangGuoAIDownload{SourceID: "71", Episode: 1, Root: cfg.Root, RelativePath: "Synthetic [huangguoai-71]/Synthetic.mp4", Status: "queued"}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	claim := func() *model.HuangGuoAIDownload {
		t.Helper()
		lease, err := s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(t.Context())
		if err != nil || lease == nil || lease.ID != row.ID {
			t.Fatalf("claim: %v", err)
		}
		return lease
	}
	reload := func() {
		t.Helper()
		if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := claim()
	firstStage = filepath.Join(cfg.Root, "downloading", first.ID+"-"+first.LeaseToken)
	s.run(t.Context(), *first)
	reload()
	if row.Status != "failed" || row.RawSize != 0 || !validHuangGuoAIStage(row.StagingPath, row.ID) {
		t.Fatalf("failed transfer checkpoint: status=%s size=%d", row.Status, row.RawSize)
	}
	oldCheckpoint := row.StagingPath
	if _, err := os.Stat(filepath.Join(firstStage, "segment-000000.ts.resume")); err != nil {
		t.Fatal("failed transfer deleted completed segment")
	}
	if _, err := os.Stat(filepath.Join(cfg.Root, "completed", row.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("partial transfer published")
	}
	// 重新解析失败也不能改变缓存检查点；随后再次显式重试。
	resolveFail = true
	if err := s.Action(t.Context(), row.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	second := claim()
	if second.LeaseToken == first.LeaseToken {
		t.Fatal("retry reused lease")
	}
	s.run(t.Context(), *second)
	reload()
	if row.Status != "failed" || row.StagingPath != oldCheckpoint {
		t.Fatal("resolve failure lost previous checkpoint")
	}
	resolveFail, failing = false, false
	if err := s.Action(t.Context(), row.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	third := claim()
	s.run(t.Context(), *third)
	reload()
	if row.Status != "waiting_verify" || !row.HLS || row.RawSize == 0 || row.Duration != 4 {
		t.Fatalf("resumed transfer bypassed handoff: %s", row.Status)
	}
	if calls["/part-000.ts"] != 1 || calls["/part-001.ts"] != 1 || calls["/part-002.ts"] != 2 {
		t.Fatalf("retry refetched completed segments: %v", calls)
	}
	if _, err := os.Stat(firstStage); !os.IsNotExist(err) {
		t.Fatal("old checkpoint not retired after durable handoff")
	}
	entries, err := os.ReadDir(filepath.Join(cfg.Root, filepath.Dir(row.StagingPath)))
	if err != nil || len(entries) != 1 || entries[0].Name() != "ready.mp4" {
		t.Fatal("segment cache survived complete handoff")
	}
	verification, err := s.repo.HuangGuoAI.ClaimHuangGuoAIVerification(t.Context())
	if err != nil || verification == nil {
		t.Fatalf("independent verification claim: %v", err)
	}
	s.run(t.Context(), *verification)
	reload()
	if row.Status != "completed" {
		t.Fatalf("resumed media not independently verified and published: %s", row.Status)
	}
	entries, err = os.ReadDir(filepath.Join(cfg.Root, "downloading"))
	if err != nil || len(entries) != 0 {
		t.Fatal("attempt files remain after publication")
	}
}
