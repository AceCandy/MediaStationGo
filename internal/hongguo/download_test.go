package hongguo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadFallbackSelectsHighestValidQuality(t *testing.T) {
	// 人工合成的测试密钥材料，对应 00112233445566778899aabbccddeeff。
	const material = "nb8T+Vu+EvdZvhL1X7kR90G6DvVfpSbcXaUm3luiJdyloHE="
	option := func(name, address, key string) string {
		return fmt.Sprintf(`{"name":%q,"src":%q,"kid":"00000000000000000000000000000000","spade_a":%q}`, name, address, key)
	}
	body := `{"key_urls":[` + option("720p", "https://media.example/720.mp4", material) + "," + option("1080p", "https://media.example/1080.mp4", material) + "," + option("2160p", "https://media.example/2160.mp4", "bad") + "," + option("1440p", "file:///bad", material) + "]}"
	media, err := parseDownloadFallback([]byte(body))
	if err != nil || media.URL != "https://media.example/1080.mp4" || len(media.Key) != 16 {
		t.Fatalf("highest valid quality: %+v %v", media, err)
	}
}

func TestDownloadPageRequiresExactEpisode(t *testing.T) {
	body := []byte(`_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":"456","video_player_info":{"main_url":"https://media.example/video.mp4","duration":"30"}}}}`)
	media, err := parseDownloadPage(body, "123", "456")
	if err != nil || media.Duration != 30 {
		t.Fatalf("valid page: %v", err)
	}
	if _, err := parseDownloadPage(body, "123", "457"); err == nil {
		t.Fatal("accepted preview episode")
	}
	if _, err := parseDownloadPage(body, "124", "456"); err == nil {
		t.Fatal("accepted different work")
	}
}

func TestDownloadRejectsPrivateNetworkAndInvalidMedia(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/video", "https://u:p@example.com/video", "http://example.com:8080/video", "https://example.com/x#fragment"} {
		if ValidDownloadURL(raw) {
			t.Fatalf("accepted %s", raw)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("private endpoint reached") }))
	defer server.Close()
	if _, err := DownloadHTTPClient().Get(server.URL); err == nil {
		t.Fatal("private network accepted")
	}
	for _, body := range []string{"v2.", "v2.abcd.", `{"parse":1,"key_urls":[]}`, `{"key_urls":[]}`} {
		if _, err := parseDownloadFallback([]byte(body)); err == nil {
			t.Fatal("invalid fallback accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewClient(DownloadHTTPClient()).ResolveDownload(ctx, "123", "456"); err == nil {
		t.Fatal("cancelled resolve succeeded")
	}
}
