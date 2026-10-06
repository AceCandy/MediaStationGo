package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestHuangGuoAIHLSRequiresValidVideoDuration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, duration := range []string{"", "0", "-1", "NaN", "+Inf", "invalid", "6"} {
		body, err := json.Marshal(map[string]any{
			"format":  map[string]string{"duration": "6"},
			"streams": []map[string]string{{"codec_type": "audio", "duration": "6"}, {"codec_type": "video", "duration": duration}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ffprobe"), []byte("#!/bin/sh\nprintf '%s' '"+string(body)+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		err = verifyDownloadMedia(t.Context(), "synthetic.mp4", 6, false, false, nil, nil, true)
		if duration == "6" {
			if err != nil {
				t.Fatalf("valid video duration rejected: %v", err)
			}
			// 检查完整校验选择所有音轨，而不是只解码首条音轨。
			if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = '0:a?' ]; then exit 0; fi\ndone\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := verifyDownloadMedia(t.Context(), "synthetic.mp4", 6, true, false, nil, nil, true); err != nil {
				t.Fatalf("HLS did not decode every audio track: %v", err)
			}
		} else if !errors.Is(err, errHongGuoDownloadSource) || !strings.Contains(err.Error(), "视频轨道缺少有效时长") {
			t.Fatalf("invalid video duration %q accepted or used container fallback: %v", duration, err)
		}
	}
}

// 仅显式启用时下载真实 HLS；数据库和所有媒体产物由测试隔离、清理。
func TestHuangGuoAIDownloadHLSLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HUANGGUOAI_LIVE") != "1" {
		t.Skip("live source check is opt-in")
	}
	s := newHuangGuoDownloadTaskTestService(t)
	cfg, err := s.Config(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	row := model.HuangGuoAIDownload{SourceID: "7912", Episode: 1, Root: cfg.Root, RelativePath: "synthetic/Season 01/S01E001.mp4", Status: "queued"}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	transfer, err := s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(t.Context())
	if err != nil || transfer == nil {
		t.Fatalf("transfer claim: %v", err)
	}
	s.run(t.Context(), *transfer)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil || row.Status != "waiting_verify" || !row.HLS {
		t.Fatalf("live HLS transfer: status=%s error=%s query=%v", row.Status, row.Error, err)
	}
	verification, err := s.repo.HuangGuoAI.ClaimHuangGuoAIVerification(t.Context())
	if err != nil || verification == nil || !verification.HLS {
		t.Fatalf("verification claim: %v", err)
	}
	s.run(t.Context(), *verification)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil || row.Status != "completed" {
		t.Fatalf("live HLS verification/publication: status=%s error=%s query=%v", row.Status, row.Error, err)
	}
	if row.VerifiedSize <= 0 || row.SHA256 == "" {
		t.Fatal("published without verification evidence")
	}
	t.Logf("live HLS merged, fully decoded and published: playlist %.3fs", row.Duration)
}

func TestHuangGuoAIDownloadHLSCompletenessAndDuration(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN") == "" {
		t.Skip("set MEDIASTATION_TEST_POSTGRES_DSN to run PostgreSQL tests")
	}
	segment := filepath.Join(t.TempDir(), "synthetic.ts")
	if err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "6", "-c:v", "mpeg2video", "-f", "mpegts", segment).Run(); err != nil {
		t.Fatal("synthetic HLS generation:", err)
	}
	data, err := os.ReadFile(segment)
	if err != nil {
		t.Fatal(err)
	}
	// 保留八秒前置音频，视频轨道只有六秒；用于区分总时长和视频时长策略。
	delayed := filepath.Join(t.TempDir(), "delayed.ts")
	if err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-itsoffset", "8", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=6", "-f", "lavfi", "-i", "sine=frequency=440:duration=14", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "mpegts", delayed).Run(); err != nil {
		t.Fatal("delayed synthetic media generation:", err)
	}
	delayedData, err := os.ReadFile(delayed)
	if err != nil {
		t.Fatal(err)
	}
	direct := filepath.Join(t.TempDir(), "delayed.mp4")
	if err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-probesize", "30000000", "-analyzeduration", "30000000", "-i", delayed, "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", direct).Run(); err != nil {
		t.Fatal("direct synthetic media generation:", err)
	}
	directData, err := os.ReadFile(direct)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name       string
		duration   int
		end        bool
		status     int
		short      bool
		decodeFail bool
		delayed    bool
		direct     bool
		failure    string
	}{
		{name: "page_duration_mismatch", duration: 6, end: true, status: 200},
		{name: "unfinished_playlist", duration: 6, status: 200, failure: "HLS 未提供完整 VOD 结束证据"},
		{name: "missing_segment", duration: 6, end: true, status: 404, failure: "HLS 资源 HTTP 404"},
		{name: "short_segment", duration: 6, end: true, status: 200, short: true, failure: "HLS 资源传输不完整"},
		{name: "file_duration_mismatch", duration: 12, end: true, status: 200, failure: "视频时长与来源不一致"},
		{name: "decode_failure", duration: 6, end: true, status: 200, decodeFail: true, failure: "音视频解码校验失败"},
		{name: "leading_audio_allowed", duration: 6, end: true, status: 200, delayed: true},
		{name: "leading_audio_wrong_video_duration", duration: 12, end: true, status: 200, delayed: true, failure: "视频时长与来源不一致"},
		{name: "leading_audio_decode_failure", duration: 6, end: true, status: 200, delayed: true, decodeFail: true, failure: "音视频解码校验失败"},
		{name: "direct_mp4_keeps_total_duration_check", duration: 6, status: 200, direct: true, failure: "视频时长与来源不一致"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newHuangGuoDownloadTaskTestService(t)
			cfg, err := s.Config(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			s.catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body []byte
				status := http.StatusOK
				switch req.URL.Path {
				case "/video/12/ep-7/":
					body = []byte(`<script id="videoInitialData">{"id":"12","title":"private-title","ep":7,"videoSrc":"https://example.com/index.m3u8?token=secret-value"}</script><script type="application/ld+json">{"@type":"VideoObject","duration":"PT4M39S"}</script>`)
					if tt.direct {
						body = []byte(`<script id="videoInitialData">{"id":"12","title":"private-title","ep":7,"videoSrc":"https://example.com/media.mp4?token=secret-value"}</script><script type="application/ld+json">{"@type":"VideoObject","duration":"PT6S"}</script>`)
					}
				case "/index.m3u8":
					body = []byte(fmt.Sprintf("#EXTM3U\n#EXTINF:%d,\nsegment.ts?token=secret-value\n", tt.duration))
					if tt.end {
						body = append(body, []byte("#EXT-X-ENDLIST\n")...)
					}
				case "/segment.ts":
					body, status = data, tt.status
					if tt.delayed {
						body = delayedData
					}
				case "/media.mp4":
					body = directData
				default:
					t.Errorf("unexpected source request path")
					status = http.StatusNotFound
				}
				length := int64(len(body))
				if tt.short && req.URL.Path == "/segment.ts" {
					length++
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: length, Header: http.Header{}, Request: req}, nil
			})})
			row := model.HuangGuoAIDownload{SourceID: "12", Episode: 7, Title: "private-title", Root: cfg.Root, RelativePath: "safe/Season 01/S01E007.mp4", Status: "queued"}
			if err := s.repo.DB.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			transfer, err := s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(t.Context())
			if err != nil || transfer == nil {
				t.Fatalf("transfer claim: %v", err)
			}
			s.run(t.Context(), *transfer)
			if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status == "waiting_verify" {
				if row.HLS != !tt.direct {
					t.Fatalf("duration policy not persisted: HLS=%v direct=%v", row.HLS, tt.direct)
				}
				if row.Duration != float64(tt.duration) {
					t.Fatalf("manifest duration lost: %.3f", row.Duration)
				}
				if tt.decodeFail {
					// 保留真实探测，只替换解码器，确认黄果发布前执行完整解码。
					dir := t.TempDir()
					if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nprintf 'Error while decoding' >&2\nexit 1\n"), 0700); err != nil {
						t.Fatal(err)
					}
					t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
				verification, err := s.repo.HuangGuoAI.ClaimHuangGuoAIVerification(t.Context())
				if err != nil || verification == nil {
					t.Fatalf("verification claim: %v", err)
				}
				if verification.HLS != row.HLS {
					t.Fatal("independent verification lost duration policy")
				}
				s.run(t.Context(), *verification)
				if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			log, err := s.tasks.ReadDefinitionLog(TaskKindHuangGuoAIDownload, "", 10000)
			if err != nil {
				t.Fatal(err)
			}
			if tt.failure == "" {
				if row.Status != "completed" || !strings.Contains(log.Content, "网页时长 279.000 秒") || !strings.Contains(log.Content, "清单时长 6.000 秒") {
					t.Fatalf("valid HLS rejected or warning missing: %s %s", row.Status, log.Content)
				}
				if tt.delayed {
					// 相同文件交给红果校验器仍应按总时长拒绝，不能把新策略传播到其他来源。
					path := filepath.Join(cfg.Root, "completed", row.RelativePath)
					if err := verifyHongGuoDownload(t.Context(), path, 6, true, false, nil, nil); err == nil {
						t.Fatal("leading audio was trimmed or total-duration policy changed")
					}
				}
			} else {
				if row.Status != "failed" || !strings.Contains(row.Error, tt.failure) || !strings.Contains(log.Content, row.Error) {
					t.Fatalf("failure not diagnosed: %s %s %s", row.Status, row.Error, log.Content)
				}
				if _, err := os.Stat(filepath.Join(cfg.Root, "completed", row.RelativePath)); !os.IsNotExist(err) {
					t.Fatal("failed media was published")
				}
			}
			for _, private := range []string{"private-title", "secret-value", "https://", cfg.Root} {
				if strings.Contains(row.Error, private) || strings.Contains(log.Content, private) {
					t.Fatal("sensitive download diagnostic leaked")
				}
			}
		})
	}
}

func TestHuangGuoAIDownloadUnexpectedErrorsRemainPrivate(t *testing.T) {
	s := newHuangGuoDownloadTaskTestService(t)
	cfg, err := s.Config(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	row := model.HuangGuoAIDownload{SourceID: "12", Episode: 7, Root: cfg.Root, RelativePath: "safe/Season 01/S01E007.mp4", Status: "queued"}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	transfer, err := s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(t.Context())
	if err != nil || transfer == nil {
		t.Fatalf("transfer claim: %v", err)
	}
	var injected atomic.Bool
	if err := s.repo.DB.Callback().Update().Before("gorm:update").Register("test:private-download-error", func(tx *gorm.DB) {
		if tx.Statement.Table == "huangguoai_downloads" && !injected.Swap(true) {
			tx.AddError(errors.New("private-title token=secret-value " + cfg.Root))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.repo.DB.Callback().Update().Remove("test:private-download-error") })
	s.run(t.Context(), *transfer)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil || row.Status != "failed" || row.Error != "黄果 AI 下载未完成" {
		t.Fatalf("unexpected failure was not kept private: %s %s %v", row.Status, row.Error, err)
	}
	log, err := s.tasks.ReadDefinitionLog(TaskKindHuangGuoAIDownload, "", 10000)
	if err != nil || !strings.Contains(log.Content, row.Error) {
		t.Fatalf("failure log missing: %v", err)
	}
	for _, private := range []string{"private-title", "secret-value", cfg.Root} {
		if strings.Contains(log.Content, private) {
			t.Fatal("unexpected private error leaked into task log")
		}
	}
}
