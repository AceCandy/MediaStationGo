package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	adultFC2Pattern         = regexp.MustCompile(`(?i)\bFC2[-_\s]?(?:PPV[-_\s]?)?(\d{5,8})\b`)
	adultHEYZOPattern       = regexp.MustCompile(`(?i)\bHEYZO[-_\s]?(\d{3,6})\b`)
	adultUncensoredPattern  = regexp.MustCompile(`(?i)\b(\d{6})[-_](\d{3,5})\b`)
	adultStandardPattern    = regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])([A-Z]{2,10})[-_\s]?(\d{2,8})(?:[^A-Z0-9]|$)`)
	adultTitlePattern       = regexp.MustCompile(`(?is)<h[123][^>]*>(.*?)</h[123]>`)
	adultTagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
	adultAnchorPattern      = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	adultImagePattern       = regexp.MustCompile(`(?is)<img\b([^>]*)>`)
	adultJavBusCoverPattern = regexp.MustCompile(`(?is)class="bigImage"[^>]*href="([^"]+)"`)
	adultSamplePattern      = regexp.MustCompile(`(?is)<a[^>]+class="[^"]*\bsample-box\b[^"]*"[^>]+href="([^"]+)"`)
	adultAttrPattern        = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*["']([^"']*)["']`)
)

var adultExcludedPrefixes = map[string]struct{}{
	"AC": {}, "AAC": {}, "AVC": {}, "BD": {}, "CD": {}, "DDP": {}, "DTS": {},
	"FHD": {}, "HD": {}, "HEVC": {}, "HDR": {}, "MP": {}, "SD": {}, "UHD": {},
	"WEB": {}, "X264": {}, "X265": {},
}

type AdultProvider struct {
	log       *zap.Logger
	client    *http.Client
	apiConfig *APIConfigService
}

func NewAdultProvider(log *zap.Logger, apiConfig *APIConfigService) *AdultProvider {
	return &AdultProvider{
		log:       log,
		apiConfig: apiConfig,
		client:    NewExternalHTTPClient(12 * time.Second),
	}
}

func (p *AdultProvider) Enabled() bool {
	return p != nil
}

func (p *AdultProvider) Search(ctx context.Context, code string) (*Match, error) {
	code = normalizeAdultCode(code)
	if code == "" {
		return nil, errors.New("empty adult code")
	}
	bases := p.resolveBases(ctx)
	if len(bases) == 0 {
		return nil, nil
	}
	var lastErr error
	for _, base := range bases {
		base = strings.TrimRight(base, "/")
		var match *Match
		var err error
		if strings.Contains(base, "javbus") {
			match, err = p.scrapeJavBus(ctx, base, code)
		} else {
			match, err = p.scrapeJavDB(ctx, base, code)
		}
		if err != nil {
			lastErr = err
			if p.log != nil {
				p.log.Debug("adult scrape source failed", zap.String("base", base), zap.String("code", code), zap.Error(err))
			}
			continue
		}
		if match != nil {
			match.OriginalName = code
			match.NSFW = true
			return match, nil
		}
	}
	return nil, lastErr
}

func (p *AdultProvider) resolveBases(ctx context.Context) []string {
	out := []string{"https://javdb.com", "https://www.javbus.com"}
	if p.apiConfig == nil {
		return out
	}
	resolved, err := p.apiConfig.Resolve(ctx, "adult")
	if err != nil {
		return out
	}
	if !resolved.Enabled && (resolved.BaseURL != "" || resolved.Extra != "" || resolved.APIKey != "") {
		return nil
	}
	if resolved.BaseURL != "" {
		out = []string{resolved.BaseURL}
	}
	if resolved.Extra != "" {
		for _, part := range strings.Split(resolved.Extra, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "http://") || strings.HasPrefix(part, "https://") {
				out = append(out, part)
			}
		}
	}
	return dedupeStrings(out)
}

func (p *AdultProvider) scrapeJavDB(ctx context.Context, base, code string) (*Match, error) {
	searchURL := base + "/search?q=" + url.QueryEscape(code) + "&f=all"
	body, err := p.fetchText(ctx, searchURL, base)
	if err != nil {
		return nil, err
	}
	detail := ""
	for _, found := range adultAnchorPattern.FindAllStringSubmatch(body, -1) {
		if len(found) < 3 {
			continue
		}
		attrs := adultAttrs(found[1])
		if !strings.Contains(" "+attrs["class"]+" ", " box ") || attrs["href"] == "" {
			continue
		}
		if strings.Contains(strings.ToUpper(stripAdultHTML(found[2])), code) {
			detail = absolutizeURL(base, attrs["href"])
			break
		}
	}
	if detail == "" {
		return nil, nil
	}
	body, err = p.fetchText(ctx, detail, base)
	if err != nil {
		return nil, err
	}
	return parseAdultDetailHTML(body, code, "javdb", detail), nil
}

func (p *AdultProvider) scrapeJavBus(ctx context.Context, base, code string) (*Match, error) {
	body, err := p.fetchText(ctx, base+"/"+url.PathEscape(code), base)
	if err != nil {
		return nil, err
	}
	return parseAdultDetailHTML(body, code, "javbus", base+"/"+url.PathEscape(code)), nil
}

func (p *AdultProvider) fetchText(ctx context.Context, targetURL, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	applyAdultHeaders(req, referer)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("adult source %s returned %d", targetURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func parseAdultDetailHTML(body, code, source, detailURL string) *Match {
	match := &Match{
		OriginalName: code,
		NSFW:         true,
		Genres:       []string{"Adult", source},
	}
	if title := firstAdultTitle(body, code); title != "" {
		match.Title = title
	}
	if match.Title == "" {
		return nil
	}
	if source == "javbus" {
		if m := adultJavBusCoverPattern.FindStringSubmatch(body); len(m) > 1 {
			match.PosterURL = absolutizeURL(detailURL, m[1])
		}
	} else if cover := firstAdultImage(body, "video-cover", "cover", "column-video-cover"); cover != "" {
		match.PosterURL = absolutizeURL(detailURL, cover)
	}
	if m := adultSamplePattern.FindStringSubmatch(body); len(m) > 1 {
		match.BackdropURL = absolutizeURL(detailURL, m[1])
	}
	match.Year = firstYearInText(body)
	match.Rating = firstRatingInText(body)
	return match
}

func firstAdultTitle(body, code string) string {
	for _, found := range adultTitlePattern.FindAllStringSubmatch(body, -1) {
		if len(found) < 2 {
			continue
		}
		title := strings.TrimSpace(stripAdultHTML(found[1]))
		if title == "" {
			continue
		}
		title = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(title, code), strings.ToUpper(code)))
		if title != "" {
			return title
		}
	}
	return ""
}

func applyAdultHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,ja;q=0.8,en;q=0.7")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

func AdultCodeFromMediaPath(path string) string {
	if code := normalizeAdultCode(filepath.Base(path)); code != "" {
		return code
	}
	return normalizeAdultCode(path)
}

func normalizeAdultCode(input string) string {
	input = strings.ToUpper(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	input = strings.ReplaceAll(input, "_", "-")
	if m := adultFC2Pattern.FindStringSubmatch(input); len(m) > 1 {
		return "FC2-PPV-" + m[1]
	}
	if m := adultHEYZOPattern.FindStringSubmatch(input); len(m) > 1 {
		return "HEYZO-" + m[1]
	}
	if m := adultUncensoredPattern.FindStringSubmatch(input); len(m) > 2 {
		return m[1] + "-" + m[2]
	}
	for _, m := range adultStandardPattern.FindAllStringSubmatch(input, -1) {
		if len(m) < 3 {
			continue
		}
		prefix := strings.TrimSpace(m[1])
		if _, excluded := adultExcludedPrefixes[prefix]; excluded {
			continue
		}
		return prefix + "-" + m[2]
	}
	return ""
}

func stripAdultHTML(value string) string {
	value = adultTagPattern.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(html.UnescapeString(value)), " ")
}

func firstAdultImage(body string, classNeedles ...string) string {
	for _, found := range adultImagePattern.FindAllStringSubmatch(body, -1) {
		if len(found) < 2 {
			continue
		}
		attrs := adultAttrs(found[1])
		class := strings.ToLower(attrs["class"])
		for _, needle := range classNeedles {
			if strings.Contains(class, strings.ToLower(needle)) {
				if attrs["src"] != "" {
					return attrs["src"]
				}
				if attrs["data-src"] != "" {
					return attrs["data-src"]
				}
			}
		}
	}
	return ""
}

func adultAttrs(raw string) map[string]string {
	out := map[string]string{}
	for _, found := range adultAttrPattern.FindAllStringSubmatch(raw, -1) {
		if len(found) >= 3 {
			out[strings.ToLower(found[1])] = html.UnescapeString(found[2])
		}
	}
	return out
}

func absolutizeURL(base, raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err == nil && u.IsAbs() {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	return b.ResolveReference(u).String()
}

func firstYearInText(body string) int {
	m := regexp.MustCompile(`(?:19|20)\d{2}[-/.]\d{1,2}[-/.]\d{1,2}`).FindString(body)
	if len(m) >= 4 {
		year, _ := strconv.Atoi(m[:4])
		return year
	}
	return 0
}

func firstRatingInText(body string) float32 {
	m := regexp.MustCompile(`(?i)(?:score|rating|評分|评分)[^0-9]{0,20}([0-9](?:\.[0-9])?)`).FindStringSubmatch(body)
	if len(m) > 1 {
		v, _ := strconv.ParseFloat(m[1], 32)
		return float32(v)
	}
	return 0
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
