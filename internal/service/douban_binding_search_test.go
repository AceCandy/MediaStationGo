package service

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestDoubanBindingSearchPage(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
		fail       bool
	}{
		{"results", `<script>window.__DATA__ = {"total":2,"items":[{"id":1293748,"title":"甜蜜与卑微","tpl_name":"search_subject"},{"id":4049664,"title":"卑微 Lowdown (2010)","tpl_name":"search_subject"}]}; window.__USER__={};</script>`, 2, false},
		{"empty", `window.__DATA__={"total":0,"items":[]};`, 0, false},
		{"blocked", `<html>请输入验证码</html>`, 0, true},
		{"malformed", `window.__DATA__ = {broken};`, 0, true},
		{"missing items", `window.__DATA__={"total":0};`, 0, true},
		{"inconsistent", `window.__DATA__={"total":2,"items":[]};`, 0, true},
		{"deduplicate", `window.__DATA__={"total":3,"items":[{"id":1,"tpl_name":"search_subject"},{"id":1,"tpl_name":"search_subject"},{"id":2,"tpl_name":"search_celebrity"}]};`, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := parseDoubanBindingSearch([]byte(tc.body))
			if errors.Is(err, ErrDoubanSearchUnavailable) != tc.fail || len(items) != tc.count {
				t.Fatalf("items=%v err=%v", items, err)
			}
		})
	}
}

func TestDoubanBindingSearchDirectAndRequest(t *testing.T) {
	d := NewDoubanProvider(nil)
	requests := 0
	d.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Host != "search.douban.com" || req.URL.Path != "/movie/subject_search" || req.URL.Query().Get("search_text") != "卑微" || req.URL.Query().Get("cat") != "1002" {
			t.Fatalf("wrong search request: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("blocked")), Request: req}, nil
	})}
	for _, query := range []string{"4049664", "https://movie.douban.com/subject/4049664/?from=search", "http://m.douban.com/subject/4049664"} {
		items, err := d.bindingSearchCandidates(t.Context(), query)
		if err != nil || len(items) != 1 || items[0].DoubanID != "4049664" {
			t.Fatalf("direct result: %v %v", items, err)
		}
	}
	for _, query := range []string{"https://evil.test/subject/4049664", "https://movie.douban.com.evil.test/subject/4049664", "https://movie.douban.com/subject/abc", "https://user@movie.douban.com/subject/4049664"} {
		if _, err := d.bindingSearchCandidates(t.Context(), query); !errors.Is(err, ErrDoubanBindingInvalid) {
			t.Fatalf("accepted invalid link: %v", err)
		}
	}
	if requests != 0 {
		t.Fatal("direct ID/link made a search request")
	}
	if _, err := d.bindingSearchCandidates(t.Context(), "卑微"); !errors.Is(err, ErrDoubanSearchUnavailable) {
		t.Fatalf("blocked response: %v", err)
	}
}

func TestDoubanBindingSearchLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_DOUBAN_LIVE") != "1" {
		t.Skip("explicit live provider check only")
	}
	d := NewDoubanProvider(nil)
	items, err := d.bindingSearchCandidates(t.Context(), "卑微")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.DoubanID != "4049664" {
			continue
		}
		detail, kind, err := d.bindingSubject(t.Context(), item.DoubanID)
		if err != nil || kind != "series" || detail.Title != "卑微" {
			t.Fatalf("live subject: %v %s %v", detail, kind, err)
		}
		return
	}
	t.Fatal("complete search missed 卑微 4049664")
}
