package service

import "testing"

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
