package hongguo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/emmansun/gmsm/sm3"
)

func TestDanmuSigning(t *testing.T) {
	query := "aid=8662&device_id=123456789&item_id=987654321"
	// 固定虚构身份的跨实现向量；包含 protobuf、Simon 与 AES-CBC 封装。
	const argus = "PMxhgUKIXtho/6stSub0VASKJDoVY13IeJ5USEZ0fLHWQXkZp5PSqsr/fMcIqn9bW6jBOKWXyyaYGh8474IVti6u59Vh2DIEkO5Eh5WY2g1ECo3Ds2vxDdYr/0EgtoHiXGm3Cx04ukAkZdj9lBi5lv6NGhWycBEXRo0+zkrxoJsEYciB7CnqxAwylzl4MICLT3nc5gdCxiaviXBATi8qgUtoXMhJeuvL6zNQmYtBcw1Awg=="
	if got := danmuArgus(query, nil, 1700000000, 0x12345678, 18); got != argus {
		t.Fatalf("Argus vector mismatch: %s", got)
	}
	for _, test := range []struct{ cookie, want string }{
		{"", "840440e500006970da9d69830c95ef10c0595eb2ef8f1f17e211"},
		{"sessionid=FAKE_TEST_ONLY; device_id=0000000000000000000", "840440e500006970da9d69830c6fa91612ec5eb2ef8f1f17e211"},
	} {
		if got := danmuGorgon(query, test.cookie, nil, 1700000000, 0x40, 0xe5); got != test.want {
			t.Fatalf("Gorgon vector mismatch: %s", got)
		}
	}
	if got := danmuLadon(1700000000, "8662", []byte{1, 2, 3, 4}); got != "AQIDBHgyNZz5g2RmlVaZawPQI2nTD+yV0fizoIdp1CeRhZOZ" {
		t.Fatalf("Ladon vector mismatch: %s", got)
	}
	hash := sm3.Sum([]byte("abc"))
	if hex.EncodeToString(hash[:]) != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" {
		t.Fatal("SM3 standard vector mismatch")
	}
}

func TestDanmuWindowsAndSafeFailures(t *testing.T) {
	for _, failure := range []string{"", "business", "json", "oversize", "redirect", "network", "cursor", "time", "empty"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			device := ""
			client := NewClient(&http.Client{Transport: albumTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "api5-normal-sinfonlinea.fqnovel.com" {
					t.Fatal("unexpected destination")
				}
				for _, header := range []string{"X-Argus", "X-Ladon", "X-Gorgon", "X-Khronos"} {
					if req.Header.Get(header) == "" {
						t.Fatalf("missing %s", header)
					}
				}
				if req.Header.Get("Cookie") != "fake-cookie" || req.Header.Get("X-Tt-Token") != "fake-token" {
					t.Fatal("configuration missing")
				}
				if device == "" {
					device = req.URL.Query().Get("device_id")
				} else if device != req.URL.Query().Get("device_id") {
					t.Fatal("identity changed mid-request")
				}
				body := `{"code":0,"data":{"video_model":{"video_duration":61.125}}}`
				status := 200
				if strings.Contains(req.URL.Path, "commentapi") {
					calls++
					if req.Header.Get("X-SS-STUB") != "" || req.Header.Get("Comment-Source") != "601" {
						t.Fatal("comment headers")
					}
					var payload map[string]any
					if json.NewDecoder(req.Body).Decode(&payload) != nil {
						t.Fatal("body")
					}
					if payload["group_id"] != "456" || payload["aid"] != float64(8662) || object(payload["business_param"])["book_id"] != "123" {
						t.Fatal("coordinate/body mismatch")
					}
					body = `{"code":0,"data":{"data_list":[{"comment":{"comment_id":7683198247380192281,"common":{"group_id":"456","content":{"text":"你好<&"},"create_timestamp":1700000000},"expand":{"offset_time":1001}}}],"common_list_info":{"cursor":"30000","has_more":true},"extra":{"next_query_danmaku_list_time":30000}}}`
					if calls > 1 {
						switch failure {
						case "business":
							body = `{"code":999,"message":"fake-secret"}`
						case "json":
							body = `{`
						case "oversize":
							body = strings.Repeat("x", (2<<20)+1)
						case "redirect":
							status = 302
						case "network":
							return nil, errors.New("fake-secret")
						case "cursor":
							body = `{"code":0,"data":{"common_list_info":{"cursor":"30000"},"extra":{"next_query_danmaku_list_time":60000}}}`
						case "time":
							body = `{"code":0,"data":{"extra":{"next_query_danmaku_list_time":30000}}}`
						case "empty":
							body = `{"code":0,"data":{"data_list":[],"extra":{"next_query_danmaku_list_time":61125}}}`
						default:
							body = strings.ReplaceAll(strings.ReplaceAll(body, "30000", "61125"), "7683198247380192281", "7683198247380192282")
						}
					}
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://example.invalid/"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			rows, err := client.Danmus(t.Context(), "123", 1, "456", DanmuAppConfig{Cookie: "fake-cookie", Token: "fake-token"})
			if calls != 2 || len(rows) < 1 || rows[0].ID != "7683198247380192281" || rows[0].OffsetMS != 1001 {
				t.Fatalf("lost successful window: calls=%d rows=%v err=%v", calls, rows, err)
			}
			if failure == "" || failure == "empty" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || strings.Contains(err.Error(), "fake-secret") {
				t.Fatalf("expected safe error, got %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := NewClient(nil).Danmus(ctx, "123", 1, "456", DanmuAppConfig{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestDanmuLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_HONGGUO_DANMU_LIVE") != "1" {
		t.Skip("explicit anonymous live check only")
	}
	rows, err := NewClient(nil).Danmus(t.Context(), "7683196130645003288", 1, "7683198247380192281", DanmuAppConfig{})
	t.Logf("anonymous comments=%d complete=%t", len(rows), err == nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDanmuFiltersSourceWindows(t *testing.T) {
	for _, next := range []any{60000, 30000, "bad", 86400001} {
		t.Run(fmt.Sprint(next), func(t *testing.T) {
			calls := 0
			row := func(id string, offset any, fields map[string]any) any {
				common := map[string]any{"content": map[string]any{"text": "a\x00b"}}
				for key, value := range fields {
					common[key] = value
				}
				return map[string]any{"comment": map[string]any{"comment_id": id, "common": common, "expand": map[string]any{"offset_time": offset}}}
			}
			client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `{"code":0,"data":{"video_model":{"video_duration":45}}}`
				if strings.Contains(req.URL.Path, "commentapi") {
					calls++
					list := []any{
						row("1", 0, map[string]any{"group_id": "456", "status": 1}),
						row("2", 29999, map[string]any{"status": "1"}),
						row("3", 30000, nil), row("4", -1, nil),
						row("5", 1000, map[string]any{"group_id": "789"}),
						row("6", 1000, map[string]any{"status": 0}),
						row("7", 1000, nil),
						row("8", 1000, map[string]any{"group_id": nil}),
						row("9", 1000, map[string]any{"status": nil}),
						row("10", 45000, nil), row("11", 1.5, nil), row("1", 1, nil),
					}
					windowEnd := any(30000)
					if calls == 2 {
						windowEnd = next
						list = []any{row("12", 30000, nil), row("13", 44999, nil), row("14", 45000, nil), row("15", 1000, nil), row("1", 30001, nil)}
					}
					data, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"data_list": list, "extra": map[string]any{"next_query_danmaku_list_time": windowEnd}}})
					body = string(data)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			rows, err := client.Danmus(t.Context(), "123", 1, "456", DanmuAppConfig{})
			want := "1,2,7"
			if next == 60000 {
				want += ",12,13"
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid window accepted")
			}
			var ids []string
			for _, item := range rows {
				ids = append(ids, item.ID)
				if item.Content != "ab" {
					t.Fatal("NUL sanitization changed")
				}
			}
			if calls != 2 || strings.Join(ids, ",") != want {
				t.Fatalf("calls=%d ids=%v want=%s", calls, ids, want)
			}
		})
	}
}

func TestDanmuResolvesOnlyRequestedEpisode(t *testing.T) {
	for _, stale := range []string{"", "999"} {
		details, comments := 0, 0
		client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := ""
			switch r.URL.Path {
			case "/detail":
				details++
				if r.Header.Get("Cookie") != "" {
					t.Fatal("App credentials sent to detail site")
				}
				body = string(detailPage(`"episode_cnt":3,"vid_list":["111","222","333"]`))
			case "/novel/player/video_model/v1/":
				var payload struct {
					Video string `json:"video_id"`
				}
				if json.NewDecoder(r.Body).Decode(&payload) != nil {
					t.Fatal("invalid body")
				}
				if payload.Video == "999" {
					body = `{"code":101002}`
				} else if payload.Video == "222" {
					body = `{"code":0,"data":{"video_model":"{\"video_duration\":2.1}"}}`
				} else {
					t.Fatalf("requested other episode: %s", payload.Video)
				}
			case "/novel/commentapi/comment/list/222/v1/":
				comments++
				body = `{"code":0,"data":{"data_list":[{"comment":{"comment_id":"1","common":{"content":{"text":"a\u0000b"}},"expand":{"offset_time":1000}}}],"extra":{"next_query_danmaku_list_time":30000}}}`
			default:
				t.Fatal("unexpected request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		rows, err := client.Danmus(t.Context(), testID, 2, stale, DanmuAppConfig{Cookie: "FAKE_COOKIE"})
		if err != nil || len(rows) != 1 || rows[0].Content != "ab" || details != 1 || comments != 1 {
			t.Fatalf("resolution failed details=%d comments=%d err=%v", details, comments, err)
		}
	}
}
