// Package service — Douban (豆瓣) metadata provider.
//
// Douban is the dominant Chinese movie/TV rating and metadata site. Its
// unofficial API returns rich Chinese-language titles, overviews, ratings
// and poster URLs. A valid Douban cookie is required to avoid IP bans.
//
// We use the search endpoint at:
//
//	https://movie.douban.com/j/subject_suggest?q=...
//
// And the detail endpoint at:
//
//	https://m.douban.com/rexxar/api/v2/movie/...
//
// The provider is used as a supplemental source: after TMDb matches we
// attempt a Douban lookup to grab a localized Chinese title + overview.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// DoubanProvider talks to the unofficial Douban movie API.
type DoubanProvider struct {
	apiConfig *APIConfigService
	client    *http.Client
}

// NewDoubanProvider is the constructor.
func NewDoubanProvider(apiConfig *APIConfigService) *DoubanProvider {
	return &DoubanProvider{
		apiConfig: apiConfig,
		client:    NewExternalHTTPClient(15 * time.Second),
	}
}

// Enabled reports whether Douban lookup is available. Public movie.douban.com
// suggest endpoints work without an API key; a cookie is optional and only
// helps when Douban applies stricter anti-scraping rules.
func (d *DoubanProvider) Enabled() bool {
	return true
}

// userAgents for anti-scraping randomization.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
}

// ErrDoubanTemporarilyUnavailable 表示移动详情接口应稍后重试。
var ErrDoubanTemporarilyUnavailable = errors.New("douban temporarily unavailable")

// ErrDoubanSubjectNotFound 表示豆瓣已明确确认条目不存在。
var ErrDoubanSubjectNotFound = errors.New("douban subject not found")

// DoubanMatch is the result of a Douban search hit.
type DoubanMatch struct {
	DoubanID string  `json:"douban_id"`
	Title    string  `json:"title"`
	Year     string  `json:"year"`
	Img      string  `json:"img"`
	Rating   float32 `json:"rating"`
	Type     string  `json:"type,omitempty"`
}

// Search runs a Douban subject_suggest query and returns the top match.
func (d *DoubanProvider) Search(ctx context.Context, query string) (*DoubanMatch, error) {
	if !d.Enabled() || query == "" {
		return nil, nil
	}
	u := "https://movie.douban.com/j/subject_suggest?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	d.setHeaders(ctx, req)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("douban search: %d", resp.StatusCode)
	}

	type suggestion struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Year  string `json:"year"`
		Img   string `json:"img"`
		Type  string `json:"type"`
	}
	var results []suggestion
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	r := results[0]
	return &DoubanMatch{
		DoubanID: r.ID,
		Title:    r.Title,
		Year:     r.Year,
		Img:      d.ResolveArtworkURL(ctx, deriveDoubanLargePosterURL(r.Img)),
		Type:     r.Type,
	}, nil
}

func (d *DoubanProvider) SearchMatch(ctx context.Context, query string) (*Match, error) {
	got, err := d.Search(ctx, query)
	if err != nil || got == nil {
		return nil, err
	}
	mediaType := ""
	if strings.TrimSpace(got.Type) != "" {
		mediaType = normalizeMediaType(got.Type, got.Title, "")
	}
	match := &Match{
		Source:    "douban",
		DoubanID:  got.DoubanID,
		MediaType: mediaType,
		Title:     got.Title,
		PosterURL: got.Img,
		Rating:    got.Rating,
	}
	if len(got.Year) >= 4 {
		_, _ = fmt.Sscanf(got.Year[:4], "%d", &match.Year)
	}
	return match, nil
}

func (d *DoubanProvider) GetMatchByID(ctx context.Context, doubanID string) (*Match, error) {
	doubanID = strings.TrimSpace(doubanID)
	if doubanID == "" {
		return nil, nil
	}
	rawJSON, err := d.getDetailRawJSON(ctx, doubanID)
	if err != nil {
		return nil, err
	}
	match, err := doubanMatchFromRawJSON(doubanID, rawJSON)
	if match != nil {
		match.PosterURL = d.ResolveArtworkURL(ctx, match.PosterURL)
	}
	return match, err
}

// GetEnrichmentMatchByID 只使用移动详情接口，供需要完整快照的补齐流程调用。
func (d *DoubanProvider) GetEnrichmentMatchByID(ctx context.Context, doubanID string) (*Match, error) {
	doubanID = strings.TrimSpace(doubanID)
	if doubanID == "" {
		return nil, nil
	}
	rawJSON, err := d.getMobileDetailRawJSON(ctx, doubanID)
	if err != nil {
		return nil, err
	}
	match, err := doubanMatchFromRawJSON(doubanID, rawJSON)
	if err != nil {
		return nil, ErrDoubanTemporarilyUnavailable
	}
	match.PosterURL = d.ResolveArtworkURL(ctx, match.PosterURL)
	return match, nil
}

func (d *DoubanProvider) getDetailRawJSON(ctx context.Context, doubanID string) ([]byte, error) {
	if rawJSON, err := d.getMobileDetailRawJSON(ctx, doubanID); err == nil {
		return rawJSON, nil
	}
	rawJSON, _, err := d.requestDetailRawJSON(
		ctx,
		"https://movie.douban.com/j/subject_abstract?subject_id="+url.QueryEscape(doubanID),
		"https://movie.douban.com/",
	)
	return rawJSON, err
}

func (d *DoubanProvider) getMobileDetailRawJSON(ctx context.Context, doubanID string) ([]byte, error) {
	escapedID := url.PathEscape(doubanID)
	rawJSON, status, err := d.requestDetailRawJSON(
		ctx,
		"https://m.douban.com/rexxar/api/v2/movie/"+escapedID,
		"https://m.douban.com/subject/"+escapedID+"/",
	)
	if status == http.StatusNotFound {
		return nil, ErrDoubanSubjectNotFound
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		if status == 0 || status == http.StatusForbidden || status == http.StatusTooManyRequests || status >= 500 || status < 400 {
			return nil, ErrDoubanTemporarilyUnavailable
		}
		return nil, err
	}
	if notFound, failed := doubanDetailResponseFailure(rawJSON); failed {
		if notFound {
			return nil, ErrDoubanSubjectNotFound
		}
		return nil, ErrDoubanTemporarilyUnavailable
	}
	return rawJSON, nil
}

func (d *DoubanProvider) requestDetailRawJSON(ctx context.Context, requestURL, referer string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, err
	}
	d.setHeaders(ctx, req)
	req.Header.Set("Referer", referer)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	rawJSON, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode >= 400:
		return nil, resp.StatusCode, fmt.Errorf("douban detail: %d", resp.StatusCode)
	case readErr != nil:
		return nil, resp.StatusCode, readErr
	case !json.Valid(rawJSON):
		return nil, resp.StatusCode, errors.New("douban detail: invalid json")
	default:
		return rawJSON, resp.StatusCode, nil
	}
}

func doubanDetailResponseFailure(rawJSON []byte) (notFound, failed bool) {
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil || raw == nil {
		return false, true
	}
	code := strings.ToLower(firstStringFromMap(raw, "code", "error_code"))
	if code != "" {
		return strings.Contains(code, "not_found") || strings.Contains(code, "not found") || strings.Contains(code, "not_exist"), true
	}
	value, ok := raw["error"]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case nil:
		return false, false
	case bool:
		return false, typed
	case string:
		typed = strings.ToLower(strings.TrimSpace(typed))
		if typed == "" {
			return false, false
		}
		return strings.Contains(typed, "not found") || strings.Contains(typed, "not_exist"), true
	default:
		return false, true
	}
}

func doubanMatchFromRawJSON(doubanID string, rawJSON []byte) (*Match, error) {
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return nil, err
	}
	subject := raw
	if nested, ok := raw["subject"].(map[string]any); ok {
		subject = nested
	} else if nested, ok := raw["data"].(map[string]any); ok {
		subject = nested
	}
	title := firstStringFromMap(subject, "title", "name")
	year := 0
	if y := firstStringFromMap(subject, "year"); len(y) >= 4 {
		_, _ = fmt.Sscanf(y[:4], "%d", &year)
	}
	m := &Match{
		Source:       "douban",
		DoubanID:     strings.TrimSpace(doubanID),
		TMDbID:       positiveIntFromMap(subject, "tmdb_id", "tmdbid"),
		Title:        title,
		OriginalName: firstStringFromMap(subject, "original_title", "original_name"),
		Overview:     firstStringFromMap(subject, "short_comment", "intro", "summary", "abstract"),
		PosterURL:    doubanPosterURL(subject),
		Year:         year,
		ReleaseDate:  normalizeReleaseDate(firstStringFromMap(subject, "release_date", "pubdate")),
		Rating:       float32FromMap(subject, "rate", "rating"),
		Languages:    stringsFromMap(subject, "languages", "language"),
		Countries:    stringsFromMap(subject, "countries", "country", "regions"),
		Genres:       stringsFromMap(subject, "genres", "genre"),
		RawJSON:      rawJSON,
	}
	if m.TMDbID == 0 {
		m.TMDbID = positiveIntFromMap(raw, "tmdb_id", "tmdbid")
	}
	m.AllowIdentifierMerge = true
	if m.Title == "" {
		m.Title = firstStringFromMap(raw, "title")
	}
	return m, nil
}

func doubanPosterURL(subject map[string]any) string {
	for _, path := range [][]string{
		{"cover", "image", "large", "url"},
		{"pic", "large"},
	} {
		if value := stringFromMapPath(subject, path...); validRemoteArtworkURL(value) {
			return value
		}
	}
	return ""
}

func stringFromMapPath(values map[string]any, path ...string) string {
	var current any = values
	for _, key := range path {
		nested, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = nested[key]
	}
	value, _ := current.(string)
	return strings.TrimSpace(value)
}

// ResolveArtworkURL applies the current Douban image origin and format without changing the JSON API endpoint.
func (d *DoubanProvider) ResolveArtworkURL(ctx context.Context, raw string) string {
	sourceURL := strings.TrimSpace(raw)
	if sourceURL == "" {
		return ""
	}
	if largeURL := deriveDoubanLargePosterURL(sourceURL); largeURL != "" {
		sourceURL = largeURL
	}
	if d == nil || d.apiConfig == nil {
		return sourceURL
	}
	resolved, err := d.apiConfig.Resolve(ctx, "douban")
	if err != nil || !resolved.Enabled || strings.TrimSpace(resolved.BaseURL) == "" {
		return sourceURL
	}
	origin, err := url.Parse(resolved.BaseURL)
	if err != nil || origin.Host == "" {
		return sourceURL
	}
	target, err := url.Parse(sourceURL)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return sourceURL
	}
	target.Scheme, target.Host, target.User = origin.Scheme, origin.Host, nil
	if ext := path.Ext(target.Path); ext != "" {
		target.Path = strings.TrimSuffix(target.Path, ext) + ".webp"
		target.RawPath = ""
	}
	target.RawQuery = strings.NewReplacer(
		"/format/jpg", "/format/webp",
		"/format/jpeg", "/format/webp",
	).Replace(target.RawQuery)
	return target.String()
}

func deriveDoubanLargePosterURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	const prefix = "/view/photo/"
	if !strings.HasPrefix(u.Path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(u.Path, prefix)
	publicAt := strings.Index(rest, "/public/")
	if publicAt <= 0 || strings.TrimSpace(rest[publicAt+len("/public/"):]) == "" {
		return ""
	}
	u.Path = prefix + "l" + rest[publicAt:]
	u.RawPath = ""
	return u.String()
}

func (d *DoubanProvider) GetEpisodeCount(ctx context.Context, query string) (int, error) {
	match, err := d.Search(ctx, query)
	if err != nil || match == nil || strings.TrimSpace(match.DoubanID) == "" {
		return 0, err
	}
	return d.GetEpisodeCountByID(ctx, match.DoubanID)
}

func (d *DoubanProvider) GetEpisodeCountByID(ctx context.Context, doubanID string) (int, error) {
	doubanID = strings.TrimSpace(doubanID)
	if doubanID == "" {
		return 0, nil
	}
	rawJSON, err := d.getDetailRawJSON(ctx, doubanID)
	if err != nil {
		return 0, err
	}
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return 0, err
	}
	for _, key := range []string{"episode_count", "episodes_count", "episodes", "eps"} {
		if count := doubanEpisodeCountFromValue(raw[key]); count > 0 {
			return count, nil
		}
	}
	for _, key := range []string{"subject", "data"} {
		if nested, ok := raw[key].(map[string]any); ok {
			for _, field := range []string{"episode_count", "episodes_count", "episodes", "eps"} {
				if count := doubanEpisodeCountFromValue(nested[field]); count > 0 {
					return count, nil
				}
			}
		}
	}
	return 0, nil
}

func (d *DoubanProvider) setHeaders(ctx context.Context, req *http.Request) {
	req.Header.Set("User-Agent", userAgents[secureRandomIntn(len(userAgents))])
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if d.apiConfig == nil {
		return
	}
	resolved, err := d.apiConfig.Resolve(ctx, "douban")
	if err != nil || !resolved.Enabled {
		return
	}
	if cookie := strings.TrimSpace(resolved.APIKey); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}
