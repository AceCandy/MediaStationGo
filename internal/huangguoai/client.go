// Package huangguoai handles the independent HuangGuo AI website protocol.
package huangguoai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"golang.org/x/net/html"
)

const BaseURL = "https://huangguoai.com"
const MaxPage = 10000

var Categories = []string{"ai-duanju", "ai-manju", "ai-huanlian", "ai-mogai"}
var Ranks = []string{"hot", "recommend", "potential"}
var numericID = regexp.MustCompile(`^[1-9][0-9]{0,31}$`)
var episodePath = regexp.MustCompile(`^/video/([1-9][0-9]{0,31})/(?:ep-([1-9][0-9]{0,5})/)?$`)

func ValidID(id string) bool             { return numericID.MatchString(id) }
func ValidCategory(category string) bool { return Kind(category) != "" }
func ValidRank(rank string) bool         { return rank == "hot" || rank == "recommend" || rank == "potential" }
func Kind(category string) string {
	switch category {
	case "ai-duanju", "ai-manju":
		return "series"
	case "ai-huanlian", "ai-mogai":
		return "movie"
	}
	return ""
}

// Summary excludes playback fields; cover addresses remain private to ingestion.
type Summary struct {
	SourceID        string     `json:"source_id"`
	Category        string     `json:"category"`
	Title           string     `json:"title"`
	Overview        string     `json:"overview"`
	Tags            []string   `json:"tags"`
	Rating          float32    `json:"rating"`
	EpisodeCount    int        `json:"episode_count"`
	TotalEpisodes   *int       `json:"total_episodes"`
	Finished        *bool      `json:"finished"`
	SourceCreatedAt string     `json:"source_created_at"`
	SourceUpdatedAt *time.Time `json:"source_updated_at,omitempty"`
	CoverURL        string     `json:"-"`
}

type Page struct {
	Items                    []Summary
	Page, Pages, Size, Total int
}

type Episode struct {
	Number   int    `json:"number"`
	PagePath string `json:"page_path"`
}

// Work contains only metadata and verified episode coordinates, never media URLs.
type Work struct {
	Summary
	Episodes []Episode `json:"episodes"`
}

type Client struct {
	http *http.Client
	base string
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = hongguo.DownloadHTTPClient()
	}
	return &Client{http: client, base: BaseURL}
}

// Request shares the existing public-IP/redirect guard, without using HongGuo source protocols.
func (c *Client) request(ctx context.Context, raw, referer string, max int64) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resp, err := hongguo.DownloadRequest(ctx, c.http, hongguo.DownloadMedia{URL: raw, Referer: referer})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", errors.New("黄果 AI 响应读取失败")
	}
	if int64(len(body)) > max {
		return nil, "", errors.New("黄果 AI 响应超出大小限制")
	}
	return body, resp.Request.URL.String(), nil
}

func (c *Client) Category(ctx context.Context, category string, page int) (Page, error) {
	if !ValidCategory(category) || page < 1 || page > MaxPage {
		return Page{}, errors.New("黄果 AI 分类或页码无效")
	}
	body, _, err := c.request(ctx, c.base+"/api/videos/category/"+category+"?sort=hot&page="+strconv.Itoa(page)+"&size=24", c.base+"/", 4<<20)
	if err != nil {
		return Page{}, err
	}
	return ParseCategory(body, category, page)
}

func ParseCategory(body []byte, category string, requested int) (Page, error) {
	var data struct {
		Status int `json:"status"`
		Data   struct {
			Items []struct {
				ID            json.RawMessage `json:"id"`
				Title         string          `json:"title"`
				Description   string          `json:"description"`
				Tags          []string        `json:"tags"`
				Cover         string          `json:"cover"`
				Score         float32         `json:"score"`
				EpisodeCount  int             `json:"episode_count"`
				TotalEpisodes int             `json:"total_episodes"`
				Finished      *bool           `json:"is_finished"`
				CreatedAt     string          `json:"created_at"`
				LastEpAt      int64           `json:"last_ep_at"`
			} `json:"items"`
			Pagination struct{ Page, Pages, Size, Total int } `json:"pagination"`
		} `json:"data"`
	}
	if !ValidCategory(category) || requested < 1 || json.Unmarshal(body, &data) != nil || data.Status != 1 {
		return Page{}, errors.New("黄果 AI 分类响应无效")
	}
	p := data.Data.Pagination
	if p.Page != requested || p.Pages < 1 || p.Pages > MaxPage || p.Page > p.Pages || p.Size != 24 || p.Total < 0 || len(data.Data.Items) > p.Size {
		return Page{}, errors.New("黄果 AI 分页数据无效")
	}
	result := Page{Page: p.Page, Pages: p.Pages, Size: p.Size, Total: p.Total, Items: []Summary{}}
	seen := map[string]bool{}
	for _, x := range data.Data.Items {
		id := scalarID(x.ID)
		if !ValidID(id) || strings.TrimSpace(x.Title) == "" || x.EpisodeCount < 0 || x.EpisodeCount > 100000 || x.TotalEpisodes < 0 || x.TotalEpisodes > 100000 {
			return Page{}, errors.New("黄果 AI 分类条目无效")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		item := Summary{SourceID: id, Category: category, Title: strings.TrimSpace(x.Title), Overview: x.Description, Tags: x.Tags, Rating: x.Score, EpisodeCount: x.EpisodeCount, Finished: x.Finished, SourceCreatedAt: x.CreatedAt, CoverURL: x.Cover}
		if x.TotalEpisodes > 0 {
			n := x.TotalEpisodes
			item.TotalEpisodes = &n
		}
		if x.LastEpAt > 0 {
			t := time.Unix(x.LastEpAt, 0).UTC()
			item.SourceUpdatedAt = &t
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 && requested < p.Pages {
		return Page{}, errors.New("黄果 AI 分类提前返回空页")
	}
	return result, nil
}

func scalarID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func (c *Client) Detail(ctx context.Context, id string) (Work, error) {
	if !ValidID(id) {
		return Work{}, errors.New("黄果 AI 作品 ID 无效")
	}
	body, _, err := c.request(ctx, c.base+"/video/"+id+"/", c.base+"/", 4<<20)
	if err != nil {
		return Work{}, err
	}
	return ParseDetail(body, id)
}

type playData struct {
	ID          json.RawMessage   `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Ep          int               `json:"ep"`
	EpPlaySrcs  map[string]string `json:"epPlaySrcs"`
	VideoSrc    string            `json:"videoSrc"`
	PreviewSrc  string            `json:"previewSrc"`
	VideoAPI    string            `json:"videoApi"`
	CoverSrc    string            `json:"coverSrc"`
	PosterSrc   string            `json:"posterSrc"`
	Tags        []string          `json:"tags"`
	TagLinks    []struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	} `json:"tagLinks"`
}

func document(body []byte) (*html.Node, error) {
	n, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("黄果 AI 页面解析失败")
	}
	return n, nil
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func walk(n *html.Node, f func(*html.Node)) {
	f(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walk(child, f)
	}
}
func textContent(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
	})
	return strings.TrimSpace(b.String())
}
func hasClass(n *html.Node, class string) bool {
	for _, v := range strings.Fields(attr(n, "class")) {
		if v == class {
			return true
		}
	}
	return false
}
func initialData(root *html.Node, id string, ep int) (playData, error) {
	var data playData
	found := false
	valid := true
	walk(root, func(n *html.Node) {
		if n.Data == "script" && attr(n, "id") == "videoInitialData" {
			if found {
				valid = false
				return
			}
			found = true
			if json.Unmarshal([]byte(textContent(n)), &data) != nil {
				valid = false
			}
		}
	})
	if !found || !valid || scalarID(data.ID) != id || data.Ep != ep || strings.TrimSpace(data.Title) == "" {
		return playData{}, errors.New("黄果 AI 详情或分集身份无效")
	}
	return data, nil
}

func ParseDetail(body []byte, id string) (Work, error) {
	if !ValidID(id) {
		return Work{}, errors.New("黄果 AI 作品 ID 无效")
	}
	root, err := document(body)
	if err != nil {
		return Work{}, err
	}
	d, err := initialData(root, id, 1)
	if err != nil {
		return Work{}, err
	}
	cover := d.CoverSrc
	if cover == "" {
		cover = d.PosterSrc
	}
	w := Work{Summary: Summary{SourceID: id, Title: strings.TrimSpace(d.Title), Overview: d.Description, CoverURL: cover, Tags: d.Tags}, Episodes: []Episode{}}
	for _, t := range d.TagLinks {
		label := t.Name
		if label == "" {
			label = t.Title
		}
		if label != "" {
			w.Tags = append(w.Tags, label)
		}
	}
	seen := map[int]bool{}
	if d.VideoSrc != "" || d.EpPlaySrcs["1"] != "" {
		seen[1] = true
	}
	walk(root, func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		u, e := url.Parse(attr(n, "href"))
		if e != nil || u.IsAbs() || u.Host != "" {
			return
		}
		m := episodePath.FindStringSubmatch(u.Path)
		if len(m) != 3 || m[1] != id {
			return
		}
		number := 1
		if m[2] != "" {
			number, _ = strconv.Atoi(m[2])
		}
		if number > 0 && number <= 100000 {
			seen[number] = true
		}
	})
	for number := range seen {
		path := "/video/" + id + "/"
		if number != 1 {
			path += "ep-" + strconv.Itoa(number) + "/"
		}
		w.Episodes = append(w.Episodes, Episode{Number: number, PagePath: path})
	}
	sort.Slice(w.Episodes, func(i, j int) bool { return w.Episodes[i].Number < w.Episodes[j].Number })
	if len(w.Episodes) == 0 {
		return Work{}, errors.New("黄果 AI 未提供有效分集")
	}
	return w, nil
}

// Media exists only during a download attempt and is never returned by public DTOs.
type Media struct {
	URL, Referer     string
	ExpectedDuration float64
}

var isoDuration = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?$`)

func pageDuration(root *html.Node) float64 {
	var duration float64
	count := 0
	walk(root, func(n *html.Node) {
		if n.Data != "script" || attr(n, "type") != "application/ld+json" {
			return
		}
		var data any
		if json.Unmarshal([]byte(textContent(n)), &data) != nil {
			return
		}
		var scan func(any)
		scan = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				if x["@type"] == "VideoObject" {
					s, _ := x["duration"].(string)
					m := isoDuration.FindStringSubmatch(s)
					if len(m) == 4 {
						h, _ := strconv.ParseFloat(m[1], 64)
						min, _ := strconv.ParseFloat(m[2], 64)
						sec, _ := strconv.ParseFloat(m[3], 64)
						count++
						duration = h*3600 + min*60 + sec
					}
				}
				for _, child := range x {
					scan(child)
				}
			case []any:
				for _, child := range x {
					scan(child)
				}
			}
		}
		scan(data)
	})
	if count != 1 || math.IsInf(duration, 0) || math.IsNaN(duration) {
		return 0
	}
	return duration
}

func (c *Client) Resolve(ctx context.Context, id string, number int) (Media, error) {
	if !ValidID(id) || number < 1 || number > 100000 {
		return Media{}, errors.New("黄果 AI 分集无效")
	}
	path := "/video/" + id + "/"
	if number != 1 {
		path += "ep-" + strconv.Itoa(number) + "/"
	}
	body, final, err := c.request(ctx, c.base+path, c.base+"/", 4<<20)
	if err != nil {
		return Media{}, err
	}
	root, err := document(body)
	if err != nil {
		return Media{}, err
	}
	d, err := initialData(root, id, number)
	if err != nil {
		return Media{}, err
	}
	src := d.VideoSrc
	if src == "" {
		src = d.EpPlaySrcs[strconv.Itoa(number)]
	}
	if src == "" {
		return Media{}, errors.New("该集仅提供试看或未提供完整媒体")
	}
	// The website also assigns the full player source to previewSrc; equality is not a preview flag.
	expected := pageDuration(root)
	if expected <= 0 {
		return Media{}, errors.New("该集缺少可核对的完整时长")
	}
	u, err := url.Parse(src)
	if err != nil {
		return Media{}, errors.New("黄果 AI 媒体地址无效")
	}
	base, _ := url.Parse(final)
	raw := base.ResolveReference(u).String()
	if !hongguo.ValidDownloadURL(raw) {
		return Media{}, errors.New("黄果 AI 媒体地址无效")
	}
	return Media{URL: raw, Referer: final, ExpectedDuration: expected}, nil
}

func (c *Client) Rank(ctx context.Context, key string) ([]Summary, error) {
	if !ValidRank(key) {
		return nil, errors.New("黄果 AI 榜单无效")
	}
	body, _, err := c.request(ctx, c.base+"/ranks/"+key+"/", c.base+"/", 4<<20)
	if err != nil {
		return nil, err
	}
	return ParseRank(body)
}

func ParseRank(body []byte) ([]Summary, error) {
	root, err := document(body)
	if err != nil {
		return nil, err
	}
	items := []Summary{}
	seen := map[string]bool{}
	invalid := false
	walk(root, func(n *html.Node) {
		if !hasClass(n, "hg-rank-item") {
			return
		}
		x := Summary{SourceID: attr(n, "data-track-id"), Title: attr(n, "data-track-title")}
		switch attr(n, "data-track-type-name") {
		case "短剧", "AI成人短剧":
			x.Category = "ai-duanju"
		case "漫剧", "AI成人漫剧":
			x.Category = "ai-manju"
		case "换脸", "AI换脸":
			x.Category = "ai-huanlian"
		case "魔改", "AI魔改":
			x.Category = "ai-mogai"
		}
		walk(n, func(child *html.Node) {
			if hasClass(child, "hg-rank-item__title") {
				x.Title = textContent(child)
			}
			if hasClass(child, "hg-rank-item__desc") {
				x.Overview = textContent(child)
			}
			if child.Data == "a" {
				u, e := url.Parse(attr(child, "href"))
				if e == nil && !u.IsAbs() {
					m := episodePath.FindStringSubmatch(u.Path)
					if len(m) == 3 {
						x.SourceID = m[1]
					}
				}
			}
			if child.Data == "img" && x.CoverURL == "" {
				x.CoverURL = attr(child, "data-src")
				if x.CoverURL == "" {
					x.CoverURL = attr(child, "src")
				}
			}
		})
		if !ValidID(x.SourceID) || x.Title == "" || seen[x.SourceID] {
			invalid = true
			return
		}
		seen[x.SourceID] = true
		items = append(items, x)
	})
	if invalid || len(items) == 0 || len(items) > 1000 {
		return nil, errors.New("黄果 AI 榜单结构或条目无效")
	}
	return items, nil
}

func (c *Client) Search(ctx context.Context, keyword string, page int) ([]Summary, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || len(keyword) > 200 || page < 1 || page > MaxPage {
		return nil, errors.New("黄果 AI 搜索参数无效")
	}
	body, _, err := c.request(ctx, c.base+"/search/?keyword="+url.QueryEscape(keyword)+"&page="+strconv.Itoa(page), c.base+"/", 4<<20)
	if err != nil {
		return nil, err
	}
	return ParseSearch(body)
}

func ParseSearch(body []byte) ([]Summary, error) {
	root, err := document(body)
	if err != nil {
		return nil, err
	}
	var results *html.Node
	walk(root, func(n *html.Node) {
		if hasClass(n, "hg-search-results") {
			results = n
		}
	})
	if results == nil {
		return nil, errors.New("黄果 AI 搜索结构未确认")
	}
	items := []Summary{}
	seen := map[string]bool{}
	walk(results, func(n *html.Node) {
		if !hasClass(n, "hg-drama-card") {
			return
		}
		x := Summary{SourceID: attr(n, "data-track-id")}
		x.Category = categoryName(attr(n, "data-track-type-name"))
		walk(n, func(child *html.Node) {
			if category := categoryName(attr(child, "data-track-type-name")); category != "" {
				x.Category = category
			}
			if category := attr(child, "data-cat"); ValidCategory(category) {
				x.Category = category
			}
			if child.Data == "a" {
				u, e := url.Parse(attr(child, "href"))
				if e == nil && !u.IsAbs() {
					m := episodePath.FindStringSubmatch(u.Path)
					if len(m) == 3 {
						x.SourceID = m[1]
					}
				}
			}
			if hasClass(child, "hg-drama-card__title") {
				x.Title = textContent(child)
			}
			if child.Data == "img" && x.CoverURL == "" {
				x.CoverURL = attr(child, "data-src")
				if x.CoverURL == "" {
					x.CoverURL = attr(child, "src")
				}
			}
		})
		if ValidID(x.SourceID) && x.Title != "" && !seen[x.SourceID] {
			seen[x.SourceID] = true
			items = append(items, x)
		}
	})
	return items, nil
}

func categoryName(name string) string {
	switch name {
	case "短剧", "AI成人短剧":
		return "ai-duanju"
	case "漫剧", "AI成人漫剧":
		return "ai-manju"
	case "换脸", "AI换脸":
		return "ai-huanlian"
	case "魔改", "AI魔改":
		return "ai-mogai"
	}
	return ""
}
