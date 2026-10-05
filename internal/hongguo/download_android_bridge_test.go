package hongguo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAndroidModelIdentityAndAddress(t *testing.T) {
	if androidMainPID("PID PPID NAME\n4843 4565 com.phoenix.read\n4565 172 com.phoenix.read\n4835 4565 com.phoenix.read\n") != "4565" {
		t.Fatal("selected a same-name fork instead of App main process")
	}
	for _, line := range []string{
		`{"type":"send","payload":{"kind":"model","video_id":"456","model":{}}}`,
		`{"type":"send","payload":{"kind":"model","video_id":"123","model":"secret"}}`,
		`{"type":"error","description":"private upstream text"}`,
	} {
		if _, ok := parseAndroidEvent([]byte(line), "123"); ok {
			t.Fatal("accepted wrong identity or envelope")
		}
	}
	event, ok := parseAndroidEvent([]byte(`{"type":"send","payload":{"kind":"model","video_id":"123","model":{"video_id":"v0different","video_list":[{"main_url":"https://example.com/media","video_meta":{"codec_type":"h264","definition":"720p"}}]}}}`), "123")
	if !ok || event.VideoID != "123" {
		t.Fatal("canonical VOD ID must not replace numeric Map identity")
	}
	for _, address := range []string{"host;echo:5555", "-host:5555", "host:0", "host:65536", "host", "host:5555/path"} {
		if validAndroidAddress(address) {
			t.Fatal("accepted invalid control address")
		}
	}
}

func TestAndroidWaitingCancellation(t *testing.T) {
	c := NewClient(nil)
	c.EnableAndroidDownload("127.0.0.1:5555", "unused")
	c.android.gate <- struct{}{}
	ctx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	if _, err := c.ResolveDownloadSource(ctx, "123", "456", DownloadAndroid); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting cancellation: %v", err)
	}
	if len(c.android.gate) != 1 {
		t.Fatal("cancelled waiter released another request's slot")
	}
}

// 显式启用才连接真实专用 Android；媒体与模型不进入仓库或日志。
func TestDownloadAndroidLive(t *testing.T) {
	address := os.Getenv("MEDIASTATION_TEST_HONGGUO_ANDROID_ADB")
	if address == "" {
		t.Skip("requires initialized dedicated Android")
	}
	client := DownloadHTTPClient()
	c := NewClient(client)
	c.EnableAndroidDownload(address, os.Getenv("MEDIASTATION_TEST_HONGGUO_ANDROID_TOOLS"))
	work, err := c.Detail(t.Context(), "7655633797097999385")
	if err != nil || len(work.VideoIDs) < 82 {
		t.Fatalf("episode inventory unavailable: %v", err)
	}
	// 活跃取模型期间超时后，下一目标必须仍可取模型。
	cancelCtx, stop := context.WithTimeout(t.Context(), 7*time.Second)
	_, err = c.ResolveDownloadSource(cancelCtx, work.SourceID, work.VideoIDs[80], DownloadAndroid)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active timeout: %v", err)
	}
	for _, index := range []int{80, 81} {
		media, err := c.ResolveDownloadSource(t.Context(), work.SourceID, work.VideoIDs[index], DownloadAndroid)
		if err != nil {
			t.Fatalf("episode %d: %v", index+1, err)
		}
		if media.Codec == "" || media.Quality <= 0 || media.Width <= 0 || media.Height <= 0 || media.Duration <= 0 {
			t.Fatalf("episode %d invalid projection: codec=%s quality=%d size=%dx%d", index+1, media.Codec, media.Quality, media.Width, media.Height)
		}
		if index == 80 && (media.Codec != "h264" || media.Quality != 720 || media.Width != 1280 || media.Height != 720) {
			t.Fatal("episode 81 changed from verified official projection")
		}
		resp, err := DownloadRequest(t.Context(), client, media)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "episode.mp4")
		file, err := os.Create(path)
		if err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(file, hash), resp.Body)
		file.Close()
		resp.Body.Close()
		if copyErr != nil || n == 0 {
			t.Fatal("incomplete media")
		}
		if index == 80 && hex.EncodeToString(hash.Sum(nil)) != "110a33a4ecf8aa9286a5bd518518466e499ad3533ee2ea8637aac072c04fcb42" {
			t.Fatal("episode 81 differs from independently verified official file")
		}
		args := []string{"-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe"}
		if len(media.Key) > 0 {
			args = append(args, "-decryption_key", hex.EncodeToString(media.Key))
		}
		args = append(args, "-i", path, "-map", "0:v:0", "-map", "0:a:0", "-f", "null", "-")
		if _, err := exec.CommandContext(t.Context(), "ffmpeg", args...).Output(); err != nil {
			t.Fatal("full audio/video decoding failed")
		}
		t.Logf("episode=%d codec=%s quality=%d bytes=%d decoded", index+1, media.Codec, media.Quality, n)
	}
}
