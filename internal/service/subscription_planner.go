// Package service — subscription planning and release candidate selection.
package service

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var (
	seriesPackRE = regexp.MustCompile(`(?i)(complete|batch|合集|全集|全\s*\d+\s*[集话話期]|整季|全季|s\d{1,2}\s*(?:complete|batch|pack)|season\s*\d{1,2}\s*(?:complete|batch|pack)|s\d{1,2}e\d{1,3}\s*[-~–—]\s*(?:e)?\d{1,3}|第\s*\d+\s*[-~–—]\s*\d+\s*[集话話期])`)
	seasonOnlyRE = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:s|season)\s*\d{1,2}(?:[\s._-]|$)|第\s*\d+\s*季`)
)

type siteSearchCandidate struct {
	Item     SearchResult
	Download string
	GUID     string
	Season   int
	Episode  int
	Pack     bool
	Score    int
}

// SubscriptionPlanner owns release selection decisions for subscriptions:
// rule matching, candidate scoring, and filtering against known availability.
type SubscriptionPlanner struct{}

func selectSiteSearchCandidates(results []SearchResult, sub *model.Subscription, seenSet map[string]struct{}, availability ...LocalAvailability) []siteSearchCandidate {
	return SubscriptionPlanner{}.SelectSiteSearchCandidates(results, sub, seenSet, availability...)
}

func (SubscriptionPlanner) SelectSiteSearchCandidates(results []SearchResult, sub *model.Subscription, seenSet map[string]struct{}, availability ...LocalAvailability) []siteSearchCandidate {
	if sub == nil {
		return nil
	}
	if seenSet == nil {
		seenSet = map[string]struct{}{}
	}
	local := LocalAvailability{}
	if len(availability) > 0 {
		local = availability[0]
	}
	return selectSiteSearchCandidatesWithAvailability(results, sub, seenSet, local)
}

func selectSiteSearchCandidatesWithAvailability(results []SearchResult, sub *model.Subscription, seenSet map[string]struct{}, local LocalAvailability) []siteSearchCandidate {
	candidates := make([]siteSearchCandidate, 0, len(results))
	for _, item := range results {
		if !matchesSubscriptionRules(sub, item.Title) {
			continue
		}
		download := strings.TrimSpace(item.DownloadURL)
		if download == "" {
			download = strings.TrimSpace(item.TorrentURL)
		}
		if download == "" {
			continue
		}
		guid := stableSiteSearchGUID(item, download)
		if _, ok := seenSet[guid]; ok {
			continue
		}
		season, episode := ParseEpisode(item.Title)
		score := subscriptionCandidateScore(sub, item)
		candidates = append(candidates, siteSearchCandidate{
			Item:     item,
			Download: download,
			GUID:     guid,
			Season:   season,
			Episode:  episode,
			Pack:     isSeriesPackTitle(item.Title),
			Score:    score,
		})
	}
	return selectPreparedSubscriptionCandidates(candidates, sub, local)
}

func selectRSSSubscriptionCandidates(items []rssItem, sub *model.Subscription, filter *regexp.Regexp, seenSet map[string]struct{}, local LocalAvailability) []siteSearchCandidate {
	if seenSet == nil {
		seenSet = map[string]struct{}{}
	}
	candidates := make([]siteSearchCandidate, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		if filter != nil && !filter.MatchString(title) {
			continue
		}
		if !matchesSubscriptionRules(sub, title) {
			continue
		}
		download := strings.TrimSpace(item.Enclosure.URL)
		if download == "" {
			download = strings.TrimSpace(item.Link)
		}
		if download == "" {
			continue
		}
		guid := stableRSSItemGUID(title, item.GUID, item.Link, item.Enclosure.URL)
		if _, ok := seenSet[guid]; ok {
			continue
		}
		searchItem := SearchResult{Title: title, DownloadURL: download}
		season, episode := ParseEpisode(title)
		candidates = append(candidates, siteSearchCandidate{
			Item:     searchItem,
			Download: download,
			GUID:     guid,
			Season:   season,
			Episode:  episode,
			Pack:     isSeriesPackTitle(title),
			Score:    subscriptionCandidateScore(sub, searchItem),
		})
	}
	return selectPreparedSubscriptionCandidates(candidates, sub, local)
}

func selectPreparedSubscriptionCandidates(candidates []siteSearchCandidate, sub *model.Subscription, local LocalAvailability) []siteSearchCandidate {
	if len(candidates) > 1 {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Score != candidates[j].Score {
				return candidates[i].Score > candidates[j].Score
			}
			if candidates[i].Item.Seeders != candidates[j].Item.Seeders {
				return candidates[i].Item.Seeders > candidates[j].Item.Seeders
			}
			return candidates[i].Item.Size > candidates[j].Item.Size
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	mediaType := normalizeMediaType(sub.MediaType, sub.Name+" "+sub.Filter, "")
	if !isSubscriptionSeriesType(mediaType) {
		// 非洗版订阅成功下载一次即满足，媒体库/下载中已存在则不再重复下载。
		if (sub == nil || !sub.WashEnabled) && local.LocalMediaCount > 0 {
			return nil
		}
		return candidates[:1]
	}

	if local.HasSeriesPack {
		return nil
	}
	if local.LocalMediaCount > 0 {
		if local.TotalEpisodes > 0 && len(local.MissingEpisodes) == 0 {
			return nil
		}
		missingSet := missingEpisodeSet(local)
		onlyMissing := make([]siteSearchCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.Episode <= 0 {
				continue
			}
			season := candidate.Season
			if season <= 0 {
				season = 1
			}
			if _, exists := local.ExistingEpisodeKeys[episodeKey(season, candidate.Episode)]; exists {
				continue
			}
			if local.TotalEpisodes > 0 {
				if _, missing := missingSet[candidate.Episode]; !missing {
					continue
				}
			}
			onlyMissing = append(onlyMissing, candidate)
		}
		return sortedEpisodeCandidates(onlyMissing)
	}

	for _, candidate := range candidates {
		if candidate.Pack {
			return []siteSearchCandidate{candidate}
		}
	}

	selected := sortedEpisodeCandidates(candidates)
	if len(selected) == 0 {
		return candidates[:1]
	}
	return selected
}

func stableRSSItemGUID(title, guid, link, enclosureURL string) string {
	parts := []string{"rss", strings.ToLower(strings.TrimSpace(title))}
	for _, raw := range []string{guid, enclosureURL, link} {
		if key := stableDownloadURLKey(raw); key != "" {
			parts = append(parts, key)
			return strings.Join(parts, "|")
		}
		if raw = strings.TrimSpace(raw); raw != "" {
			parts = append(parts, strings.ToLower(raw))
			return strings.Join(parts, "|")
		}
	}
	return strings.Join(parts, "|")
}

func stableSiteSearchGUID(item SearchResult, download string) string {
	parts := []string{
		"site",
		strings.ToLower(strings.TrimSpace(firstNonEmpty(item.SiteID, item.SiteName))),
		strings.ToLower(strings.TrimSpace(item.Category)),
		strings.ToLower(strings.TrimSpace(item.Title)),
		fmt.Sprintf("%d", item.Size),
	}
	if key := stableDownloadURLKey(download); key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, "|")
}

func stableDownloadURLKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	if strings.EqualFold(u.Scheme, "magnet") {
		xt := strings.ToLower(strings.TrimSpace(u.Query().Get("xt")))
		if xt != "" {
			return "magnet:" + xt
		}
		return strings.ToLower(raw)
	}
	if u.Host == "" {
		return strings.ToLower(raw)
	}
	q := u.Query()
	kept := make([]string, 0, 4)
	for _, key := range []string{"id", "tid", "torrent", "torrent_id", "torrentid", "hash", "info_hash"} {
		if value := strings.TrimSpace(q.Get(key)); value != "" {
			kept = append(kept, key+"="+strings.ToLower(value))
		}
	}
	base := strings.ToLower(strings.TrimRight(u.Host, "/") + "/" + strings.TrimLeft(u.Path, "/"))
	if len(kept) > 0 {
		return base + "?" + strings.Join(kept, "&")
	}
	return base
}

// defaultExcludeWords 是默认过滤的「垃圾版本」排除清单，对所有订阅生效，
// 与用户自定义排除词合并。拉丁词在 containsAnyExcludeToken 里按词边界匹配以避免子串误伤。
const defaultExcludeWords = "cam,ts,tc,telesync,telecine,hdcam,hdts,枪版,抢先,抢鲜,预告,trailer,sample"

func matchesSubscriptionRules(sub *model.Subscription, title string) bool {
	titleFold := strings.ToLower(title)
	if containsAnyExcludeToken(titleFold, defaultExcludeWords) {
		return false
	}
	if sub == nil {
		return true
	}
	if sub.ExcludeWords != "" && containsAnyExcludeToken(titleFold, sub.ExcludeWords) {
		return false
	}
	if sub.ReleaseGroups != "" && !containsAnyToken(titleFold, sub.ReleaseGroups) {
		return false
	}
	if sub.Resolution != "" && sub.Resolution != "best" && !titleMatchesResolution(titleFold, sub.Resolution) {
		return false
	}
	if sub.Quality != "" && sub.Quality != "best" && !titleMatchesQuality(titleFold, sub.Quality) {
		return false
	}
	if sub.Effects != "" && !containsAnyEffect(titleFold, sub.Effects) {
		return false
	}
	return true
}

func subscriptionCandidateScore(sub *model.Subscription, item SearchResult) int {
	title := strings.ToLower(item.Title)
	score := item.Seeders
	if sub == nil || !sub.WashEnabled {
		if item.Free {
			score += 25
		}
		return score
	}
	resolutionScore := detectResolutionScore(title)
	qualityScore := detectQualityScore(title)
	effectScore := detectEffectScore(title)

	priority := "balanced"
	if sub != nil && strings.TrimSpace(sub.WashPriority) != "" {
		priority = strings.ToLower(strings.TrimSpace(sub.WashPriority))
	}
	switch priority {
	case "resolution":
		score += resolutionScore*1000 + qualityScore*100 + effectScore*50
	case "quality":
		score += qualityScore*1000 + resolutionScore*200 + effectScore*50
	case "effects":
		score += effectScore*1000 + resolutionScore*200 + qualityScore*100
	case "seeders":
		score += qualityScore*3 + resolutionScore*2 + effectScore
	default:
		score += resolutionScore*500 + qualityScore*300 + effectScore*150
	}
	if item.Free {
		score += 25
	}
	return score
}

func containsAnyToken(titleFold, csv string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(csv), func(r rune) bool {
		return r == ',' || r == '/' || r == '|' || r == ';' || r == '，'
	}) {
		token = strings.TrimSpace(token)
		if token != "" && strings.Contains(titleFold, token) {
			return true
		}
	}
	return false
}

// containsAnyExcludeToken 用于排除词匹配：纯 ASCII 字母数字的词按词边界匹配（避免 "ts"
// 误伤 "tsukihime"、"cam" 误伤 "camp" 之类的子串误判），含 CJK/符号的词仍按子串匹配。
func containsAnyExcludeToken(titleFold, csv string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(csv), func(r rune) bool {
		return r == ',' || r == '/' || r == '|' || r == ';' || r == '，'
	}) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if isASCIIWordToken(token) {
			if matchesWordBoundary(titleFold, token) {
				return true
			}
			continue
		}
		if strings.Contains(titleFold, token) {
			return true
		}
	}
	return false
}

func isASCIIWordToken(token string) bool {
	for _, r := range token {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return token != ""
}

// matchesWordBoundary 判断 token 是否作为独立词出现在 title 中，词边界为「非字母数字」。
func matchesWordBoundary(titleFold, token string) bool {
	isWordRune := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	from := 0
	for {
		idx := strings.Index(titleFold[from:], token)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(token)
		leftOK := start == 0 || !isWordRune(rune(titleFold[start-1]))
		rightOK := end >= len(titleFold) || !isWordRune(rune(titleFold[end]))
		if leftOK && rightOK {
			return true
		}
		from = start + 1
		if from >= len(titleFold) {
			return false
		}
	}
}

func containsAnyEffect(titleFold, csv string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(csv), func(r rune) bool {
		return r == ',' || r == '/' || r == '|' || r == ';' || r == '，'
	}) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		switch token {
		case "dolby-vision", "dolby vision", "dv":
			if strings.Contains(titleFold, "dolby vision") || strings.Contains(titleFold, "dovi") || regexp.MustCompile(`\bdv\b`).MatchString(titleFold) {
				return true
			}
		default:
			if strings.Contains(titleFold, token) {
				return true
			}
		}
	}
	return false
}

func titleMatchesResolution(titleFold, resolution string) bool {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "2160p", "4k", "uhd":
		return strings.Contains(titleFold, "2160p") || strings.Contains(titleFold, "4k") || strings.Contains(titleFold, "uhd")
	case "1080p":
		return strings.Contains(titleFold, "1080p") || strings.Contains(titleFold, "fhd")
	case "720p":
		return strings.Contains(titleFold, "720p")
	default:
		return strings.Contains(titleFold, strings.ToLower(strings.TrimSpace(resolution)))
	}
}

func titleMatchesQuality(titleFold, quality string) bool {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "webdl", "web-dl":
		return strings.Contains(titleFold, "web-dl") || strings.Contains(titleFold, "webdl")
	case "bluray", "blu-ray":
		return strings.Contains(titleFold, "bluray") || strings.Contains(titleFold, "blu-ray") || strings.Contains(titleFold, "bdrip")
	case "remux":
		return strings.Contains(titleFold, "remux")
	case "hdtv":
		return strings.Contains(titleFold, "hdtv")
	default:
		return strings.Contains(titleFold, strings.ToLower(strings.TrimSpace(quality)))
	}
}

func detectResolutionScore(titleFold string) int {
	switch {
	case titleMatchesResolution(titleFold, "2160p"):
		return 4
	case titleMatchesResolution(titleFold, "1080p"):
		return 3
	case titleMatchesResolution(titleFold, "720p"):
		return 2
	default:
		return 1
	}
}

func detectQualityScore(titleFold string) int {
	switch {
	case titleMatchesQuality(titleFold, "remux"):
		return 5
	case titleMatchesQuality(titleFold, "bluray"):
		return 4
	case titleMatchesQuality(titleFold, "web-dl"):
		return 3
	case titleMatchesQuality(titleFold, "hdtv"):
		return 2
	default:
		return 1
	}
}

func detectEffectScore(titleFold string) int {
	score := 0
	if containsAnyEffect(titleFold, "dolby-vision") {
		score += 4
	}
	if strings.Contains(titleFold, "hdr10+") {
		score += 3
	} else if strings.Contains(titleFold, "hdr") {
		score += 2
	}
	if strings.Contains(titleFold, "atmos") {
		score += 2
	}
	return score
}

func isSubscriptionSeriesType(mediaType string) bool {
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		return true
	default:
		return false
	}
}

func isSeriesPackTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	if seriesPackRE.MatchString(title) {
		return true
	}
	_, episode := ParseEpisode(title)
	return episode == 0 && seasonOnlyRE.MatchString(title)
}
