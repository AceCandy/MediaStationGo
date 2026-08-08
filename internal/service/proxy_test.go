package service

import (
	"net/http"
	"testing"
	"time"
)

func TestProxyURLFromProxyServer(t *testing.T) {
	cases := []struct {
		name          string
		proxyServer   string
		requestScheme string
		want          string
	}{
		{"bare", "127.0.0.1:10808", "https", "http://127.0.0.1:10808"},
		{"scheme map https", "http=127.0.0.1:7890;https=127.0.0.1:7891", "https", "http://127.0.0.1:7891"},
		{"fallback http", "http=127.0.0.1:7890", "https", "http://127.0.0.1:7890"},
		{"socks", "socks=127.0.0.1:1080", "https", "socks5://127.0.0.1:1080"},
		{"explicit", "http://127.0.0.1:8080", "https", "http://127.0.0.1:8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := proxyURLFromProxyServer(tc.proxyServer, tc.requestScheme)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("got %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestNewInternalHTTPClientBypassesProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")

	client := NewInternalHTTPClient(time.Second)
	req, err := http.NewRequest(http.MethodGet, "http://172.17.0.1:8085", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if transport.Proxy == nil {
		return
	}
	if proxyURL, err := transport.Proxy(req); err != nil {
		t.Fatal(err)
	} else if proxyURL != nil {
		t.Fatalf("internal service client must bypass proxy, got %s", proxyURL)
	}
}
