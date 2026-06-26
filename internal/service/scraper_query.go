package service

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// yearPattern extracts a 4-digit year (1900-2099).
var yearPattern = regexp.MustCompile(`(?:^|[^\d])(19\d{2}|20\d{2})(?:[^\d]|$)`)

// noiseTokens are stripped before search.
var noiseTokens = []string{
	// 视频规格
	"1080p", "2160p", "4k", "720p", "480p", "uhd", "ds4k", "fhd",
	"bd", "bdrip", "brrip", "dvd", "dvdrip", "hdtv", "pdtv", "webdl",
	"hdrip", "bluray", "blu-ray", "webrip", "web-dl", "web",
	"x264", "x265", "h264", "h265", "h266", "hevc", "avc", "av1", "vvc", "10bit", "8bit", "hi10p", "hi10",
	"hdr", "hdr10", "sdr", "dts", "ddp", "ddp5", "dd5", "dd2", "eac3", "truehd",
	"dovi", "atmos", "aac", "ac3", "flac", "fps", "hlg", "dv",
	"remux", "extended", "uncut", "remastered", "repack", "proper", "internal",
	"limited", "imax", "directors-cut", "directors_cut",
	"hkfree", "yify", "rarbg", "ettv", "fgt", "tgx", "ctrlhd", "ntb", "flux", "qhstudio",

	// 流媒体平台 / 字幕组 / 国家版本（动漫常见）
	"netflix", "nf", "amzn", "hulu", "disney", "max", "hbo",
	"linetv", "ourtv", "iqiyi", "youku", "bilibili", "qiyi", "krj",
	"atvp", "appletv", "apple-tv", "tx", "txweb",
	"crunchyroll", "funimation", "anidb", "horriblesubs", "subsplease",
	"erai-raws", "judas", "asw", "smcat", "leopard-raws", "ohys-raws", "colortv",
	"mweb", "ubweb", "hhweb", "adweb", "chdweb", "kurosawa", "qhstudio",

	// 中文字幕标记
	"zm", "zw", "ch", "chs", "cht", "cn", "tc", "sc",
	"中字", "繁字", "简中", "繁中", "国语", "粤语", "日语",

	// 季数前缀残留 — ParseEpisode 已抽取过
	"season", "264", "265", "aac2", "aac5",
}

var noiseTokenSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(noiseTokens)+1)
	for _, token := range noiseTokens {
		set[token] = struct{}{}
	}
	set["dl"] = struct{}{}
	return set
}()

var releaseBoundaryTokenSet = map[string]struct{}{
	"1080p": {}, "2160p": {}, "4k": {}, "720p": {}, "480p": {}, "uhd": {}, "fhd": {},
	"bd": {}, "bdrip": {}, "brrip": {}, "dvd": {}, "dvdrip": {}, "hdtv": {}, "pdtv": {},
	"webdl": {}, "hdrip": {}, "bluray": {}, "webrip": {}, "web": {}, "remux": {},
	"x264": {}, "x265": {}, "h264": {}, "h265": {}, "h266": {}, "hevc": {}, "avc": {}, "av1": {}, "vvc": {},
}

var (
	episodeOnlyQueryRE       = regexp.MustCompile(`(?i)^\s*(?:e(?:p(?:isode)?)?\s*\d{1,3}|episode\s*\d{1,3}|第\s*[0-9一二三四五六七八九十百零两]+\s*[集期话話](?:\s*[上下])?)\s*$`)
	episodeTitleQueryRE      = regexp.MustCompile(`^\s*第\s*[0-9一二三四五六七八九十百零两]+\s*[集期话話](?:\s*[上下])?\s*[:：].+`)
	genericEpisodeWordsRE    = regexp.MustCompile(`^\s*第\s*[集期话話]\s*$`)
	episodeReleaseTitleTagRE = regexp.MustCompile(`(?i)(?:^|[\s._-])s\d{1,2}e\d{1,3}(?:[\s._-]|$)`)
)

// bracketedTag matches "[anything]", "(anything)" or "{anything}" segments.
var bracketedTag = regexp.MustCompile(`[\[\(\{][^\]\)\}]*[\]\)\}]`)
var multiWordNoise = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bweb[\s._-]*dl\b`),
	regexp.MustCompile(`(?i)\bblu[\s._-]*ray\b`),
	regexp.MustCompile(`(?i)\bdirectors[\s._-]*cut\b`),
	regexp.MustCompile(`(?i)\berai[\s._-]*raws\b`),
	regexp.MustCompile(`(?i)\bohys[\s._-]*raws\b`),
}

// CleanQuery converts a filename like "Inception.2010.1080p.BluRay.x264.mkv"
// into a TMDb-friendly title plus an optional year hint.
func CleanQuery(raw string) (title string, year int) {
	base := pathBaseSlash(raw)
	if base == "" {
		base = strings.TrimSpace(raw)
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	lower := strings.ToLower(name)

	if m := yearPattern.FindStringSubmatch(lower); len(m) >= 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			year = v
			lower = strings.ReplaceAll(lower, m[1], " ")
		}
	}

	lower = bracketedTag.ReplaceAllString(lower, " ")

	lower = patSEnE.ReplaceAllString(lower, " ")
	lower = patDanglingSE.ReplaceAllString(lower, " ")
	lower = patNxE.ReplaceAllString(lower, " ")
	lower = patEP.ReplaceAllString(lower, " ")
	lower = patCN.ReplaceAllString(lower, " ")
	// 去掉中文季/部标记（如「第二季」「第2部」），避免残留在标题里既污染
	// 搜索查询又导致整理后的目录名重复季信息。
	lower = patSeasonOnly.ReplaceAllString(lower, " ")
	lower = patCNSeason.ReplaceAllString(lower, " ")

	for _, pat := range multiWordNoise {
		lower = pat.ReplaceAllString(lower, " ")
	}
	for _, sep := range []string{".", "_", "-", "[", "]", "(", ")", "×"} {
		lower = strings.ReplaceAll(lower, sep, " ")
	}
	// 拆分后丢掉过短（≤1）且全为 ASCII 数字 / 字母的"碎片"，避免
	// 「2」「0」「v」之类残留干扰 TMDb 搜索。中文字符不算碎片。
	out := make([]string, 0, 8)
	seenReleaseBoundary := false
	for _, w := range strings.Fields(lower) {
		if _, ok := noiseTokenSet[w]; ok {
			if _, boundary := releaseBoundaryTokenSet[w]; boundary {
				seenReleaseBoundary = true
			}
			continue
		}
		if seenReleaseBoundary && isASCIIWord(w) {
			continue
		}
		if len(w) <= 1 {
			r := []rune(w)
			if len(r) == 1 && r[0] < 128 {
				continue
			}
		}
		out = append(out, w)
	}
	title = strings.TrimSpace(strings.Join(out, " "))
	return title, year
}

func isASCIIWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 128 {
			return false
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func scrapeQueryCandidates(m *model.Media, lib *model.Library) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		cleaned, _ := CleanQuery(raw)
		if cleaned == "" {
			cleaned = strings.TrimSpace(raw)
		}
		for _, candidate := range titleCandidates(cleaned) {
			if unsafeAutomaticEpisodeQuery(candidate) {
				continue
			}
			key := strings.ToLower(candidate)
			if _, ok := seen[key]; ok || candidate == "" {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, candidate)
		}
	}
	episodic := mediaIsEpisodic(m, lib)
	if lib != nil && episodic {
		add(seriesFolderTitle(m.Path, lib.Path))
	}
	if lib != nil {
		add(mediaFolderTitle(m.Path, lib.Path))
	}
	add(m.Title)
	add(m.Path)
	if len(out) == 0 {
		base := pathBaseSlash(m.Path)
		out = append(out, strings.TrimSuffix(base, filepath.Ext(base)))
	}
	return out
}

func mediaFolderTitle(mediaPath, libraryRoot string) string {
	dir := parentSlashPath(mediaPath)
	root := comparableLibraryRoot(libraryRoot)
	for depth := 0; depth < 5 && dir != ""; depth++ {
		if root != "" && sameSlashPath(dir, root) {
			return libraryRootTitle(libraryRoot)
		}
		base := pathBaseSlash(dir)
		if base == "" || base == "." {
			return ""
		}
		if isTechnicalMediaFolder(base) || strictSeasonFolderMatched(base) {
			dir = parentSlashPath(dir)
			continue
		}
		if isGenericMediaCategoryFolder(base) {
			return ""
		}
		title, _ := CleanQuery(base)
		if title == "" {
			title = strings.TrimSpace(base)
		}
		return strings.TrimSpace(title)
	}
	return ""
}

func isTechnicalMediaFolder(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	compact := strings.NewReplacer(" ", "", "_", "", ".", "", "-", "").Replace(key)
	switch compact {
	case "bdmv", "stream", "certificate", "videots", "audiots",
		"subs", "subtitles", "subtitle", "sample", "samples",
		"extra", "extras", "featurette", "featurettes":
		return true
	default:
		return numberedTechnicalFolder(compact, "disc") ||
			numberedTechnicalFolder(compact, "disk") ||
			numberedTechnicalFolder(compact, "cd") ||
			numberedTechnicalFolder(compact, "dvd") ||
			numberedTechnicalFolder(compact, "part")
	}
}

func numberedTechnicalFolder(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return false
	}
	for _, r := range value[len(prefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func cleanSlashPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	return strings.TrimRight(value, "/")
}

func comparableLibraryRoot(libraryRoot string) string {
	if info, ok := ParseCloudLibraryMount(libraryRoot); ok {
		if strings.TrimSpace(info.DisplayDir) == "" {
			return "cloud://" + info.Provider
		}
		return "cloud://" + info.Provider + "/" + info.DisplayDir
	}
	return cleanSlashPath(libraryRoot)
}

func sameSlashPath(a, b string) bool {
	return strings.EqualFold(cleanSlashPath(a), cleanSlashPath(b))
}

func parentSlashPath(value string) string {
	value = cleanSlashPath(value)
	if value == "" {
		return ""
	}
	idx := strings.LastIndex(value, "/")
	if idx < 0 {
		return ""
	}
	return strings.TrimRight(value[:idx], "/")
}

func seriesFolderTitle(mediaPath, libraryRoot string) string {
	dir := parentSlashPath(mediaPath)
	if strictSeasonFolderMatched(pathBaseSlash(dir)) {
		dir = parentSlashPath(dir)
	}
	if root := comparableLibraryRoot(libraryRoot); root != "" && sameSlashPath(dir, root) {
		return libraryRootTitle(libraryRoot)
	}
	base := pathBaseSlash(dir)
	if base == "" || base == "." {
		return ""
	}
	if isGenericMediaCategoryFolder(base) || isTechnicalMediaFolder(base) || strictSeasonFolderMatched(base) {
		return ""
	}
	return base
}

func libraryRootTitle(libraryRoot string) string {
	base := ""
	if info, ok := ParseCloudLibraryMount(libraryRoot); ok {
		base = pathBaseSlash(info.DisplayDir)
	} else {
		base = pathBaseSlash(libraryRoot)
	}
	if base == "" || base == "." || isGenericMediaCategoryFolder(base) || isTechnicalMediaFolder(base) || strictSeasonFolderMatched(base) {
		return ""
	}
	return base
}

func isGenericMediaCategoryFolder(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.Trim(key, `\/`)
	switch key {
	case "",
		"电影", "movies", "movie",
		"电视剧", "剧集", "tv", "shows", "series",
		"动漫", "动画", "anime", "bangumi",
		"国产剧", "国剧", "大陆剧", "国产电视剧",
		"欧美剧", "欧美电视剧",
		"日韩剧", "日剧", "韩剧",
		"华语电影", "国产电影", "大陆电影",
		"外语电影", "欧美电影", "日韩电影",
		"动画电影", "动漫电影",
		"国漫", "国产动漫", "日番", "日漫", "日本动漫", "日本动画", "欧美动漫", "欧美动画", "西方动画",
		"综艺", "真人秀",
		"纪录片", "纪录",
		"儿童", "少儿",
		"成人", "番号", "9kg",
		"未分类", "uncategorized":
		return true
	default:
		return false
	}
}

func strictSeasonFolder(name string) int {
	if season, ok := seasonFromDir(name); ok {
		return season
	}
	return 0
}

func strictSeasonFolderMatched(name string) bool {
	_, ok := seasonFromDir(name)
	return ok
}

func titleCandidates(title string) []string {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if title == "" {
		return nil
	}
	out := make([]string, 0, 2)
	if cjk := cjkTitleOnly(title); cjk != "" {
		out = append(out, cjk)
		if cjk != title {
			return out
		}
	}
	out = append(out, title)
	return out
}

func cjkTitleOnly(title string) string {
	parts := make([]string, 0, 4)
	for _, field := range strings.Fields(title) {
		if containsCJK(field) {
			parts = append(parts, field)
		}
	}
	return strings.Join(parts, " ")
}

func containsCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= '\u3400' && r <= '\u4dbf':
			return true
		case r >= '\u4e00' && r <= '\u9fff':
			return true
		case r >= '\uf900' && r <= '\ufaff':
			return true
		}
	}
	return false
}

func mediaIsEpisodic(m *model.Media, lib *model.Library) bool {
	if m != nil && (m.SeasonNum > 0 || m.EpisodeNum > 0) {
		return true
	}
	if m != nil {
		season, episode := ParseEpisode(m.Path)
		if season > 0 || episode > 0 {
			return true
		}
	}
	return librarySupportsSeasons(lib)
}

func librarySupportsSeasons(lib *model.Library) bool {
	if lib == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(lib.Type)) {
	case "tv", "anime", "variety", "show", "shows":
		return true
	default:
		return false
	}
}

func unsafeAutomaticEpisodeQuery(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	if episodeOnlyQueryRE.MatchString(query) || genericEpisodeWordsRE.MatchString(query) {
		return true
	}
	if episodeTitleQueryRE.MatchString(query) {
		return true
	}
	_, episode := ParseEpisode(query)
	if episode > 0 && !looksLikeSeriesReleaseTitle(query) {
		return true
	}
	return false
}

func looksLikeSeriesReleaseTitle(query string) bool {
	cleaned, _ := CleanQuery(query)
	return strings.TrimSpace(cleaned) != "" && episodeReleaseTitleTagRE.MatchString(query)
}
