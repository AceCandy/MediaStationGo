package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"golang.org/x/net/html"
)

// DoubanRichCredit 只属于共享豆瓣资料，不创建本地媒体或人物记录。
type DoubanRichCredit struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Type      string `json:"type"`
	SourceURL string `json:"source_url,omitempty"`
	ImageKey  string `json:"image_key,omitempty"`
}

// DoubanRatingShare 保存网页明确给出的星级占比，缺失时不推算。
type DoubanRatingShare struct {
	Stars   int     `json:"stars"`
	Percent float64 `json:"percent"`
}

// DoubanAward 保存公开网页奖项名称、获奖或提名类别及相关人员。
type DoubanAward struct {
	Event    string `json:"event"`
	Category string `json:"category"`
	People   string `json:"people"`
}

// DoubanRichPayload 与原始 API 响应分开保存，网页失败不清空已有补充资料。
type DoubanRichPayload struct {
	FetchedAt    time.Time           `json:"fetched_at"`
	Writers      []string            `json:"writers,omitempty"`
	IMDbID       string              `json:"imdb_id,omitempty"`
	Distribution []DoubanRatingShare `json:"distribution,omitempty"`
	Credits      []DoubanRichCredit  `json:"credits,omitempty"`
	Awards       []DoubanAward       `json:"awards,omitempty"`
}

var doubanIMDbPattern = regexp.MustCompile(`^tt[0-9]+$`)
var doubanStyleURLPattern = regexp.MustCompile(`url\(["']?([^"')]+)["']?\)`)

type doubanWebRequestKey struct{}

func doubanRichPayload(subject *model.DoubanSubject) DoubanRichPayload {
	var rich DoubanRichPayload
	if subject != nil && subject.RichPayload != nil {
		_ = json.Unmarshal([]byte(*subject.RichPayload), &rich)
	}
	return rich
}

// refreshRichSubject 共用详情有效期及串行传输，只有明确刷新才访问公开网页。
func (d *DoubanProvider) refreshRichSubject(ctx context.Context, id, kind string, force bool) (bool, error) {
	started := time.Now().UTC()
	release, err := d.enterDataRequest(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	observed, err := d.dataRepo.Douban.Find(ctx, id, kind)
	if err != nil {
		return false, err
	}
	if observed == nil {
		return false, ErrDoubanSubjectMissing
	}
	old := doubanRichPayload(observed)
	if !old.FetchedAt.IsZero() && time.Since(old.FetchedAt) < d.detailTTL(ctx) && (!force || old.FetchedAt.After(started)) {
		return true, nil
	}
	if !force && observed.RichNextRetryAt != nil && time.Now().Before(*observed.RichNextRetryAt) {
		return false, nil
	}
	failure := func() (bool, error) {
		return false, d.dataRepo.Douban.RecordRichFailure(ctx, observed, time.Now().UTC().Add(5*time.Minute))
	}
	pageURL := "https://movie.douban.com/subject/" + url.PathEscape(id) + "/"
	body, status, err := d.requestJSONRemote(context.WithValue(ctx, doubanWebRequestKey{}, true), pageURL, "https://movie.douban.com/")
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil || status != 200 {
		return failure()
	}
	rich, err := parseDoubanRichHTML(body, id)
	if err != nil {
		return failure()
	}
	rich.FetchedAt = time.Now().UTC().Truncate(time.Microsecond)
	for range 4 {
		old = doubanRichPayload(observed)
		for i := range rich.Credits {
			rich.Credits[i].ImageKey = ""
			for _, previous := range old.Credits {
				if rich.Credits[i].SourceURL != "" && rich.Credits[i].SourceURL == previous.SourceURL {
					rich.Credits[i].ImageKey = previous.ImageKey
					break
				}
			}
		}
		raw, err := json.Marshal(rich)
		if err != nil {
			return false, err
		}
		saved, err := d.dataRepo.Douban.SetRichPayload(ctx, observed, raw)
		if saved || err != nil {
			return saved, err
		}
		current, err := d.dataRepo.Douban.FindSubjectByID(ctx, observed.ID)
		if err != nil {
			return false, err
		}
		if current == nil || current.FetchedAt == nil || observed.FetchedAt == nil || !current.FetchedAt.Equal(*observed.FetchedAt) {
			return false, nil
		}
		if current.RichFetchedAt != nil && current.RichFetchedAt.After(rich.FetchedAt) {
			return true, nil
		}
		observed = current
	}
	return false, nil
}

// parseDoubanRichHTML 验证条目身份和核心资料区，拒绝安全验证页、登录页及错页。
func parseDoubanRichHTML(body []byte, id string) (DoubanRichPayload, error) {
	var result DoubanRichPayload
	if len(body) > 4<<20 {
		return result, errors.New("douban webpage too large")
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	identity := false
	walkDoubanHTML(doc, func(n *html.Node) {
		if n.Data == "meta" && doubanHTMLAttr(n, "property") == "og:url" {
			u, err := url.Parse(doubanHTMLAttr(n, "content"))
			identity = err == nil && u.Hostname() == "movie.douban.com" && u.Path == "/subject/"+id+"/"
		}
	})
	info := findDoubanHTML(doc, func(n *html.Node) bool { return doubanHTMLAttr(n, "id") == "info" })
	heading := findDoubanHTML(doc, func(n *html.Node) bool { return n.Data == "h1" })
	if !identity || strings.TrimSpace(doubanHTMLText(info)) == "" || strings.TrimSpace(doubanHTMLText(heading)) == "" {
		return result, errors.New("douban webpage identity unavailable")
	}
	for _, line := range strings.Split(doubanHTMLText(info), "\n") {
		label, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(label) {
		case "编剧":
			result.Writers = splitDoubanNames(value)
		case "IMDb":
			if v := strings.TrimSpace(value); doubanIMDbPattern.MatchString(v) {
				result.IMDbID = v
			}
		}
	}
	ratings := findDoubanHTML(doc, func(n *html.Node) bool { return doubanHTMLClass(n, "ratings-on-weight") })
	walkDoubanHTML(ratings, func(n *html.Node) {
		if !doubanHTMLClass(n, "item") {
			return
		}
		star := findDoubanHTML(n, func(x *html.Node) bool { return doubanHTMLClass(x, "starstop") })
		percent := findDoubanHTML(n, func(x *html.Node) bool { return doubanHTMLClass(x, "rating_per") })
		stars, e1 := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(doubanHTMLText(star)), "星"))
		value, e2 := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(doubanHTMLText(percent)), "%"), 64)
		if e1 == nil && e2 == nil && stars >= 1 && stars <= 5 && !math.IsNaN(value) && value >= 0 && value <= 100 {
			result.Distribution = append(result.Distribution, DoubanRatingShare{stars, value})
		}
	})
	celebrities := findDoubanHTML(doc, func(n *html.Node) bool { return doubanHTMLAttr(n, "id") == "celebrities" })
	walkDoubanHTML(celebrities, func(n *html.Node) {
		if !doubanHTMLClass(n, "celebrity") {
			return
		}
		name := findDoubanHTML(n, func(x *html.Node) bool { return doubanHTMLClass(x, "name") })
		role := findDoubanHTML(n, func(x *html.Node) bool { return doubanHTMLClass(x, "role") })
		avatar := findDoubanHTML(n, func(x *html.Node) bool { return doubanHTMLClass(x, "avatar") })
		c := DoubanRichCredit{Name: strings.TrimSpace(doubanHTMLText(name)), Role: doubanHTMLAttr(role, "title"), Type: "Actor"}
		if c.Role == "" {
			c.Role = strings.TrimSpace(doubanHTMLText(role))
		}
		if c.Role == "导演" {
			c.Type = "Director"
		}
		if c.Role == "编剧" {
			c.Type = "Writer"
		}
		if m := doubanStyleURLPattern.FindStringSubmatch(doubanHTMLAttr(avatar, "style")); len(m) == 2 && validDoubanProfileURL(m[1]) {
			c.SourceURL = m[1]
		}
		if c.Name != "" {
			result.Credits = append(result.Credits, c)
		}
	})
	walkDoubanHTML(doc, func(n *html.Node) {
		if n.Data != "ul" || !doubanHTMLClass(n, "award") {
			return
		}
		var cells []string
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Data == "li" {
				cells = append(cells, strings.Join(strings.Fields(doubanHTMLText(child)), " "))
			}
		}
		if len(cells) >= 2 && cells[0] != "" && cells[1] != "" {
			award := DoubanAward{Event: cells[0], Category: cells[1]}
			if len(cells) > 2 {
				award.People = cells[2]
			}
			result.Awards = append(result.Awards, award)
		}
	})
	return result, nil
}

func validDoubanProfileURL(source string) bool {
	u, err := url.Parse(source)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && strings.HasSuffix(u.Hostname(), ".doubanio.com") && strings.HasPrefix(u.Path, "/view/")
}

func splitDoubanNames(value string) []string {
	var values []string
	for _, part := range strings.Split(value, "/") {
		if v := strings.TrimSpace(part); v != "" {
			values = append(values, v)
		}
	}
	return values
}

func doubanHTMLAttr(n *html.Node, key string) string {
	if n != nil {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
	}
	return ""
}
func doubanHTMLClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(doubanHTMLAttr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}
func walkDoubanHTML(n *html.Node, visit func(*html.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkDoubanHTML(c, visit)
	}
}
func findDoubanHTML(n *html.Node, test func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if test(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findDoubanHTML(c, test); found != nil {
			return found
		}
	}
	return nil
}
func doubanHTMLText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Data == "br" {
		return "\n"
	}
	if n.Data == "script" || n.Data == "style" {
		return ""
	}
	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text.WriteString(doubanHTMLText(c))
	}
	return text.String()
}

func doubanCreditImageURL(id string, index int) string {
	return fmt.Sprintf("/api/admin/douban/subjects/%s/credits/%d/image", url.PathEscape(id), index)
}

// DoubanHonor 展示接口已返回的榜单名称与明确排名，未提供排名时只显示名称。
type DoubanHonor struct {
	Title string `json:"title"`
	Rank  int    `json:"rank"`
}

func populateDoubanRichDetail(detail *DoubanSubjectDetail, subject *model.DoubanSubject) {
	raw := doubanCacheSubject([]byte(*subject.Payload))
	rich := doubanRichPayload(subject)
	detail.Directors = doubanCreditNames(raw["directors"])
	detail.Actors = doubanCreditNames(raw["actors"])
	detail.Writers = doubanCreditNames(raw["writers"])
	if len(rich.Writers) > 0 {
		detail.Writers = rich.Writers
	}
	detail.Aliases = stringsFromMap(raw, "aka")
	detail.Durations = stringsFromMap(raw, "durations")
	detail.ReleaseDates = stringsFromMap(raw, "pubdate")
	if len(detail.ReleaseDates) == 0 && detail.ReleaseDate != "" {
		detail.ReleaseDates = []string{detail.ReleaseDate}
	}
	detail.IMDbID = rich.IMDbID
	if detail.IMDbID == "" {
		if v := firstStringFromMap(raw, "imdb_id"); doubanIMDbPattern.MatchString(v) {
			detail.IMDbID = v
		}
	}
	if rating, ok := raw["rating"].(map[string]any); ok {
		detail.RatingCount = positiveIntFromMap(rating, "count")
	}
	detail.RatingDistribution = rich.Distribution
	detail.Awards = rich.Awards
	if !rich.FetchedAt.IsZero() {
		detail.RichFetchedAt = &rich.FetchedAt
	}
	for i, credit := range rich.Credits {
		image := ""
		if credit.SourceURL != "" || credit.ImageKey != "" {
			image = doubanCreditImageURL(subject.ID, i) + "?v=" + url.QueryEscape(rich.FetchedAt.Format(time.RFC3339Nano))
		}
		detail.Credits = append(detail.Credits, MediaCredit{PersonID: fmt.Sprintf("douban-%s-%d", subject.DoubanID, i), Name: credit.Name, Role: credit.Role, Type: credit.Type, ProfileURL: image})
	}
	if len(detail.Credits) == 0 {
		for _, group := range []struct {
			names []string
			kind  string
		}{{detail.Directors, "Director"}, {detail.Actors, "Actor"}, {detail.Writers, "Writer"}} {
			for _, name := range group.names {
				detail.Credits = append(detail.Credits, MediaCredit{PersonID: fmt.Sprintf("douban-%s-%d", subject.DoubanID, len(detail.Credits)), Name: name, Type: group.kind})
			}
		}
	}
	seen := map[string]bool{}
	for _, key := range []string{"honor_infos", "subject_collections"} {
		values, _ := raw[key].([]any)
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				title := firstStringFromMap(item, "title")
				if title != "" && !seen[title] {
					seen[title] = true
					detail.Honors = append(detail.Honors, DoubanHonor{Title: title, Rank: positiveIntFromMap(item, "rank")})
				}
			}
		}
	}
}

func doubanCreditNames(value any) []string {
	var names []string
	if values, ok := value.([]any); ok {
		for _, item := range values {
			var name string
			switch v := item.(type) {
			case string:
				name = strings.TrimSpace(v)
			case map[string]any:
				name = firstStringFromMap(v, "name")
			}
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}
