package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestHuangGuoAIDownloadResumeDirectoryOwnership(t *testing.T) {
	for _, mode := range []string{"failed", "failed_work", "shutdown", "expired", "cancelled", "cancelled_work", "checkpoint_failure"} {
		t.Run(mode, func(t *testing.T) {
			s := newHuangGuoDownloadTaskTestService(t)
			cfg, err := s.Config(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			row := model.HuangGuoAIDownload{SourceID: "71", Episode: 1, Root: cfg.Root, RelativePath: "synthetic.mp4", Status: "failed"}
			if err := s.repo.DB.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			oldToken := uuid.NewString()
			stage := filepath.Join("downloading", row.ID+"-"+oldToken)
			oldDir := filepath.Join(cfg.Root, stage)
			if err := os.MkdirAll(oldDir, 0700); err != nil {
				t.Fatal(err)
			}
			segment := "segment-000000.ts"
			data := []byte("complete")
			if err := os.WriteFile(filepath.Join(oldDir, segment), data, 0600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			record, err := json.Marshal(map[string]any{"Identity": strings.Repeat("0", 64), "Size": len(data), "SHA256": hex.EncodeToString(digest[:])})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(oldDir, segment+".resume"), record, 0600); err != nil {
				t.Fatal(err)
			}
			oldFile, err := os.Open(filepath.Join(oldDir, segment))
			if err != nil {
				t.Fatal(err)
			}
			defer oldFile.Close()
			oldInfo, err := oldFile.Stat()
			if err != nil {
				t.Fatal(err)
			}
			row.StagingPath = filepath.Join(stage, "ready.mp4")
			if mode == "shutdown" {
				row.Status = "queued"
			}
			if mode == "expired" || strings.HasPrefix(mode, "cancelled") {
				row.Status = "downloading"
				row.LeaseToken = oldToken
				expired := time.Now().Add(-time.Minute)
				row.LeaseUntil = &expired
			}
			if err := s.repo.DB.Save(&row).Error; err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(mode, "cancelled") {
				if err := s.Action(t.Context(), row.ID, "cancel"); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "expired" && mode != "shutdown" {
				if strings.HasSuffix(mode, "_work") {
					if n, err := s.WorkAction(t.Context(), row.SourceID, "retry"); err != nil || n != 1 {
						t.Fatalf("work retry: %d %v", n, err)
					}
				} else {
					if err := s.Action(t.Context(), row.ID, "retry"); err != nil {
						t.Fatal(err)
					}
				}
			}
			lease, err := s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(t.Context())
			if err != nil || lease == nil {
				t.Fatalf("claim: %v", err)
			}
			wantReuse := mode != "expired" && !strings.HasPrefix(mode, "cancelled")
			if lease.ReuseStaging != wantReuse || lease.LeaseToken == oldToken {
				t.Fatal("claim did not distinguish stopped worker from expired ownership")
			}
			mediaRequests := 0
			s.catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/video/71/" {
					body := `<script id="videoInitialData">{"id":"71","title":"Synthetic","ep":1,"videoSrc":"https://example.com/index.m3u8"}</script><script type="application/ld+json">{"@type":"VideoObject","duration":"PT4S"}</script>`
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
				}
				mediaRequests++
				return nil, errors.New("synthetic playlist unavailable")
			})})
			if mode == "checkpoint_failure" {
				if err := s.repo.DB.Callback().Update().Before("gorm:update").Register("test:resume-checkpoint", func(tx *gorm.DB) {
					values, _ := tx.Statement.Dest.(map[string]any)
					if tx.Statement.Table == "huangguoai_downloads" && len(values) == 1 && values["staging_path"] != nil {
						tx.AddError(errors.New("synthetic checkpoint failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.repo.DB.Callback().Update().Remove("test:resume-checkpoint") })
			}
			s.run(t.Context(), *lease)
			checkpoint := row.StagingPath
			if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil || row.Status != "failed" {
				t.Fatalf("failure checkpoint: %v", err)
			}
			info, err := os.Stat(filepath.Join(cfg.Root, filepath.Dir(row.StagingPath), segment))
			if err != nil || os.SameFile(oldInfo, info) != wantReuse {
				t.Fatal("cache was copied for stopped worker or shared with expired worker")
			}
			if (row.StagingPath == checkpoint) != wantReuse {
				t.Fatal("wrong directory handoff")
			}
			if mode == "checkpoint_failure" && mediaRequests != 0 {
				t.Fatal("network started before successful checkpoint")
			}
		})
	}
}

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
	if !third.ReuseStaging {
		t.Fatal("normal failed retry did not allow exclusive cache reuse")
	}
	s.run(t.Context(), *third)
	reload()
	if row.Status != "waiting_verify" || !row.HLS || row.RawSize == 0 || row.Duration != 4 {
		t.Fatalf("resumed transfer bypassed handoff: %s", row.Status)
	}
	if calls["/part-000.ts"] != 1 || calls["/part-001.ts"] != 1 || calls["/part-002.ts"] != 2 {
		t.Fatalf("retry refetched completed segments: %v", calls)
	}
	if row.StagingPath != oldCheckpoint {
		t.Fatal("normal failed retry moved or copied the cache directory")
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
