package hongguo

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadFakeIPFallback(t *testing.T) {
	for _, tc := range []struct {
		host, address string
		fallback      bool
	}{
		{"media.example", "198.18.2.76", true},
		{"media.example", "198.19.255.1", true},
		{"media.example", "8.8.8.8", false},
		{"localhost", "127.0.0.1", false},
		{"198.18.2.76", "198.18.2.76", false},
	} {
		called := false
		ips, err := downloadLookupIP(context.Background(), tc.host,
			func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP(tc.address)}}, nil
			},
			func(context.Context, string) ([]net.IPAddr, error) {
				called = true
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			})
		if err != nil || called != tc.fallback || len(ips) != 1 {
			t.Fatalf("%+v: called=%v err=%v", tc, called, err)
		}
		if called && ips[0].IP.String() != "8.8.8.8" {
			t.Fatal("fake IP was not replaced")
		}
	}
	for _, address := range []string{"127.0.0.1", "192.168.1.1", "169.254.169.254", "198.18.2.1", "100.64.0.1", "0.0.0.0", "not-an-ip"} {
		_, err := parseDownloadDNS(strings.NewReader(`{"Status":0,"Answer":[{"Type":1,"Data":"8.8.8.8"},{"Type":1,"Data":"` + address + `"}]}`))
		if err == nil {
			t.Fatalf("accepted unsafe mixed DNS: %s", address)
		}
	}
	for _, body := range []string{`{`, `{}`, `{"Status":3}`, strings.Repeat(" ", 65537)} {
		if _, err := parseDownloadDNS(strings.NewReader(body)); err == nil {
			t.Fatal("accepted invalid DNS")
		}
	}
	ips, err := parseDownloadDNS(strings.NewReader(`{"Status":0,"Answer":[{"Type":5,"Data":"alias.example"},{"Type":1,"Data":"8.8.8.8"}]}`))
	if err != nil || len(ips) != 1 {
		t.Fatalf("valid DNS rejected: %v", err)
	}
}

func TestDownloadErrorsAreRedacted(t *testing.T) {
	for _, cause := range []error{errDownloadPrivateIP, errDownloadDNS, errDownloadPublicDNS, errDownloadConnect, errDownloadRedirect, context.DeadlineExceeded, errors.New("secret-signature")} {
		err := publicDownloadError(&url.Error{Op: "Get", URL: "https://media.example/?token=secret-signature", Err: cause})
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "https:") {
			t.Fatal("leaked signed URL")
		}
		if cause == errDownloadPrivateIP && !errors.Is(err, cause) {
			t.Fatal("lost safe error category")
		}
	}
}

func TestDownloadDNSFailureDoesNotReturnFakeIP(t *testing.T) {
	for _, systemFailure := range []bool{false, true} {
		ips, err := downloadLookupIP(context.Background(), "media.example",
			func(context.Context, string) ([]net.IPAddr, error) {
				if systemFailure {
					return nil, errors.New("DNS failed")
				}
				return []net.IPAddr{{IP: net.ParseIP("198.18.2.1")}}, nil
			}, func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("DoH failed") })
		if err == nil || len(ips) != 0 {
			t.Fatal("failed lookup returned usable addresses")
		}
	}
}

// 独立验证实际报错作品的完整读取和解码，不写业务队列；临时视频由测试清理。
func TestDownloadFakeIPLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HONGGUO_DOWNLOAD_LIVE") != "1" {
		t.Skip("live download opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	httpClient := DownloadHTTPClient()
	defer httpClient.CloseIdleConnections()
	client := NewClient(httpClient)
	work, err := client.Detail(ctx, "7621200213801176126")
	if err != nil {
		t.Fatalf("detail: %v", publicDownloadError(err))
	}
	if len(work.VideoIDs) == 0 {
		t.Fatal("no episode IDs")
	}
	media, err := client.ResolveDownload(ctx, work.SourceID, work.VideoIDs[0])
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	resp, err := DownloadRequest(ctx, httpClient, media)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	path := filepath.Join(t.TempDir(), "episode.mp4")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal("cannot create temporary media")
	}
	n, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n == 0 || (resp.ContentLength >= 0 && n != resp.ContentLength) {
		t.Fatal("media response incomplete")
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-f", "mov"}
	if len(media.Key) > 0 {
		args = append(args, "-decryption_key", hex.EncodeToString(media.Key))
	}
	args = append(args, "-i", path, "-map", "0:v:0", "-map", "0:a:0?", "-f", "null", "-")
	if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
		t.Fatal("full media decoding failed")
	}
	t.Logf("complete episode read and decoded: %d bytes", n)
}
