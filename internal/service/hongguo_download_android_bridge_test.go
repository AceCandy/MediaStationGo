package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// 仅替代 ADB 边界；实际队列、持久化、传输、独立校验及发布仍走生产实现。
func TestHongGuoDownloadAndroidPipeline(t *testing.T) {
	for _, mode := range []string{"third-failure", "verify-recovery", "android-failure", "stop-resume", "local-error"} {
		t.Run(mode, func(t *testing.T) {
			s := newDownloadTestService(t)
			seed := seedDownload(t, s)
			fixture := downloadFixture(t)
			bin := t.TempDir()
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			started := filepath.Join(bin, "started")
			writeADB := func(block, failure bool) {
				response := `printf '%s\n' '{"type":"send","payload":{"kind":"model","video_id":"456","model":{"video_duration":1,"video_list":[{"main_url":"https://media.example/video","video_meta":{"codec_type":"h264","definition":"720p"}}]}}}'`
				if failure {
					response = `printf '%s\n' '{"type":"send","payload":{"kind":"error"}}'`
				}
				if block {
					response = "exec sleep 60"
				}
				script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in\n*'dumpsys package'*) echo 'versionCode=73932 minSdk=21' ;;\n*'ps -A -o'*) echo '123 1 com.phoenix.read' ;;\n*'exec-out'*) touch '%s'; printf '%%s\\n' '{\"type\":\"send\",\"payload\":{\"kind\":\"ready\"}}'; %s ;;\nesac\n", started, response)
				if err := os.WriteFile(filepath.Join(bin, "adb"), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeADB(mode == "stop-resume", mode == "android-failure")
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				if response := downloadDetailTestResponse(r); response != nil {
					return response, nil
				}
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
			})})
			s.client.EnableAndroidDownload("android.example:5555", bin)
			s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(fixture)), ContentLength: int64(len(fixture)), Header: make(http.Header), Request: r}, nil
			})}
			updates := map[string]any{"source_tries": 3, "source": hongguo.DownloadFallback}
			if mode == "third-failure" {
				updates["source_tries"], updates["source"] = 2, hongguo.DownloadApp
			}
			if mode == "local-error" {
				updates["root"] = filepath.Join(bin, "missing")
			}
			if mode == "verify-recovery" {
				stage := "downloading/old"
				if err := os.MkdirAll(filepath.Join(seed.Root, stage), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(seed.Root, stage, "source.bin"), []byte("corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
				updates["raw_size"], updates["staging_path"], updates["status"] = 7, filepath.Join(stage, "ready.mp4"), "waiting_verify"
			}
			if err := s.repo.DB.Model(&seed).Updates(updates).Error; err != nil {
				t.Fatal(err)
			}
			claim := s.repo.HongGuo.ClaimHongGuoDownload
			if mode == "verify-recovery" {
				claim = s.repo.HongGuo.ClaimHongGuoVerification
			}
			row, err := claim(t.Context())
			if err != nil || row == nil {
				t.Fatal("claim failed")
			}
			if mode == "stop-resume" {
				ctx, cancel := context.WithCancel(t.Context())
				joined := make(chan struct{})
				go func() { defer close(joined); s.run(ctx, *row) }()
				deadline := time.Now().Add(15 * time.Second)
				for {
					if _, err := os.Stat(started); err == nil {
						break
					}
					if time.Now().After(deadline) {
						cancel()
						<-joined
						t.Fatal("Android did not start")
					}
					time.Sleep(20 * time.Millisecond)
				}
				cancel()
				<-joined
				if err := s.repo.DB.First(&seed, "id = ?", seed.ID).Error; err != nil || seed.Status != "queued" || seed.SourceTries != 3 {
					t.Fatal("shutdown consumed Android budget")
				}
				writeADB(false, false)
				row, err = claim(t.Context())
				if err != nil || row == nil {
					t.Fatal("resume claim failed")
				}
			}
			finishDownloadTest(t, s, t.Context(), *row)
			if err := s.repo.DB.First(&seed, "id = ?", seed.ID).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "local-error" {
				if _, err := os.Stat(started); !os.IsNotExist(err) || seed.SourceTries != 3 || seed.Status != "failed" {
					t.Fatal("local error invoked Android")
				}
				return
			}
			want := "completed"
			if mode == "android-failure" {
				want = "failed"
			}
			if seed.Status != want || seed.Source != hongguo.DownloadAndroid || seed.SourceTries != 4 {
				t.Fatalf("mode=%s status=%s source=%s tries=%d error=%s", mode, seed.Status, seed.Source, seed.SourceTries, seed.Error)
			}
			if mode == "third-failure" && seed.SourceErrors[hongguo.DownloadFallback] == "" {
				t.Fatal("third source error not persisted")
			}
		})
	}
}

// 在隔离数据库中从第四次来源执行真实取模型、下载、完整校验及发布。
func TestHongGuoDownloadAndroidPipelineLive(t *testing.T) {
	address := os.Getenv("MEDIASTATION_TEST_HONGGUO_ANDROID_ADB")
	if address == "" {
		t.Skip("requires initialized dedicated Android")
	}
	s := newDownloadTestService(t)
	s.client.EnableAndroidDownload(address, os.Getenv("MEDIASTATION_TEST_HONGGUO_ANDROID_TOOLS"))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cfg, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row := model.HongGuoDownload{SourceID: "7655633797097999385", Episode: 81, VideoID: "7655636095014538302", Title: "离线下载校验测试", Root: cfg.Root, RelativePath: "android-test/S01E081.mp4", Status: "queued", SourceTries: 3, Source: hongguo.DownloadFallback}
	detail, err := s.client.Detail(ctx, row.SourceID)
	if err != nil || len(detail.VideoIDs) < 81 || detail.VideoIDs[80] != row.VideoID {
		t.Fatal("live target identity unavailable")
	}
	if len(detail.VideoIDs) > row.Episode {
		tail := model.HongGuoDownload{SourceID: row.SourceID, Episode: len(detail.VideoIDs), Root: cfg.Root, RelativePath: "android-test/tail.mp4", Status: "completed"}
		if err := s.repo.DB.Create(&tail).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := s.repo.HongGuo.ClaimHongGuoDownload(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim failed")
	}
	finishDownloadTest(t, s, ctx, *claimed)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "completed" || row.Source != hongguo.DownloadAndroid || row.SourceTries != 4 || row.Codec != "h264" || row.Width != 1280 || row.Height != 720 || row.Quality != 720 || row.Encrypted || !cfg.FullVerification {
		t.Fatalf("Android pipeline: status=%s source=%s tries=%d error=%s", row.Status, row.Source, row.SourceTries, row.Error)
	}
	t.Log("第81集Android取模型、Go传输、完整解码及发布通过")
}
