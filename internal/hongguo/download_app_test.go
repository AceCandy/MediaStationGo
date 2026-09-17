package hongguo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadAppHighestCompatibleQuality(t *testing.T) {
	const material = "nb8T+Vu+EvdZvhL1X7kR90G6DvVfpSbcXaUm3luiJdyloHE="
	variant := func(quality, codec, address, key string, width, height int) map[string]any {
		return map[string]any{"main_url": address, "video_meta": map[string]any{"definition": quality, "codec_type": codec, "vwidth": width, "vheight": height}, "encrypt_info": map[string]any{"encrypt": true, "spade_a": key}}
	}
	rows := []any{variant("2160p", "bytevc2", "https://media.example/unsupported", material, 2160, 3840), variant("1440p", "h264", "file:///invalid", material, 1440, 2560), variant("1440p", "h264", "https://media.example/bad-key", "bad", 1440, 2560), variant("720p", "h264", "https://media.example/low", material, 720, 1280), variant("", "bytevc1", "https://media.example/high", material, 1080, 1922)}
	for _, indexed := range []bool{false, true} {
		model := map[string]any{"video_duration": 61.65, "video_list": rows}
		if indexed {
			model["video_list"] = map[string]any{"a": rows[0], "b": rows[1], "c": rows[2], "d": rows[3], "e": rows[4]}
		}
		var value any = model
		if indexed {
			encoded, _ := json.Marshal(model)
			value = string(encoded)
		}
		body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"video_model": value}})
		got, err := parseDownloadApp(body)
		if err != nil || got.Quality != 1080 || got.Width != 1080 || got.Height != 1922 || got.Codec != "hevc" || len(got.Key) != 16 || got.Duration != 61.65 {
			t.Fatalf("quality selection failed: %v", err)
		}
	}
	for _, body := range []string{`{}`, `{"code":101002,"message":"https://secret.example/token"}`, `{"code":429}`, `{"data":{"video_model":"bad"}}`, `{"code":"https://secret.example/token"}`} {
		_, err := parseDownloadApp([]byte(body))
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("missing or unsafe App error")
		}
		if strings.Contains(body, "101002") && !strings.Contains(err.Error(), "已下架") {
			t.Fatal("lost takedown reason")
		}
	}
}

func TestDownloadAppRequestAndPageErrors(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/novel/player/video_model/v1/" {
			if r.Method != http.MethodPost || r.Header.Get("X-Gorgon") == "" || r.Header.Get("X-Khronos") == "" {
				t.Fatal("missing signed POST")
			}
			var body struct {
				VideoID string `json:"video_id"`
				Biz     struct {
					All bool `json:"need_all_video_definition"`
				} `json:"biz_param"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.VideoID != "456" || !body.Biz.All {
				t.Fatal("wrong episode or quality request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":101002}`)), Header: make(http.Header), Request: r}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})})
	if _, err := client.ResolveDownloadSource(t.Context(), "123", "456", DownloadApp); err == nil || !strings.Contains(err.Error(), "已下架") {
		t.Fatal("missing App reason")
	}
	if _, err := client.ResolveDownloadSource(t.Context(), "123", "456", DownloadOfficial); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatal("404 hidden as network failure")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.ResolveDownloadSource(ctx, "123", "456", DownloadApp); err != context.Canceled {
		t.Fatal("cancelled request admitted")
	}
}

// 显式开启后只在测试临时目录下载第23集，完整解码后自动清理，不接触业务队列。
func TestDownloadAppLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HONGGUO_APP_LIVE") != "1" {
		t.Skip("opt-in live App download")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	httpClient := DownloadHTTPClient()
	defer httpClient.CloseIdleConnections()
	media, err := NewClient(httpClient).ResolveDownloadSource(ctx, "7684156322886978584", "7684158788726737944", DownloadApp)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DownloadRequest(ctx, httpClient, media)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	path := filepath.Join(t.TempDir(), "episode.mp4")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal("temporary file unavailable")
	}
	n, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n == 0 || resp.ContentLength >= 0 && n != resp.ContentLength {
		t.Fatal("incomplete download")
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-f", "mov"}
	if len(media.Key) > 0 {
		args = append(args, "-decryption_key", hex.EncodeToString(media.Key))
	}
	args = append(args, "-i", path, "-map", "0:v:0", "-map", "0:a:0?", "-f", "null", "-")
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		t.Fatal("full decode failed")
	}
	t.Logf("App episode 23 fully decoded: %dp %dx%d %s, %d bytes", media.Quality, media.Width, media.Height, media.Codec, n)
}
