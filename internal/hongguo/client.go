// Package hongguo 读取红果公开资料，不请求或解析播放地址。
package hongguo

import (
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
)

const BaseURL = "https://hongguoduanju.com"

// MaxCategoryPage 是分页请求的安全上限，达到上限不代表来源已经拉取完整。
const MaxCategoryPage = 10000

// CategoryPageSize 是来源分类页的固定条数；不足一页表示该分类已到末页。
const CategoryPageSize = 24
const maxResponseBytes = 8 << 20

// ErrNotFound 表示固定红果资料路径返回 HTTP 404。
var ErrNotFound = errors.New("红果资料不存在")

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,31}$`)
var routerAssignment = regexp.MustCompile(`(?:window\.)?_ROUTER_DATA\s*=\s*`)
var finishedEpisodes = regexp.MustCompile(`^全\s*([0-9]+)\s*集$`)

// Categories 是来源公开的分类路由，不能作为作品的跨季关系。
var Categories = [...]string{"real-drama", "comic-drama", "ai-drama", "comic"}

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

// Category 返回一页分类摘要及原始条目数；摘要不能作为完整详情或作品下架的证据。
func (c *Client) Category(ctx context.Context, category string, page int) ([]Work, int, error) {
	valid := false
	for _, value := range Categories {
		if category == value {
			valid = true
		}
	}
	if !valid || page < 1 || page > MaxCategoryPage {
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
	raw := root.LoaderData[name+"_page"]
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
