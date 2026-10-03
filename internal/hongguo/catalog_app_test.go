package hongguo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppCatalogPagination(t *testing.T) {
	calls, attempts := 0, 0
	device, install := "", ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary gateway", http.StatusBadGateway)
			return
		}
		calls++
		if r.URL.Path != "/reading/distribution/category/landpage/v/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if calls == 1 {
			device, install = r.URL.Query().Get("device_id"), r.URL.Query().Get("iid")
		}
		if device == "" || install == "" || device != r.URL.Query().Get("device_id") || install != r.URL.Query().Get("iid") {
			t.Error("identity changed across pages")
		}
		if r.Header.Get("X-Gorgon") == "" || r.Header.Get("X-SS-STUB") == "" {
			t.Error("ordinary App signature missing")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("bad body")
		}
		if calls == 1 {
			fmt.Fprint(w, `{"code":0,"data":{"video_data":[{"series_id":9000000000000000001,"series_title":"第一部"}],"has_more":true,"next_offset":18,"session_id":"test-session"}}`)
		} else {
			if body["offset"] != float64(18) || body["session_id"] != "test-session" {
				t.Error("cursor not carried")
			}
			fmt.Fprint(w, `{"code":0,"data":{"video_data":[{"series_id_str":"9000000000000000001","series_title":"重复"},{"series_id_str":"9000000000000000002","title":"第二部","cover":"https://example.invalid/cover","video_desc":"简介"}],"has_more":false,"next_offset":0}}`)
		}
	}))
	defer server.Close()
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(r)
	})})
	ids := []string{}
	if err := client.ScanAppCatalog(t.Context(), "real-drama", func(works []Work) error {
		for _, w := range works {
			ids = append(ids, w.SourceID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || calls != 2 || len(ids) != 2 || ids[0] != "9000000000000000001" || ids[1] != "9000000000000000002" {
		t.Fatalf("calls=%d ids=%v", calls, ids)
	}
	for _, raw := range []string{`{"code":1}`, `{"data":{"video_data":[],"has_more":true,"next_offset":18}}`, `{"data":{"video_data":[],"has_more":true,"next_offset":0}}`, `{"data":{"video_data":[],"next_offset":18}}`} {
		if _, _, _, _, err := parseAppCatalog([]byte(raw), 0); err == nil {
			t.Fatalf("invalid page accepted: %s", raw)
		}
	}
}

func TestAppCatalogRecoversRepeatedPage(t *testing.T) {
	for _, recoverable := range []bool{true, false} {
		t.Run(fmt.Sprint(recoverable), func(t *testing.T) {
			calls, saved := 0, 0
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("bad request")
				}
				payload := `{"code":0,"data":{"video_data":[{"series_id":"100","title":"作品"}],"has_more":true,"next_offset":18,"session_id":"test-session"}}`
				if calls > 1 {
					payload = `{"code":0,"data":{"video_data":[{"series_id":"100","title":"作品"}],"has_more":true,"next_offset":36,"session_id":"test-session"}}`
				}
				if calls == 3 {
					if body["session_id"] != "" || body["offset"] != float64(18) {
						t.Error("recovery must reset session without skipping offset")
					}
					if recoverable {
						payload = `{"code":0,"data":{"video_data":[{"series_id":"200","title":"新作品"}],"has_more":false,"next_offset":0}}`
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header), Request: r}, nil
			})})
			err := client.ScanAppCatalog(t.Context(), "comic-drama", func(works []Work) error { saved += len(works); return nil })
			if calls != 3 {
				t.Fatalf("calls=%d", calls)
			}
			if recoverable {
				if err != nil || saved != 2 {
					t.Fatalf("saved=%d err=%v", saved, err)
				}
			} else if err == nil || saved != 1 {
				t.Fatalf("repeated page claimed complete: saved=%d err=%v", saved, err)
			}
		})
	}
}
