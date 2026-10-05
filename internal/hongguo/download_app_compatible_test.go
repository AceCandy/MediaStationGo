package hongguo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// 人工合成向量：seed 为 0..31；密文由独立实现生成，并与官方版本 1 解码器核对。
const compatibleTestSeed = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
const compatibleTestURL = "qAABAATLkVO0juTi0MD2LGOcCuqSNp60dsmK6lxEwtaTIa5L"
const compatibleTestMaterial = "nb8T+Vu+EvdZvhL1X7kR90G6DvVfpSbcXaUm3luiJdyloHE="

func TestDownloadAppURLKnownAnswerAndInvalidEnvelopes(t *testing.T) {
	for _, tc := range []struct{ value, seed string }{
		{"https://media.example/1080.mp4", ""},
		{base64.StdEncoding.EncodeToString([]byte("https://media.example/1080.mp4")), ""},
		{compatibleTestURL, compatibleTestSeed},
	} {
		got, err := decodeDownloadAppURL(tc.value, tc.seed)
		if err != nil || got != "https://media.example/1080.mp4" {
			t.Fatal("known address vector failed")
		}
	}
	raw, _ := base64.StdEncoding.DecodeString(compatibleTestURL)
	wrongVersion := bytes.Clone(raw)
	wrongVersion[2] = 2
	wrongMagic := bytes.Clone(raw)
	wrongMagic[0] = 0
	wrongPadding := bytes.Clone(raw)
	wrongPadding[len(wrongPadding)-17] ^= 1
	for _, tc := range []struct{ value, seed string }{
		{"", ""},
		{"bad", ""},
		{strings.Repeat("A", 16*1024+1), ""},
		{compatibleTestURL, "bad"},
		{compatibleTestURL, base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{compatibleTestURL, strings.Repeat("A", 129)},
		{compatibleTestURL, ""},
		{base64.StdEncoding.EncodeToString(wrongVersion), compatibleTestSeed},
		{base64.StdEncoding.EncodeToString(wrongMagic), compatibleTestSeed},
		{base64.StdEncoding.EncodeToString(wrongPadding), compatibleTestSeed},
		{base64.StdEncoding.EncodeToString(raw[:len(raw)-1]), compatibleTestSeed},
		{base64.StdEncoding.EncodeToString([]byte("file:///private")), ""},
	} {
		if _, err := decodeDownloadAppURL(tc.value, tc.seed); err == nil || strings.Contains(err.Error(), tc.value) && len(tc.value) > 5 {
			t.Fatal("invalid or unsafe address accepted")
		}
	}
}

func TestDownloadAppCompatibleRequestAndKeyRecovery(t *testing.T) {
	calls := 0
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"code":0,"data":{"video_model":{"video_id":"v-test","fallback_api":{"fallback_api":"https://vas-lf-x.snssdk.com/video/fplay/1/reference/v-test?force_fids=old&codec_type=4"},"video_list":[{"video_meta":{"codec_type":"bytevc2"},"main_url":"https://media.example/unsupported"}]}}}`
		if r.Method == http.MethodGet {
			if r.URL.Host != "vas-lf-x.snssdk.com" || r.URL.Path != "/video/fplay/1/reference/v-test" ||
				r.URL.Query().Get("codec_type") != "1" || r.URL.Query().Has("force_fids") ||
				r.Header.Get("X-Gorgon") != "" || r.Header.Get("X-SS-STUB") != "" {
				t.Fatal("wrong compatibility endpoint, parameters or headers")
			}
			body = `{"code":0,"video_info":{"code":0,"data":{"video_id":"v-test","video_duration":84.034,"key_seed":"` + compatibleTestSeed + `","video_list":{
				"video_1":{"main_url":"https://media.example/720.mp4","codec_type":"h264","definition":"720p","vwidth":1280,"vheight":720},
				"video_2":{"main_url":"` + compatibleTestURL + `","codec_type":"h265","definition":"1080p","vwidth":1920,"vheight":1080,"encrypt":true,"spade_a":"` + compatibleTestMaterial + `"},
				"video_3":{"main_url":"https://media.example/unsupported","codec_type":"bytevc2","definition":"2160p"},
				"video_4":{"main_url":"https://media.example/bad-key","codec_type":"h265","definition":"1440p","encrypt":true,"spade_a":"bad"}
			}}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	for range 2 {
		media, err := client.ResolveDownloadSource(t.Context(), "123", "456", DownloadApp)
		if err != nil || media.URL != "https://media.example/1080.mp4" || media.Codec != "hevc" ||
			media.Quality != 1080 || media.Width != 1920 || media.Height != 1080 || media.Duration != 84.034 ||
			!bytes.Equal(media.Key, []byte{0, 17, 34, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255}) {
			t.Fatal("compatible selection or repeated key resolution failed")
		}
	}
	if calls != 4 {
		t.Fatal("compatibility request was skipped or repeated")
	}
}

func TestDownloadAppCompatibilityTriggerAndEndpointSafety(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		success    bool
	}{
		{"compatible", `{"code":0,"data":{"video_model":{"video_list":[{"main_url":"https://media.example/video","video_meta":{"codec_type":"h264"}}]}}}`, true},
		{"empty", `{"code":0,"data":{"video_model":{}}}`, false},
		{"unclassified-mixed", `{"code":0,"data":{"video_model":{"video_list":[{"video_meta":{"codec_type":"bytevc2"}},null]}}}`, false},
		{"invalid-compatible-mixed", `{"code":0,"data":{"video_model":{"video_list":[{"video_meta":{"codec_type":"bytevc2"}},{"video_meta":{"codec_type":"h264"},"main_url":"bad"}]}}}`, false},
		{"takedown", `{"code":101002}`, false},
		{"business", `{"code":429}`, false},
		{"malformed", `{"code":0,"data":{"video_model":"bad"}}`, false},
		{"bad-key", `{"code":0,"data":{"video_model":{"video_list":[{"main_url":"https://media.example/video","video_meta":{"codec_type":"h264"},"encrypt_info":{"encrypt":true,"spade_a":"bad"}}]}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: r}, nil
			})})
			_, err := client.ResolveDownloadSource(t.Context(), "123", "456", DownloadApp)
			if (err == nil) != tc.success || calls != 1 {
				t.Fatal("unexpected compatibility request")
			}
		})
	}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid compatibility endpoint reached")
		return nil, nil
	})})
	for _, address := range []string{
		"https://other.example/video/fplay/1/reference/v-test",
		"http://vas-lf-x.snssdk.com/video/fplay/1/reference/v-test",
		"https://vas-lf-x.snssdk.com/other/v-test",
		"https://vas-lf-x.snssdk.com/video/fplay/1/reference/other",
		"https://vas-lf-x.snssdk.com/video/fplay/1/reference/v-test#fragment",
		"https://vas-lf-x.snssdk.com/video/fplay/1/reference/v-test?bad=%zz",
	} {
		_, err := client.resolveDownloadAppCompatible(t.Context(), map[string]any{"video_id": "v-test", "fallback_api": map[string]any{"fallback_api": address}})
		if err == nil || strings.Contains(err.Error(), address) {
			t.Fatal("invalid or unredacted compatibility endpoint")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.resolveDownloadAppCompatible(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was not preserved")
	}
}

func TestDownloadAppCompatibleResponseIdentity(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"code":1,"video_info":{"code":0,"data":{"video_id":"v-test"}}}`,
		`{"code":0,"video_info":{"code":1,"data":{"video_id":"v-test"}}}`,
		`{"code":0,"video_info":{"code":0,"data":{"video_id":"other","video_list":[{"main_url":"https://media.example/video","codec_type":"h264"}]}}}`,
		`{"code":0,"video_info":{"code":0,"data":{"video_id":"v-test","key_seed":"bad","video_list":[{"main_url":"` + compatibleTestURL + `","codec_type":"h265"}]}}}`,
	} {
		if _, err := parseDownloadAppCompatible([]byte(body), "v-test"); err == nil {
			t.Fatal("invalid status, identity or envelope accepted")
		}
	}
	body, _ := json.Marshal(map[string]any{"code": 0, "video_info": map[string]any{"code": 0, "data": map[string]any{
		"video_id": "v-test", "video_list": []any{map[string]any{"main_url": base64.StdEncoding.EncodeToString([]byte("https://media.example/video")), "codec_type": "h264", "definition": "720p"}},
	}}})
	if media, err := parseDownloadAppCompatible(body, "v-test"); err != nil || media.Quality != 720 || media.Codec != "h264" {
		t.Fatal("plain compatible array response rejected")
	}
}

func TestDownloadAppCompatibleNetworkBoundaries(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusForbidden, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.example/secret"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 4*1024*1024+1))), Request: r}, nil
			})})
			_, err := client.resolveDownloadAppCompatible(t.Context(), map[string]any{"video_id": "v-test", "fallback_api": map[string]any{"fallback_api": "https://vas-lf-x.snssdk.com/video/fplay/1/reference/v-test"}})
			if err == nil || calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal("redirect, HTTP error or oversized response accepted")
			}
		})
	}
}
