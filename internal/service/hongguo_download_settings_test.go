package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHongGuoDownloadCommandErrorClassification(t *testing.T) {
	// 获取真实非零退出状态，stderr 使用合成诊断，不依赖机器上 FFmpeg 的版本。
	err := exec.Command("sh", "-c", "exit 1").Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		stderr string
		source bool
	}{
		{"Invalid data found when processing input", true},
		{"moov atom not found", true},
		{"No space left on device", false},
		{"No such file or directory", false},
		{"Is a directory", false},
		{"Unknown local tool failure", false},
	} {
		exit.Stderr = []byte(tt.stderr)
		got := downloadMediaCommandError(exit, "媒体处理失败")
		if errors.Is(got, errHongGuoDownloadSource) != tt.source || strings.Contains(got.Error(), tt.stderr) {
			t.Fatalf("classification/leak: %s -> %v", tt.stderr, got)
		}
	}
	if errors.Is(downloadMediaCommandError(exec.ErrNotFound, "工具不可用"), errHongGuoDownloadSource) {
		t.Fatal("missing tool triggers fallback")
	}
}

func TestHongGuoDownloadCancelThenRetryRunningLease(t *testing.T) {
	s := newDownloadTestService(t)
	seed := seedDownload(t, s)
	media := downloadFixture(t)
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if response := downloadDetailTestResponse(r); response != nil {
			return response, nil
		}
		body := `_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":"456","video_player_info":{"main_url":"https://media.example/video","duration":1}}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	type pending struct {
		ctx     context.Context
		release chan struct{}
	}
	started := make(chan pending, 2)
	s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		item := pending{r.Context(), make(chan struct{})}
		started <- item
		select {
		case <-item.release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(media)), ContentLength: int64(len(media)), Header: make(http.Header), Request: r}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	t.Cleanup(func() { cancel(); s.Wait() })
	next := func() pending {
		t.Helper()
		select {
		case item := <-started:
			return item
		case <-time.After(10 * time.Second):
			t.Fatal("download did not start")
			return pending{}
		}
	}
	old := next()
	if err := s.Action(ctx, seed.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, seed.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	current := next()
	select {
	case <-old.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("old HTTP not cancelled by lease heartbeat")
	}
	if current.ctx.Err() != nil {
		t.Fatal("new lease was cancelled")
	}
	close(current.release)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var row model.HongGuoDownload
		if err := s.repo.DB.First(&row, "id = ?", seed.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status == "completed" {
			if row.Attempts != 2 {
				t.Fatalf("unexpected claims: %d", row.Attempts)
			}
			if err := verifyHongGuoDownload(ctx, filepath.Join(row.Root, "completed", row.RelativePath), 1, true, false, nil, nil); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry failed: %s %s", row.Status, row.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHongGuoDownloadSettings(t *testing.T) {
	s := newDownloadTestService(t)
	ctx := context.Background()
	cfg, err := s.Config(ctx)
	if err != nil || cfg.Concurrency != 3 || cfg.VerificationConcurrency != 2 || cfg.HardwareVerification || !cfg.FullVerification || cfg.Priority != hongguo.DownloadApp {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	concurrency, priority := 10, hongguo.DownloadFallback
	verification := 20
	hardware := true
	full := false
	patch := HongGuoDownloadConfigPatch{Concurrency: &concurrency, VerificationConcurrency: &verification, FullVerification: &full, HardwareVerification: &hardware, Priority: &priority}
	if _, err := s.SaveConfig(ctx, cfg.Root, patch); err != nil {
		t.Fatal(err)
	}
	// 模拟重启后从数据库读取，而不是依赖服务内缓存。
	restarted := NewHongGuoDownloadService(s.repo, s.catalog, s.tasks)
	cfg, err = restarted.Config(ctx)
	if err != nil || cfg.Concurrency != 10 || cfg.VerificationConcurrency != 20 || !cfg.HardwareVerification || cfg.FullVerification || cfg.Priority != priority {
		t.Fatalf("persisted: %+v %v", cfg, err)
	}
	if cfg, err = s.SaveConfig(ctx, cfg.Root, HongGuoDownloadConfigPatch{}); err != nil || cfg.Concurrency != 10 || cfg.VerificationConcurrency != 20 || !cfg.HardwareVerification || cfg.FullVerification || cfg.Priority != priority {
		t.Fatalf("legacy save: %+v %v", cfg, err)
	}
	for _, invalid := range []int{0, -1, 21} {
		if _, err := s.SaveConfig(ctx, cfg.Root, HongGuoDownloadConfigPatch{VerificationConcurrency: &invalid}); err == nil {
			t.Fatal("accepted invalid verification concurrency")
		}
	}
	for _, invalid := range []int{0, -1, 11} {
		concurrency = invalid
		if _, err := s.SaveConfig(ctx, filepath.Join(t.TempDir(), "unused"), patch); err == nil {
			t.Fatal("accepted invalid concurrency")
		}
	}
	concurrency, priority = 2, "https://arbitrary.example"
	if _, err := s.SaveConfig(ctx, cfg.Root, patch); err == nil {
		t.Fatal("accepted arbitrary source")
	}
	after, err := s.Config(ctx)
	if err != nil || after != cfg {
		t.Fatalf("invalid save changed settings: %+v %v", after, err)
	}
	hardware = false
	full = true
	if cfg, err = s.SaveConfig(ctx, cfg.Root, HongGuoDownloadConfigPatch{FullVerification: &full}); err != nil || !cfg.FullVerification {
		t.Fatalf("cannot re-enable full verification: %+v %v", cfg, err)
	}
	if cfg, err = s.SaveConfig(ctx, cfg.Root, HongGuoDownloadConfigPatch{HardwareVerification: &hardware}); err != nil || cfg.HardwareVerification {
		t.Fatalf("cannot disable hardware: %+v %v", cfg, err)
	}
}

func TestHongGuoDownloadSourcePriorityAndFallback(t *testing.T) {
	media := downloadFixture(t)
	for _, tt := range []struct {
		name, priority, bad string
		want                []string
	}{
		{"official", "official", "", []string{"official"}},
		{"fallback", "fallback", "", []string{"fallback", "fallback"}},
		{"fallback-parse", "fallback", "parse", []string{"fallback", "app", "official"}},
		{"official-http", "official", "http", []string{"official", "app", "fallback", "fallback"}},
		{"official-truncated", "official", "truncated", []string{"official", "app", "fallback", "fallback"}},
		{"official-corrupt", "official", "corrupt", []string{"official", "app", "fallback", "fallback"}},
		{"app", "app", "", []string{"app", "app"}},
		{"app-parse", "app", "parse", []string{"app", "fallback", "fallback"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newDownloadTestService(t)
			seedDownload(t, s)
			if err := s.repo.Setting.Set(context.Background(), hongGuoDownloadPriorityKey, tt.priority); err != nil {
				t.Fatal(err)
			}
			var visits []string
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				if response := downloadDetailTestResponse(r); response != nil {
					return response, nil
				}
				source := "official"
				body := `_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":"456","video_player_info":{"main_url":"https://media.example/official","duration":1}}}}`
				if strings.Contains(r.URL.Path, "/api/hongguo/play") {
					source = "fallback"
					body = `{"key_urls":[{"name":"1080p","src":"https://media.example/fallback","kid":"00000000000000000000000000000000","spade_a":"nb8T+Vu+EvdZvhL1X7kR90G6DvVfpSbcXaUm3luiJdyloHE="}]}`
				}
				if r.URL.Path == "/novel/player/video_model/v1/" {
					source = "app"
					body = `{"code":101002}`
					if tt.priority == "app" {
						body = `{"code":0,"data":{"video_model":{"duration":1,"video_list":[{"main_url":"https://media.example/app","video_meta":{"definition":"1080p","codec_type":"h264","vwidth":1080,"vheight":1920},"encrypt_info":{"encrypt":true,"spade_a":"nb8T+Vu+EvdZvhL1X7kR90G6DvVfpSbcXaUm3luiJdyloHE="}}]}}}`
					}
				}
				visits = append(visits, source)
				if source == tt.priority && tt.bad == "parse" {
					body = "{}"
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				data, status, length := media, 200, int64(len(media))
				if r.URL.Path == "/"+tt.priority {
					switch tt.bad {
					case "http":
						status = 403
					case "truncated":
						data = data[:16]
					case "corrupt":
						data = bytes.Repeat([]byte("bad"), 100)
						length = int64(len(data))
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: length, Header: make(http.Header), Request: r}, nil
			})}
			row, err := s.repo.HongGuo.ClaimHongGuoDownload(context.Background())
			if err != nil || row == nil {
				t.Fatal(err)
			}
			finishDownloadTest(t, s, context.Background(), *row)
			var saved model.HongGuoDownload
			if err := s.repo.DB.First(&saved, "id = ?", row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.Status != "completed" || strings.Join(visits, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("status=%s error=%s visits=%v", saved.Status, saved.Error, visits)
			}
			if saved.Width <= 0 || saved.Height <= 0 || saved.Codec == "" {
				t.Fatal("verified media details missing")
			}
			if tt.bad != "" && saved.SourceErrors[tt.priority] == "" {
				t.Fatal("source error history missing")
			}
			if tt.priority == "fallback" && tt.bad == "parse" && !strings.Contains(saved.SourceErrors["app"], "已下架") {
				t.Fatal("App failure lost after official fallback")
			}
			// 本地目录失效不应继续访问另一个来源。
			row.Root = filepath.Join(t.TempDir(), "missing")
			visits = nil
			s.run(context.Background(), *row)
			if len(visits) != 0 {
				t.Fatalf("local error resolved sources: %v", visits)
			}
		})
	}
}

func TestHongGuoDownloadThreeSourceOrder(t *testing.T) {
	for _, priority := range []string{hongguo.DownloadApp, hongguo.DownloadFallback, hongguo.DownloadOfficial} {
		want := hongguo.DownloadSources(priority)
		previous := ""
		for tries := 0; tries < maxHongGuoSourceTries; tries++ {
			got := nextHongGuoDownloadSource(priority, previous, tries)
			if got != want[tries%3] {
				t.Fatalf("priority=%s attempt=%d source=%s", priority, tries, got)
			}
			previous = got
		}
	}
}

func TestHongGuoDownloadDiagnosticsAndRetry(t *testing.T) {
	s := newDownloadTestService(t)
	row := seedDownload(t, s)
	ctx := t.Context()
	if err := s.repo.DB.Model(&row).Updates(map[string]any{"status": "failed", "source": "app", "source_errors": `{"app":"当前视频已下架（101002）"}`, "quality": 1080, "width": 1080, "height": 1922, "codec": "hevc", "source_tries": 9}).Error; err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.ListEpisodes(ctx, row.SourceID, 1)
	if err != nil || len(rows) != 1 || rows[0].SourceErrors["app"] == "" {
		t.Fatal("diagnostics did not survive storage/list")
	}
	encoded, err := json.Marshal(rows[0])
	if err != nil || !strings.Contains(string(encoded), `"source":"app"`) || !strings.Contains(string(encoded), `"width":1080`) {
		t.Fatal("public metadata missing")
	}
	for _, hidden := range []string{`"video_id"`, `"root"`, `"encrypted"`, `"lease_token"`, `"staging_path"`} {
		if strings.Contains(string(encoded), hidden) {
			t.Fatal("private field exposed")
		}
	}
	if err := s.Action(ctx, row.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	var saved model.HongGuoDownload
	if err := s.repo.DB.First(&saved, "id = ?", row.ID).Error; err != nil || saved.Status != "queued" || saved.SourceTries != 0 || len(saved.SourceErrors) != 0 {
		t.Fatal("manual retry retained stale failures")
	}
}

func TestHongGuoDownloadDynamicConcurrencyAndShutdown(t *testing.T) {
	s := newDownloadTestService(t)
	seed := seedDownload(t, s)
	for i := 2; i <= 6; i++ {
		row := model.HongGuoDownload{SourceID: "123", VideoID: fmt.Sprint(455 + i), Episode: i, Title: seed.Title, Root: seed.Root, RelativePath: fmt.Sprintf("work/S01E%03d.mp4", i), Status: "queued"}
		if err := s.repo.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	media := downloadFixture(t)
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if response := downloadDetailTestResponse(r); response != nil {
			return response, nil
		}
		id := filepath.Base(r.URL.Path)
		body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":%q,"video_player_info":{"main_url":"https://media.example/%s","duration":1}}}}`, id, id)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	type request struct {
		id      string
		release chan struct{}
	}
	started := make(chan request, 6)
	s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		item := request{r.URL.Path, make(chan struct{})}
		started <- item
		select {
		case <-item.release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(media)), ContentLength: int64(len(media)), Header: make(http.Header), Request: r}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	t.Cleanup(func() { cancel(); s.Wait() })
	seen := map[string]bool{}
	next := func() request {
		t.Helper()
		select {
		case item := <-started:
			if seen[item.id] {
				t.Fatalf("duplicate claim: %s", item.id)
			}
			seen[item.id] = true
			return item
		case <-time.After(10 * time.Second):
			t.Fatal("download did not start")
			return request{}
		}
	}
	noNext := func() {
		t.Helper()
		select {
		case item := <-started:
			t.Fatalf("exceeded concurrency: %s", item.id)
		case <-time.After(200 * time.Millisecond):
		}
	}
	first, second, third := next(), next(), next()
	noNext()
	setLimit := func(n int) {
		t.Helper()
		if _, err := s.SaveConfig(ctx, seed.Root, HongGuoDownloadConfigPatch{Concurrency: &n}); err != nil {
			t.Fatal(err)
		}
	}
	setLimit(1)
	close(first.release)
	close(second.release)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var count int64
		if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("status = ?", "completed").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("active downloads were interrupted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	noNext()
	close(third.release)
	next()
	noNext()
	setLimit(2)
	next()
	noNext()
	cancel()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not join workers")
	}
	var rows []model.HongGuoDownload
	if err := s.repo.DB.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	completed, queued := 0, 0
	for _, row := range rows {
		switch row.Status {
		case "completed":
			completed++
		case "queued":
			queued++
		default:
			t.Fatalf("shutdown state: %s", row.Status)
		}
		if row.Attempts > 1 {
			t.Fatal("task claimed twice")
		}
	}
	if completed != 3 || queued != 3 {
		t.Fatalf("completed=%d queued=%d", completed, queued)
	}
	if _, err := os.Stat(filepath.Join(seed.Root, "completed")); err != nil {
		t.Fatal(err)
	}
}
