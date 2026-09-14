// Package hongguo 读取红果公开资料，不请求或解析播放地址。
package hongguo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const BaseURL = "https://hongguoduanju.com"

// MaxCategoryPage 是分页请求的安全上限，达到上限不代表来源已经拉取完整。
const MaxCategoryPage = 10000

// CategoryPageSize 是来源分类页的固定条数；不足一页表示该分类已到末页。
const CategoryPageSize = 24
const MaxRankPage = 100
const maxResponseBytes = 8 << 20

// ErrNotFound 表示固定红果资料路径返回 HTTP 404。
var ErrNotFound = errors.New("红果资料不存在")

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,31}$`)
var routerAssignment = regexp.MustCompile(`(?:window\.)?_ROUTER_DATA\s*=\s*`)
var finishedEpisodes = regexp.MustCompile(`^全\s*([0-9]+)\s*集$`)

// Categories 是来源公开的分类路由，不能作为作品的跨季关系。
var Categories = [...]string{"real-drama", "comic-drama", "ai-drama"}

type Rank struct {
	Key            string
	Label          string
	SourceCategory string
}

var Ranks = [...]Rank{
	{Key: "hot-drama", Label: "红果热播榜"},
	{Key: "hot-real-drama", Label: "真人剧热播榜", SourceCategory: "real-drama"},
	{Key: "hot-ai-drama", Label: "AI剧热播榜", SourceCategory: "ai-drama"},
	{Key: "hot-comic-drama", Label: "漫剧热播榜", SourceCategory: "comic-drama"},
}

func ValidCategory(category string) bool {
	for _, value := range Categories {
		if category == value {
			return true
		}
	}
	return false
}

func rankByKey(key string) (Rank, bool) {
	for _, rank := range Ranks {
		if rank.Key == key {
			return rank, true
		}
	}
	return Rank{}, false
}

func ValidRank(key string) bool { _, ok := rankByKey(key); return ok }

// Work 是来源详情的白名单投影。上线时间与首播时间不是同一个含义。
type Work struct {
	SourceID           string
	Title              string
	Overview           string
	CoverURL           string
	Tags               []string
	EpisodeCount       int
	TotalEpisodes      int
	AccessibleEpisodes int
	UpdateText         string
	SourceStatus       string
	Completed          bool
	FirstVisibleAt     *time.Time
	Rating             float32
	RatingCount        int64
	VideoIDs           []string
	People             []Person
	Snapshot           json.RawMessage
}

// Person 只保留来源人物身份和公开演职员字段，不按同名合并人物。
type Person struct {
	SourceID  string
	Name      string
	Subtitle  string
	AvatarURL string
}

// Client 使用固定来源与有界响应；注入 HTTP 客户端用于测试和网络策略。
type Client struct{ http *http.Client }

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{http: client}
}

func ValidID(id string) bool { return numericID.MatchString(id) }

func (c *Client) page(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html")
	// 固定来源请求不跟随重定向，避免来源跳转到内网或非资料站点。
	client := *c.http
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("红果资料请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w（HTTP 404）", ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("红果资料 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("红果资料读取失败")
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("红果资料响应过大")
	}
	return body, nil
}

func (c *Client) Detail(ctx context.Context, id string) (Work, error) {
	if !ValidID(id) {
		return Work{}, errors.New("红果作品 ID 无效")
	}
	body, err := c.page(ctx, "/detail?series_id="+url.QueryEscape(id))
	if err != nil {
		return Work{}, err
	}
	return ParseDetail(body, id)
}

// ValidSearch 限制官网关键词长度，作品 ID 仍按字符串处理。
func ValidSearch(keyword string) bool {
	return keyword != "" && keyword != "." && keyword != ".." && utf8.RuneCountInString(keyword) <= 100 && !strings.ContainsAny(keyword, "\x00\r\n")
}

// Search 读取官网搜索首屏；官网未提供已验证的分页链接，不能用总命中数伪造可翻页结果。
func (c *Client) Search(ctx context.Context, keyword string) ([]Work, error) {
	keyword = strings.TrimSpace(keyword)
	if !ValidSearch(keyword) {
		return nil, errors.New("红果搜索关键词无效")
	}
	if ValidID(keyword) {
		work, err := c.Detail(ctx, keyword)
		if errors.Is(err, ErrNotFound) {
			return []Work{}, nil
		}
		if err != nil {
			return nil, err
		}
		return []Work{work}, nil
	}
	body, err := c.page(ctx, "/search/"+url.PathEscape(keyword))
	if err != nil {
		return nil, err
	}
	data, err := loader(body, "search_(keyword)/page")
	if err != nil {
		return nil, err
	}
	items, ok := data["searchList"].([]any)
	if !ok {
		return nil, errors.New("红果搜索列表格式无效")
	}
	works := []Work{}
	seen := map[string]bool{}
	for _, item := range items {
		data := object(object(item)["video_data"])
		id, title := scalar(data["series_id"]), scalar(data["series_title"])
		if !ValidID(id) || title == "" || seen[id] {
			continue
		}
		seen[id] = true
		tags := []string{}
		if categories, ok := data["category_list"].([]any); ok {
			for _, category := range categories {
				if name := scalar(object(category)["name"]); name != "" {
					tags = append(tags, name)
				}
			}
		}
		works = append(works, Work{SourceID: id, Title: title, Overview: scalar(data["series_intro"]), EpisodeCount: count(data["episode_cnt"]), UpdateText: scalar(data["episode_right_text"]), Tags: tags})
	}
	return works, nil
}

// Category 返回一页分类摘要及原始条目数；摘要不能作为完整详情或作品下架的证据。
func (c *Client) Category(ctx context.Context, category string, page int) ([]Work, int, error) {
	if !ValidCategory(category) || page < 1 || page > MaxCategoryPage {
		return nil, 0, errors.New("红果分类或页码无效")
	}
	path := "/category/" + category
	// 来源会将 page=1 重定向到无参数首页，直接请求规范地址以保持禁重定向策略。
	if page > 1 {
		path += "?page=" + strconv.Itoa(page)
	}
	body, err := c.page(ctx, path)
	if err != nil {
		return nil, 0, fmt.Errorf("红果分类 %s 第 %d 页（%s）请求失败：%w", category, page, path, err)
	}
	works, itemCount, err := parseCategory(body)
	if err != nil {
		return nil, 0, fmt.Errorf("红果分类 %s 第 %d 页（%s）解析失败：%w", category, page, path, err)
	}
	return works, itemCount, nil
}

// Rank 返回官网热播榜的一页摘要和是否存在下一页；切勿用本地评分或收录时间代替官网名次。
func (c *Client) Rank(ctx context.Context, key string, page int) ([]Work, bool, error) {
	rank, ok := rankByKey(key)
	if !ok || page < 1 || page > MaxRankPage {
		return nil, false, errors.New("红果榜单或页码无效")
	}
	path := "/rank/" + key
	if page > 1 {
		path += "?page=" + strconv.Itoa(page)
	}
	body, err := c.page(ctx, path)
	if err != nil {
		return nil, false, fmt.Errorf("红果榜单 %s 第 %d 页（%s）请求失败：%w", key, page, path, err)
	}
	works, hasNext, err := parseRank(body, rank.Label)
	if err != nil {
		return nil, false, fmt.Errorf("红果榜单 %s 第 %d 页（%s）解析失败：%w", key, page, path, err)
	}
	return works, hasNext, nil
}

func loader(body []byte, name string) (map[string]any, error) {
	loc := routerAssignment.FindIndex(body)
	if loc == nil {
		return nil, errors.New("红果页面缺少资料数据")
	}
	dec := json.NewDecoder(strings.NewReader(string(body[loc[1]:])))
	dec.UseNumber()
	var root struct {
		LoaderData map[string]json.RawMessage `json:"loaderData"`
	}
	if err := dec.Decode(&root); err != nil {
		return nil, errors.New("红果页面资料格式无效")
	}
	raw := root.LoaderData[name]
	if len(raw) == 0 {
		raw = root.LoaderData[name+"_page"]
	}
	if len(raw) == 0 {
		// 动态资料路由与同前缀 layout 并存，优先选择明确的资料节点。
		raw = root.LoaderData[name+"_$"]
	}
	if len(raw) == 0 {
		for key, value := range root.LoaderData {
			if strings.HasPrefix(key, name+"_") {
				if len(raw) > 0 {
					return nil, errors.New("红果页面资料节点不唯一")
				}
				raw = value
			}
		}
	}
	dec = json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var data map[string]any
	if err := dec.Decode(&data); err != nil || data == nil {
		return nil, errors.New("红果页面资料节点无效")
	}
	if success, exists := data["isSuccess"]; exists && success != true {
		return nil, errors.New("红果未返回成功资料")
	}
	return data, nil
}

func scalar(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func count(v any) int {
	n, err := strconv.Atoi(scalar(v))
	if err != nil || n < 0 || n > 100000 {
		return 0
	}
	return n
}

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }

func ParseCategory(body []byte) ([]Work, error) {
	works, _, err := parseCategory(body)
	return works, err
}

func parseCategory(body []byte) ([]Work, int, error) {
	page, err := loader(body, "category")
	if err != nil {
		return nil, 0, err
	}
	items, ok := page["recommendList"].([]any)
	if !ok {
		return nil, 0, errors.New("红果分类列表格式无效")
	}
	works := make([]Work, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		m := object(item)
		id := scalar(object(m["video_data"])["series_id"])
		if id == "" {
			id = scalar(m["series_id"])
		}
		if ValidID(id) && !seen[id] {
			data := object(m["video_data"])
			field := func(names ...string) string {
				for _, fields := range []map[string]any{data, m} {
					for _, name := range names {
						if value := scalar(fields[name]); value != "" {
							return value
						}
					}
				}
				return ""
			}
			works = append(works, Work{SourceID: id, Title: field("series_title", "series_name", "name"), Overview: field("series_intro"), CoverURL: field("series_cover"), EpisodeCount: count(field("episode_cnt")), UpdateText: field("episode_right_text")})
			seen[id] = true
		}
	}
	if len(items) > 0 && len(works) == 0 {
		return nil, 0, errors.New("红果分类没有有效作品 ID")
	}
	return works, len(items), nil
}

func parseRank(body []byte, label string) ([]Work, bool, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, false, errors.New("红果榜单页面格式无效")
	}
	var list, pagination *html.Node
	walkHTML(doc, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "ol" && htmlAttr(node, "aria-label") == label {
			list = node
		}
		if node.Data == "nav" && htmlAttr(node, "aria-label") == "榜单分页" {
			pagination = node
		}
	})
	if list == nil {
		return nil, false, errors.New("红果榜单列表不存在")
	}
	works := []Work{}
	seen := map[string]bool{}
	for item := list.FirstChild; item != nil; item = item.NextSibling {
		if item.Type != html.ElementNode || item.Data != "li" {
			continue
		}
		var id, title, cover string
		walkHTML(item, func(node *html.Node) {
			if node.Type != html.ElementNode {
				return
			}
			if node.Data == "h2" && strings.HasPrefix(htmlAttr(node, "id"), "rank-title-") {
				id = strings.TrimPrefix(htmlAttr(node, "id"), "rank-title-")
				title = strings.TrimSpace(htmlText(node))
			}
			if node.Data == "img" && cover == "" {
				cover = htmlAttr(node, "src")
			}
		})
		if !ValidID(id) || title == "" || seen[id] {
			return nil, false, errors.New("红果榜单作品身份无效")
		}
		seen[id] = true
		works = append(works, Work{SourceID: id, Title: title, CoverURL: cover})
	}
	if len(works) == 0 {
		return nil, false, errors.New("红果榜单没有有效作品")
	}
	hasNext := false
	if pagination != nil {
		walkHTML(pagination, func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "a" && htmlAttr(node, "rel") == "next" {
				hasNext = true
			}
		})
	}
	return works, hasNext, nil
}

func walkHTML(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func htmlAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func htmlText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var value strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		value.WriteString(htmlText(child))
	}
	return value.String()
}

// ParseDetail 严格校验请求身份，保留集号空位，不用可访问集数推断总集数。
func ParseDetail(body []byte, expectedID string) (Work, error) {
	page, err := loader(body, "detail")
	if err != nil {
		return Work{}, err
	}
	d := object(page["seriesDetail"])
	w := Work{SourceID: scalar(d["series_id"]), Title: scalar(d["series_name"]), Overview: scalar(d["series_intro"]), CoverURL: scalar(d["series_cover"]), Tags: []string{}, People: []Person{}, VideoIDs: []string{}}
	if !ValidID(expectedID) || w.SourceID != expectedID || w.Title == "" {
		return Work{}, errors.New("红果作品身份或标题不匹配")
	}
	info := object(d["series_episode_info"])
	w.EpisodeCount = count(info["episode_cnt"])
	if w.EpisodeCount == 0 {
		w.EpisodeCount = count(d["episode_cnt"])
	}
	w.TotalEpisodes = count(info["episode_total_cnt"])
	w.AccessibleEpisodes = count(d["accessible_episode_cnt"])
	w.UpdateText = scalar(d["episode_right_text"])
	w.SourceStatus = scalar(info["series_status"])
	// 来源状态枚举尚无完整契约，只接受原始“全 N 集”文案作为完结证据。
	if match := finishedEpisodes.FindStringSubmatch(w.UpdateText); match != nil {
		total := count(match[1])
		if total > 0 && (w.TotalEpisodes == 0 || w.TotalEpisodes == total) && (w.EpisodeCount == 0 || w.EpisodeCount == total) {
			w.TotalEpisodes, w.Completed = total, true
		}
	}
	if tags, ok := d["tags"].([]any); ok {
		for _, tag := range tags {
			if value := scalar(tag); value != "" {
				w.Tags = append(w.Tags, value)
			}
		}
	}
	if seconds, err := strconv.ParseInt(scalar(d["first_visible_time"]), 10, 64); err == nil && seconds > 0 && seconds <= 253402300799 {
		at := time.Unix(seconds, 0).UTC()
		w.FirstVisibleAt = &at
	}
	if videos, ok := d["vid_list"].([]any); ok {
		for _, video := range videos {
			id := scalar(video)
			if id != "" && !ValidID(id) {
				return Work{}, errors.New("红果分集 ID 格式无效")
			}
			w.VideoIDs = append(w.VideoIDs, id)
		}
	}
	if len(w.VideoIDs) > w.EpisodeCount {
		w.EpisodeCount = len(w.VideoIDs)
	}
	if w.Completed && w.EpisodeCount > w.TotalEpisodes {
		w.Completed = false
	}
	if people, ok := d["celebrities"].([]any); ok {
		for _, value := range people {
			person := object(value)
			p := Person{SourceID: scalar(person["celebrity_id"]), Name: scalar(person["nickname"]), Subtitle: scalar(person["sub_title"]), AvatarURL: scalar(person["avatar"])}
			if ValidID(p.SourceID) && p.Name != "" {
				w.People = append(w.People, p)
			}
		}
	}
	social := object(page["seriesSocialInfo"])
	if rating, err := strconv.ParseFloat(scalar(social["rating"]), 32); err == nil && rating >= 0 && rating <= 10 {
		w.Rating = float32(rating)
	}
	if n, err := strconv.ParseInt(scalar(social["rating_count"]), 10, 64); err == nil && n >= 0 {
		w.RatingCount = n
	}
	// 只保存已知资料字段，避免将请求上下文、评论或未来新增隐私字段入库。
	snapshot := make(map[string]any)
	for _, key := range []string{"series_id", "series_name", "series_intro", "series_cover", "tags", "episode_cnt", "accessible_episode_cnt", "episode_right_text", "first_visible_time", "vid_list"} {
		if value, ok := d[key]; ok {
			snapshot[key] = value
		}
	}
	snapshot["series_episode_info"] = map[string]any{"episode_cnt": info["episode_cnt"], "episode_total_cnt": info["episode_total_cnt"], "series_status": info["series_status"]}
	snapshot["rating"], snapshot["rating_count"] = social["rating"], social["rating_count"]
	peopleSnapshot := make([]map[string]string, 0, len(w.People))
	for _, p := range w.People {
		peopleSnapshot = append(peopleSnapshot, map[string]string{"celebrity_id": p.SourceID, "nickname": p.Name, "sub_title": p.Subtitle, "avatar": p.AvatarURL})
	}
	snapshot["celebrities"] = peopleSnapshot
	w.Snapshot, err = json.Marshal(snapshot)
	return w, err
}

func (w Work) IsMovie() bool { return w.Completed && w.TotalEpisodes == 1 && w.EpisodeCount <= 1 }
