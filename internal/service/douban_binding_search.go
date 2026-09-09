package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var ErrDoubanSearchUnavailable = errors.New("豆瓣搜索受限或页面解析失败，请稍后重试，或输入豆瓣条目链接 / ID")

var doubanSearchData = regexp.MustCompile(`window\.__DATA__\s*=\s*`)

// bindingSearchCandidates 仅用于人工绑定，不改变自动刮削的联想匹配策略。
func (d *DoubanProvider) bindingSearchCandidates(ctx context.Context, query string) ([]*DoubanMatch, error) {
	if validDoubanID(query) {
		return []*DoubanMatch{{DoubanID: query, Title: query}}, nil
	}
	if strings.Contains(query, "://") {
		u, err := url.Parse(query)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" || (u.Hostname() != "movie.douban.com" && u.Hostname() != "m.douban.com" && u.Hostname() != "www.douban.com") {
			return nil, ErrDoubanBindingInvalid
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != "subject" || !validDoubanID(parts[1]) {
			return nil, ErrDoubanBindingInvalid
		}
		return []*DoubanMatch{{DoubanID: parts[1], Title: parts[1]}}, nil
	}
	raw, status, err := d.requestJSON(ctx, "https://search.douban.com/movie/subject_search?cat=1002&search_text="+url.QueryEscape(query), "https://movie.douban.com/")
	if err != nil || status != 200 {
		return nil, ErrDoubanSearchUnavailable
	}
	return parseDoubanBindingSearch(raw)
}

// 只解码页面内的数据对象，绝不执行上游 JavaScript；缺少数据不能当作零结果。
func parseDoubanBindingSearch(raw []byte) ([]*DoubanMatch, error) {
	location := doubanSearchData.FindIndex(raw)
	if location == nil {
		return nil, ErrDoubanSearchUnavailable
	}
	var page struct {
		Total *int `json:"total"`
		Items []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Template string `json:"tpl_name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(strings.NewReader(string(raw[location[1]:]))).Decode(&page); err != nil || page.Total == nil || *page.Total < 0 || page.Items == nil || (*page.Total > 0 && len(page.Items) == 0) {
		return nil, ErrDoubanSearchUnavailable
	}
	out := make([]*DoubanMatch, 0, 5)
	seen := make(map[int64]bool)
	for _, item := range page.Items {
		if item.Template != "search_subject" || item.ID <= 0 || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		out = append(out, &DoubanMatch{DoubanID: strconv.FormatInt(item.ID, 10), Title: item.Title})
		if len(out) == 5 {
			break
		}
	}
	return out, nil
}
