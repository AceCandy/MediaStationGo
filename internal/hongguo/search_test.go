package hongguo

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSearchReadsOfficialResultsWithoutLosingIDs(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "hongguoduanju.com" || r.URL.EscapedPath() != "/search/%E7%83%AC%E4%B9%9D%E5%B7%9E%2F%3F" || r.URL.RawQuery != "" {
			t.Fatalf("unexpected URL: %s", r.URL)
		}
		body := `_ROUTER_DATA={"loaderData":{"search_layout":null,"search_(keyword)/page":{"isSuccess":true,"totalCount":"196","searchList":[{"video_data":{"series_id":9000000000000000001,"series_title":"烬九州","series_intro":"简介","episode_cnt":100,"episode_right_text":"全100集","category_list":[{"name":"剧情"}],"series_cover":"https://example.invalid/private"}},{"video_data":{"series_id":"9000000000000000001","series_title":"重复"}},{"user_info":{"name":"演员"}}]}}};`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	works, err := client.Search(t.Context(), " 烬九州/? ")
	if err != nil || len(works) != 1 {
		t.Fatalf("results=%+v error=%v", works, err)
	}
	w := works[0]
	if w.SourceID != testID || w.Title != "烬九州" || w.EpisodeCount != 100 || len(w.Tags) != 1 || w.Tags[0] != "剧情" || w.CoverURL != "" || len(w.Snapshot) != 0 {
		t.Fatalf("unexpected projection: %+v", w)
	}
}

func TestSearchValidationErrorsAndDirectID(t *testing.T) {
	calls := 0
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/detail" || r.URL.Query().Get("series_id") != testID {
			t.Fatalf("unexpected URL: %s", r.URL)
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header)}, nil
	})})
	for _, q := range []string{"", " ", "..", strings.Repeat("字", 101), "bad\nquery"} {
		if _, err := client.Search(t.Context(), q); err == nil {
			t.Fatalf("accepted %q", q)
		}
	}
	if calls != 0 {
		t.Fatal("invalid search made HTTP request")
	}
	rows, err := client.Search(t.Context(), testID)
	if err != nil || len(rows) != 0 || calls != 1 {
		t.Fatalf("404: %v %v calls=%d", rows, err, calls)
	}
	for _, body := range []string{`<html>blocked</html>`, `_ROUTER_DATA={"loaderData":{"search_(keyword)/page":{"isSuccess":false,"searchList":[]}}}`, `_ROUTER_DATA={"loaderData":{"search_(keyword)/page":{}}}`} {
		client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})})
		if _, err := client.Search(t.Context(), "测试"); err == nil {
			t.Fatal("invalid response treated as empty result")
		}
	}
}
