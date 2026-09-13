package hongguo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const testID = "9000000000000000001"

func detailPage(fields string) []byte {
	return []byte(`<script>window._ROUTER_DATA = {"loaderData":{"detail_page":{"isSuccess":true,"req":{"secret":"private"},"seriesDetail":{"series_id":9000000000000000001,"series_name":"测试剧",` + fields + `},"seriesSocialInfo":{"rating":"9.2","rating_count":123,"reviews":[{"text":"private"}]}}}};</script>`)
}

func TestDetailIdentityCountsAndSnapshot(t *testing.T) {
	w, err := ParseDetail(detailPage(`"episode_cnt":81,"accessible_episode_cnt":3,"episode_right_text":"全81集","first_visible_time":"1700000000","series_episode_info":{"episode_total_cnt":81,"series_status":1},"vid_list":["9000000000000000002","","9000000000000000004"],"celebrities":[{"celebrity_id":9000000000000000005,"nickname":"演员甲","sub_title":"主演","private":"private"}]`), testID)
	if err != nil {
		t.Fatal(err)
	}
	if w.SourceID != testID || w.EpisodeCount != 81 || w.TotalEpisodes != 81 || w.AccessibleEpisodes != 3 || !w.Completed || w.IsMovie() {
		t.Fatalf("invalid work: %+v", w)
	}
	if len(w.VideoIDs) != 3 || w.VideoIDs[1] != "" || w.VideoIDs[2] != "9000000000000000004" {
		t.Fatalf("episode coordinates changed: %v", w.VideoIDs)
	}
	if len(w.People) != 1 || w.People[0].SourceID != "9000000000000000005" || w.Rating != 9.2 || w.FirstVisibleAt == nil {
		t.Fatal("lost source details")
	}
	if strings.Contains(string(w.Snapshot), "private") || !strings.Contains(string(w.Snapshot), testID) {
		t.Fatalf("invalid snapshot: %s", w.Snapshot)
	}
	if _, err := ParseDetail(detailPage(`"episode_cnt":1`), "9000000000000000009"); err == nil {
		t.Fatal("accepted another work")
	}
}

func TestMovieRequiresConsistentCompletionEvidence(t *testing.T) {
	for _, tc := range []struct {
		fields string
		movie  bool
	}{
		{`"episode_cnt":1,"episode_right_text":"全1集"`, true},
		{`"episode_cnt":1,"episode_right_text":"更新至1集"`, false},
		{`"episode_cnt":1,"series_episode_info":{"series_status":1}`, false},
		{`"episode_cnt":1,"episode_right_text":"全1集","series_episode_info":{"episode_total_cnt":12}`, false},
		{`"episode_cnt":1,"episode_right_text":"全1集","vid_list":["2","3"]`, false},
	} {
		w, err := ParseDetail(detailPage(tc.fields), testID)
		if err != nil || w.IsMovie() != tc.movie {
			t.Fatalf("%s: movie=%v err=%v", tc.fields, w.IsMovie(), err)
		}
	}
}

func TestCategoryKeepsNumericIDPrecision(t *testing.T) {
	body := []byte(`_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[{"video_data":{"series_id":9000000000000000001,"series_title":"分类标题","episode_cnt":12,"episode_right_text":"更新至12集"},"series_intro":"外层介绍","series_cover":"https://example.invalid/poster"},{"series_id":"9000000000000000001"}]}}}`)
	ids, err := ParseCategory(body)
	if err != nil || len(ids) != 1 || ids[0].SourceID != testID {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if ids[0].Title != "分类标题" || ids[0].Overview != "外层介绍" || ids[0].CoverURL != "https://example.invalid/poster" || ids[0].EpisodeCount != 12 || ids[0].UpdateText != "更新至12集" || ids[0].Completed || len(ids[0].Snapshot) != 0 {
		t.Fatalf("category summary lost fields or fabricated details: %+v", ids[0])
	}
	for _, body := range []string{`<html>blocked</html>`, `_ROUTER_DATA={"loaderData":{"category_page":{"isSuccess":false}}}`, `_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[{}]}}}`} {
		if _, err := ParseCategory([]byte(body)); err == nil {
			t.Fatal("invalid page treated as empty success")
		}
	}
}

func TestDynamicDataNodeIgnoresLayout(t *testing.T) {
	ids, err := ParseCategory([]byte(`_ROUTER_DATA={"loaderData":{"category_layout":{},"category_$":{"recommendList":[{"series_id":"9000000000000000001"}]}}}`))
	if err != nil || len(ids) != 1 || ids[0].SourceID != testID {
		t.Fatalf("category IDs=%v err=%v", ids, err)
	}
	body := strings.Replace(string(detailPage(`"episode_cnt":1`)), `"detail_page":`, `"detail_layout":{},"detail_$":`, 1)
	work, err := ParseDetail([]byte(body), testID)
	if err != nil || work.SourceID != testID {
		t.Fatalf("detail ID=%s err=%v", work.SourceID, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestCategoryUsesCanonicalPaginationURL(t *testing.T) {
	if len(Categories) != 3 || Categories[0] != "real-drama" || Categories[1] != "comic-drama" || Categories[2] != "ai-drama" || ValidCategory("comic") {
		t.Fatalf("unexpected discovery categories: %v", Categories)
	}
	for _, category := range Categories {
		for _, page := range []int{1, 2, 10000} {
			t.Run(fmt.Sprintf("%s/%d", category, page), func(t *testing.T) {
				calls := 0
				client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					want := "/category/" + category
					if page > 1 {
						want += fmt.Sprintf("?page=%d", page)
					}
					if r.URL.RequestURI() != want {
						t.Fatalf("request URI = %q, want %q", r.URL.RequestURI(), want)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[{"series_id":"9000000000000000001"}]}}}`)), Header: make(http.Header), Request: r}, nil
				})})
				ids, _, err := client.Category(t.Context(), category, page)
				if err != nil || calls != 1 || len(ids) != 1 || ids[0].SourceID != testID {
					t.Fatalf("ids=%v calls=%d err=%v", ids, calls, err)
				}
			})
		}
	}
}

func TestRankUsesOfficialPageOrderAndPagination(t *testing.T) {
	for _, rank := range Ranks {
		for _, page := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", rank.Key, page), func(t *testing.T) {
				client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					want := "/rank/" + rank.Key
					if page > 1 {
						want += "?page=2"
					}
					if r.URL.RequestURI() != want {
						t.Fatalf("request URI = %q, want %q", r.URL.RequestURI(), want)
					}
					next := ""
					if page == 1 {
						next = `<a rel="next" href="` + want + `?page=2">下一页</a>`
					}
					body := `<html><body><ol aria-label="` + rank.Label + `"><li><article><img src="https://example.invalid/rank"><h2 id="rank-title-` + testID + `"> 榜单标题 </h2></article></li></ol><nav aria-label="榜单分页">` + next + `</nav></body></html>`
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
				})})
				works, hasNext, err := client.Rank(t.Context(), rank.Key, page)
				if err != nil || len(works) != 1 || works[0].SourceID != testID || works[0].Title != "榜单标题" || works[0].CoverURL != "https://example.invalid/rank" || hasNext != (page == 1) {
					t.Fatalf("works=%+v next=%v err=%v", works, hasNext, err)
				}
			})
		}
	}
}

func TestCategoryCompletionUsesSourceItemCount(t *testing.T) {
	for _, itemCount := range []int{23, 24} {
		t.Run(strconv.Itoa(itemCount), func(t *testing.T) {
			items := make([]string, itemCount)
			for i := range items {
				// 重复来源 ID 会被资料投影去重，但不能因此把完整的 24 项原始页误判为尾页。
				items[i] = `{"series_id":"9000000000000000001"}`
			}
			body := `_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[` + strings.Join(items, ",") + `]}}}`
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			works, sourceCount, err := client.Category(t.Context(), "real-drama", 2)
			if err != nil || len(works) != 1 || sourceCount != itemCount {
				t.Fatalf("items=%d works=%d source_count=%d err=%v", itemCount, len(works), sourceCount, err)
			}
		})
	}
}

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientRejectsInvalidInputAndHTTPFailure(t *testing.T) {
	calls := 0
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "hongguoduanju.com" || r.URL.Query().Get("series_id") != testID {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("private")), Header: make(http.Header), Request: r}, nil
	})})
	if _, err := client.Detail(context.Background(), "../bad"); err == nil || calls != 0 {
		t.Fatal("invalid ID requested")
	}
	if _, _, err := client.Category(context.Background(), "../bad", 1); err == nil || calls != 0 {
		t.Fatal("invalid category requested")
	}
	if _, _, err := client.Rank(context.Background(), "../bad", 1); err == nil || calls != 0 {
		t.Fatal("invalid rank requested")
	}
	_, err := client.Detail(context.Background(), testID)
	if err == nil || calls != 1 || !strings.Contains(err.Error(), "HTTP 403") || strings.Contains(fmt.Sprint(err), "private") {
		t.Fatalf("unexpected error: %v", err)
	}
	categoryClient := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("private")), Header: make(http.Header), Request: r}, nil
	})})
	_, _, err = categoryClient.Category(context.Background(), "real-drama", 14)
	if err == nil || !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "real-drama 第 14 页（/category/real-drama?page=14）") || strings.Contains(err.Error(), BaseURL) || strings.Contains(err.Error(), "private") {
		t.Fatalf("category error lacks safe request context: %v", err)
	}
}
