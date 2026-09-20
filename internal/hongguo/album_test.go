package hongguo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseAlbum(t *testing.T) {
	const id = "9000000000000000001"
	for _, tc := range []struct {
		name, body string
		want       Album
		bad        bool
	}{
		{"precision", `{"code":0,"data":{"video_data":{"series_id":9000000000000000001,"related_album_id":9000000000000000099,"season_index":12}}}`, Album{ID: "9000000000000000099", Season: 12}, false},
		{"standalone", `{"code":0,"data":{"video_data":{"series_id_str":"9000000000000000001"}}}`, Album{}, false},
		{"zero", `{"code":0,"data":{"video_data":{"series_id_str":"9000000000000000001","related_album_id":0}}}`, Album{}, false},
		{"wrong identity", `{"code":0,"data":{"video_data":{"series_id":"2"}}}`, Album{}, true},
		{"bad season", `{"code":0,"data":{"video_data":{"series_id":"9000000000000000001","related_album_id":"99","season_index":0}}}`, Album{}, true},
		{"fraction", `{"code":0,"data":{"video_data":{"series_id":"9000000000000000001","related_album_id":"99","season_index":1.5}}}`, Album{}, true},
		{"error", `{"code":429,"message":"private upstream context"}`, Album{}, true},
		{"empty", `{}`, Album{}, true},
		{"invalid album type", `{"code":0,"data":{"video_data":{"series_id":"9000000000000000001","related_album_id":{}}}}`, Album{}, true},
		{"trailing data", `{"code":0,"data":{"video_data":{"series_id":"9000000000000000001"}}}{}`, Album{}, true},
		{"truncated", `{"code":0,`, Album{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAlbum([]byte(tc.body), id)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

func TestAlbumErrorsIdentifySafeFields(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"code":429,"message":"private upstream context"}`, "红果官方合集业务错误（code=429）"},
		{`{"code":"private upstream context"}`, "红果官方合集业务码 code 缺失或无效"},
		{`{"code":0,`, "红果官方合集响应 JSON 无效"},
		{`{}`, "红果官方合集业务码 code 缺失或无效"},
		{`{"code":0,"data":{"video_data":{"series_id":"123","related_album_id":"private upstream context"}}}`, "红果官方合集 related_album_id 无效"},
		{`{"code":0,"data":{"video_data":{"series_id":"123","related_album_id":"456","season_index":"private upstream context"}}}`, "红果官方合集 season_index 缺失或不是整数"},
		{`{"code":0,"data":{"video_data":{"series_id":"123","related_album_id":"456","season_index":0}}}`, "红果官方合集 season_index=0 超出有效范围 1–100000"},
	} {
		_, err := parseAlbum([]byte(tc.body), "123")
		if fmt.Sprint(err) != tc.want {
			t.Fatalf("error=%v, want %s", err, tc.want)
		}
	}
}

type albumTransport func(*http.Request) (*http.Response, error)

func (f albumTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAlbumRequestAndCancellation(t *testing.T) {
	c := NewClient(&http.Client{Transport: albumTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/novel/player/video_detail/v1/" || r.Header.Get("X-Gorgon") == "" {
			t.Fatal("wrong request")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["series_id"] != "9000000000000000001" {
			t.Fatal("wrong identity")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"video_data":{"series_id_str":"9000000000000000001"}}}`))}, nil
	})})
	if _, err := c.Album(t.Context(), "9000000000000000001"); err != nil {
		t.Fatal(err)
	}
	c = NewClient(&http.Client{Transport: albumTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Album(ctx, "9000000000000000001"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
