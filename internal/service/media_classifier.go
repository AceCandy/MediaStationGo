package service

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

var (
	classifierEpisodeRE = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}\b|第\s*\d+\s*[集期]|(?:^|[\s._-])E\d{1,3}(?:[\s._-]|$)`)
	classifierSeasonRE  = regexp.MustCompile(`(?i)\bS\d{1,2}\b|第\s*\d+\s*季`)
	classifierJAVCodeRE = regexp.MustCompile(`(?:^|[\s._\-/\[\]()])[A-Z]{2,6}[-_]?\d{3,5}(?:[\s._\-/\[\]()]|$)`)
	classifierMovieRE   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:movies?|films?)(?:[^a-z0-9]|$)`)
	classifierTVRE      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:tv|series|shows?|dramas?)(?:[^a-z0-9]|$)`)
	classifierAnimeRE   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:anime|bangumi)(?:[^a-z0-9]|$)`)
)

const DownloadSmartClassifySettingKey = "downloads.smart_classify"

type mediaClassifyInput struct {
	MediaType string
	Title     string
	Languages []string
	Countries []string
	Genres    []string
	Category  string
}

func classifyMediaCategory(input mediaClassifyInput, categories map[string]string) string {
	mediaType := normalizeMediaType(input.MediaType, input.Title, input.Category)
	genres := normalizeTokens(input.Genres...)
	countries := normalizeTokens(input.Countries...)
	languages := normalizeTokens(input.Languages...)
	rawTitleText := input.Title + " " + strings.Join(input.Genres, " ")
	categoryText := strings.ToLower(input.Category)
	rawText := rawTitleText + " " + input.Category
	text := strings.ToLower(rawText)
	hasMetadata := len(genres) > 0 || len(countries) > 0 || len(languages) > 0
	sourceHint := sourceCategoryHint(input.Category, mediaType, categories)

	isChineseByMetadata := hasAny(languages, "ZH", "ZH-CN", "ZH-TW", "CN", "BO", "ZA") || hasAny(countries, "CN", "TW", "HK", "MO")
	isChineseByText := containsHan(rawTitleText) || containsAnyText(strings.ToLower(rawTitleText), "华语", "国产", "国剧", "国漫")
	isChineseByCategory := containsAnyText(categoryText, "华语", "国产", "国剧", "大陆剧", "国产电视剧", "国产电影", "国漫", "国产动漫", "国产动画")
	isChinese := isChineseByMetadata || (!hasMetadata && isChineseByText)
	// 动漫的中文译名几乎都是纯汉字(如日本动画「葬送的芙莉莲」),用 containsHan
	// 判中文会把日本动画误判成国漫。动漫只在有元数据或显式中文标记时才算国漫,
	// 否则默认日番(日本动画占绝大多数;未刮削的国漫刮出 origin_country=CN 后仍正确)。
	isChineseAnime := isChineseByMetadata || (!hasMetadata && containsAnyText(text, "华语", "国产", "国漫", "國漫", "国创", "国产动漫", "国产动画"))
	isJapanese := hasAny(languages, "JA", "JP") || hasAny(countries, "JP") || containsJapaneseKana(rawText) || strings.Contains(text, "日番")
	isKorean := hasAny(languages, "KO", "KR") || hasAny(countries, "KR", "KP") || containsKoreanHangul(rawText)
	isEastAsianByCategory := containsAnyText(categoryText, "日韩剧", "日剧", "韩剧", "日韩电影")
	isEastAsian := isJapanese || isKorean || hasAny(countries, "TH", "IN", "SG") || (!hasMetadata && isEastAsianByCategory)
	isWesternByMetadata := hasAny(countries,
		"US", "GB", "UK", "FR", "DE", "CA", "AU", "NZ", "IE", "NL", "SE", "NO", "DK",
		"FI", "ES", "IT", "PT", "AT", "CH", "BE", "RU",
	)
	isWesternByCategory := containsAnyText(categoryText, "欧美剧", "欧美电视剧", "美剧", "英剧", "欧美电影", "外语电影")
	isWestern := isWesternByMetadata || (!hasMetadata && isWesternByCategory)
	hasAnimeText := containsAnyText(text, "动画", "动漫", "番剧", "年番", "国漫", "日番", "bangumi", "anime", "b-global", "ani-one", "crunchyroll")
	hasVarietyText := containsAnyText(text, "综艺", "真人秀", "脱口秀", "晚会", "春晚", "gala", "festival gala", "reality", "talk show")
	hasDocumentaryText := containsAnyText(text, "纪录", "纪录片", "documentary", "docu", "national geographic", "natgeo")
	isAdultText := containsAnyText(text, "adult", "nsfw", "成人", "番号", "jav", "9kg", "uncensored", "无码", "有码") || classifierJAVCodeRE.MatchString(strings.ToUpper(rawText))

	hasGenre := func(values ...string) bool {
		for _, value := range values {
			if hasAny(genres, strings.ToUpper(value)) {
				return true
			}
			if isDigits(value) {
				continue
			}
			if strings.Contains(text, strings.ToLower(value)) {
				return true
			}
		}
		return false
	}

	switch mediaType {
	case "movie":
		if isAdultText {
			return categoryName(categories, "adult", "成人")
		}
		if hasGenre("16", "ANIMATION", "动画", "动漫") || hasAnimeText {
			return categoryName(categories, "animation_movie", "动画电影")
		}
		if !hasMetadata && sourceHint != "" {
			return sourceHint
		}
		if isChinese {
			return categoryName(categories, "chinese_movie", "华语电影")
		}
		return categoryName(categories, "foreign_movie", "外语电影")
	case "anime":
		if !hasMetadata && sourceHint != "" {
			return sourceHint
		}
		if isChineseAnime {
			return categoryName(categories, "cn_anime", "国漫")
		}
		if isWesternByMetadata || (!hasMetadata && containsAnyText(categoryText, "欧美动漫", "欧美动画", "西方动画")) {
			return categoryName(categories, "euus_anime", "欧美动漫")
		}
		return categoryName(categories, "jp_anime", "日番")
	case "variety":
		return categoryName(categories, "variety", "综艺")
	case "tv":
		if isAdultText {
			return categoryName(categories, "adult", "成人")
		}
		if hasGenre("10764", "10767", "REALITY", "TALK", "综艺", "真人秀", "脱口秀") || hasVarietyText {
			return categoryName(categories, "variety", "综艺")
		}
		if hasGenre("99", "DOCUMENTARY", "纪录", "纪录片") || hasDocumentaryText {
			return categoryName(categories, "documentary", "纪录片")
		}
		if hasGenre("10762", "KIDS", "儿童") {
			return categoryName(categories, "children", "儿童")
		}
		if hasGenre("16", "ANIMATION", "动画", "动漫") || hasAnimeText {
			if isChineseAnime {
				return categoryName(categories, "cn_anime", "国漫")
			}
			if isWesternByMetadata || (!hasMetadata && containsAnyText(categoryText, "欧美动漫", "欧美动画", "西方动画")) {
				return categoryName(categories, "euus_anime", "欧美动漫")
			}
			return categoryName(categories, "jp_anime", "日番")
		}
		if !hasMetadata && sourceHint != "" {
			return sourceHint
		}
		if isChinese || (!hasMetadata && isChineseByCategory) {
			return categoryName(categories, "domestic_tv", "国产剧")
		}
		if isEastAsian {
			return categoryName(categories, "jk_tv", "日韩剧")
		}
		if isWestern {
			return categoryName(categories, "euus_tv", "欧美剧")
		}
		return categoryName(categories, "uncategorized_tv", "未分类")
	case "adult":
		return categoryName(categories, "adult", "成人")
	}
	return ""
}

func normalizeMediaType(mediaType, title, category string) string {
	raw := strings.ToLower(strings.TrimSpace(mediaType))
	switch raw {
	case "movie", "film":
		return "movie"
	case "tv", "series", "show", "drama":
		return "tv"
	case "anime", "animation":
		return "anime"
	case "variety":
		return "variety"
	case "adult", "nsfw":
		return "adult"
	}
	switch {
	case containsAnyText(raw, "成人", "番号", "jav", "nsfw"):
		return "adult"
	case containsAnyText(raw, "综艺", "真人秀"):
		return "variety"
	case (containsAnyText(raw, "国漫", "日漫", "日番", "动漫", "动画") || classifierAnimeRE.MatchString(raw)) && !containsAnyText(raw, "动画电影"):
		return "anime"
	case containsAnyText(raw, "电视剧", "国产剧", "欧美剧", "日韩剧", "日剧", "韩剧", "剧集") || classifierTVRE.MatchString(raw):
		return "tv"
	case containsAnyText(raw, "电影") || classifierMovieRE.MatchString(raw):
		return "movie"
	}
	text := strings.ToLower(title + " " + category)
	switch {
	case strings.Contains(text, "adult") || strings.Contains(text, "nsfw") || strings.Contains(text, "成人") || strings.Contains(text, "番号") || strings.Contains(text, "jav") || strings.Contains(text, "9kg") || classifierJAVCodeRE.MatchString(strings.ToUpper(title+" "+category)):
		return "adult"
	case containsAnyText(text, "综艺", "真人秀", "脱口秀", "晚会", "春晚", "gala", "festival gala", "reality", "talk show"):
		return "variety"
	case strings.Contains(text, "电影") || classifierMovieRE.MatchString(text):
		return "movie"
	case classifierAnimeRE.MatchString(text) || strings.Contains(text, "动漫") || strings.Contains(text, "动画"):
		return "anime"
	case strings.Contains(text, "variety") || strings.Contains(text, "综艺") || strings.Contains(text, "真人秀"):
		return "variety"
	case classifierEpisodeRE.MatchString(text) || classifierSeasonRE.MatchString(text) || classifierTVRE.MatchString(text) || strings.Contains(text, "剧集") || strings.Contains(text, "电视剧"):
		return "tv"
	default:
		return "movie"
	}
}

func normalizeTokens(values ...string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '/' || r == '|' || r == ';'
		}) {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part != "" {
				out[part] = struct{}{}
			}
		}
	}
	return out
}

func hasAny(values map[string]struct{}, needles ...string) bool {
	for _, needle := range needles {
		if _, ok := values[strings.ToUpper(needle)]; ok {
			return true
		}
	}
	return false
}

func containsAnyText(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func containsHan(text string) bool {
	for _, r := range text {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func containsJapaneseKana(text string) bool {
	for _, r := range text {
		if (r >= '\u3040' && r <= '\u30ff') || (r >= '\u31f0' && r <= '\u31ff') {
			return true
		}
	}
	return false
}

func containsKoreanHangul(text string) bool {
	for _, r := range text {
		if (r >= '\uac00' && r <= '\ud7af') || (r >= '\u1100' && r <= '\u11ff') || (r >= '\u3130' && r <= '\u318f') {
			return true
		}
	}
	return false
}

func containsLatin(text string) bool {
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func categoryName(categories map[string]string, key, fallback string) string {
	if categories != nil {
		if name := strings.TrimSpace(categories[key]); name != "" {
			return name
		}
	}
	return fallback
}

type sourceCategoryHintDef struct {
	Key       string
	Fallback  string
	MediaType string
}

var sourceCategoryHints = []sourceCategoryHintDef{
	{Key: "animation_movie", Fallback: "动画电影", MediaType: "movie"},
	{Key: "chinese_movie", Fallback: "华语电影", MediaType: "movie"},
	{Key: "jk_movie", Fallback: "日韩电影", MediaType: "movie"},
	{Key: "euus_movie", Fallback: "欧美电影", MediaType: "movie"},
	{Key: "foreign_movie", Fallback: "外语电影", MediaType: "movie"},
	{Key: "domestic_tv", Fallback: "国产剧", MediaType: "tv"},
	{Key: "euus_tv", Fallback: "欧美剧", MediaType: "tv"},
	{Key: "jk_tv", Fallback: "日韩剧", MediaType: "tv"},
	{Key: "cn_anime", Fallback: "国漫", MediaType: "anime"},
	{Key: "jp_anime", Fallback: "日番", MediaType: "anime"},
	{Key: "euus_anime", Fallback: "欧美动漫", MediaType: "anime"},
	{Key: "variety", Fallback: "综艺", MediaType: "variety"},
	{Key: "documentary", Fallback: "纪录片", MediaType: "tv"},
	{Key: "children", Fallback: "儿童", MediaType: "tv"},
	{Key: "adult", Fallback: "成人", MediaType: "adult"},
	{Key: "adult_9kg", Fallback: "9KG", MediaType: "adult"},
	{Key: "adult_jav", Fallback: "番号", MediaType: "adult"},
}

func sourceCategoryHint(category, mediaType string, categories map[string]string) string {
	tokens := sourceCategoryTokens(category)
	if len(tokens) == 0 {
		return ""
	}
	for _, hint := range sourceCategoryHints {
		if !sourceCategoryCompatible(mediaType, hint.MediaType) {
			continue
		}
		for _, name := range []string{hint.Fallback, categoryName(categories, hint.Key, hint.Fallback)} {
			if _, ok := tokens[strings.ToLower(strings.TrimSpace(name))]; ok {
				return categoryName(categories, hint.Key, hint.Fallback)
			}
		}
	}
	return ""
}

func sourceCategoryTokens(category string) map[string]struct{} {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil
	}
	normalized := strings.NewReplacer("\\", " ", "/", " ", "|", " ", ",", " ", ";", " ").Replace(category)
	out := map[string]struct{}{
		strings.ToLower(category): {},
	}
	for _, field := range strings.Fields(normalized) {
		out[strings.ToLower(strings.TrimSpace(field))] = struct{}{}
	}
	return out
}

func sourceCategoryCompatible(mediaType, categoryMediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	categoryMediaType = strings.ToLower(strings.TrimSpace(categoryMediaType))
	if mediaType == "" || categoryMediaType == "" || mediaType == categoryMediaType {
		return true
	}
	if mediaType == "tv" && (categoryMediaType == "anime" || categoryMediaType == "variety") {
		return true
	}
	if categoryMediaType == "adult" {
		return true
	}
	return false
}

func (o *OrganizerService) categoryMap() map[string]string {
	if o == nil || o.cfg == nil || o.cfg.Organizer.Categories == nil {
		return nil
	}
	return o.cfg.Organizer.Categories
}

func (o *OrganizerService) classifyMedia(ctx context.Context, m *model.Media, mediaType string) string {
	if m == nil {
		return ""
	}
	if m.Languages == "" && m.Countries == "" && m.Genres == "" && o != nil && o.repo != nil && o.repo.Media != nil {
		if fresh, err := o.repo.Media.FindByID(ctx, m.ID); err == nil && fresh != nil {
			m = fresh
		}
	}
	return classifyMediaCategory(mediaClassifyInput{
		MediaType: mediaType,
		Title:     m.Title + " " + m.OriginalName,
		Languages: parseCommaList(m.Languages),
		Countries: parseCommaList(m.Countries),
		Genres:    parseCommaList(m.Genres),
	}, o.categoryMap())
}

func (s *SubscriptionService) classifySubscriptionItem(ctx context.Context, sub *model.Subscription, title, sourceCategory string) (string, string) {
	mediaType := normalizeMediaType(sub.MediaType, title+" "+sub.Name+" "+sub.Filter, sourceCategory)
	category := strings.TrimSpace(sub.MediaCategory)
	if category == "" {
		if match := s.lookupSubscriptionMetadata(ctx, mediaType, title, sub); match != nil {
			category = classifyMediaCategory(mediaClassifyInput{
				MediaType: mediaType,
				Title:     match.Title + " " + match.OriginalName,
				Languages: match.Languages,
				Countries: match.Countries,
				Genres:    match.Genres,
				Category:  sourceCategory,
			}, s.categoryMap())
			if s != nil && s.log != nil && category != "" {
				s.log.Info("subscription metadata classified",
					zap.String("title", title),
					zap.String("matched_title", match.Title),
					zap.String("media_type", mediaType),
					zap.String("media_category", category),
					zap.Int("tmdb_id", match.TMDbID),
					zap.Int("bangumi_id", match.BangumiID),
					zap.String("douban_id", match.DoubanID),
					zap.String("thetvdb_id", match.TheTVDBID))
			}
		}
	}
	if category == "" {
		category = classifyMediaCategory(mediaClassifyInput{
			MediaType: mediaType,
			Title:     title + " " + sub.Name + " " + sub.Filter,
			Category:  sourceCategory,
		}, s.categoryMap())
	}
	return mediaType, category
}

func (s *SubscriptionService) lookupSubscriptionMetadata(ctx context.Context, mediaType, title string, sub *model.Subscription) *Match {
	if s == nil || s.scraper == nil || !s.scraper.AnyEnabled() {
		return nil
	}
	queries := subscriptionMetadataQueries(title, sub)
	if len(queries) == 0 {
		return nil
	}
	for _, libType := range subscriptionMetadataLibraryTypes(mediaType, title) {
		lib := &model.Library{Type: libType, Enabled: true}
		for _, query := range queries {
			cleaned, year := CleanQuery(query)
			if cleaned == "" {
				cleaned = strings.TrimSpace(query)
			}
			for _, candidate := range titleCandidates(cleaned) {
				if candidate == "" {
					continue
				}
				match := s.scraper.lookup(ctx, lib, nil, candidate, year)
				if match == nil || strings.TrimSpace(match.Title) == "" {
					continue
				}
				if !organizeMetadataMatchTrusted(candidate, year, match) {
					continue
				}
				return match
			}
		}
	}
	return nil
}

func subscriptionMetadataQueries(title string, sub *model.Subscription) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	add(title)
	if sub != nil {
		add(sub.Filter)
		add(sub.Name)
	}
	return out
}

func subscriptionMetadataLibraryTypes(mediaType, title string) []string {
	switch normalizeMediaType(mediaType, title, "") {
	case "movie":
		return []string{"movie"}
	case "anime":
		return []string{"anime", "tv"}
	case "tv", "variety":
		return []string{"tv", "anime"}
	default:
		if classifierEpisodeRE.MatchString(title) || classifierSeasonRE.MatchString(title) {
			return []string{"tv", "anime"}
		}
		return []string{"movie", "tv", "anime"}
	}
}

func (s *SubscriptionService) categoryMap() map[string]string {
	if s == nil || s.cfg == nil || s.cfg.Organizer.Categories == nil {
		return nil
	}
	return s.cfg.Organizer.Categories
}

func (s *SubscriptionService) resolveSubscriptionSavePath(ctx context.Context, sub *model.Subscription, mediaType, category string) string {
	if sub == nil {
		return ""
	}
	base := strings.TrimSpace(sub.SavePath)
	if base == "" {
		base = downloadDefaultSaveRoot(ctx, s.repo)
	}
	if base == "" {
		return ""
	}
	if !s.isSmartClassifyEnabled(ctx) || category == "" {
		return base
	}
	return downloadSavePathCategoryRoot(base, sanitizeFilename(category))
}

func (s *SubscriptionService) isSmartClassifyEnabled(ctx context.Context) bool {
	if s != nil && s.repo != nil && s.repo.Setting != nil {
		val, err := s.repo.Setting.Get(ctx, DownloadSmartClassifySettingKey)
		if err == nil && val != "" {
			return parseBoolSetting(val, true)
		}
		val, err = s.repo.Setting.Get(ctx, "organizer.smart_classify")
		if err == nil && parseBoolSetting(val, false) {
			return true
		}
	}
	if s != nil && s.cfg != nil && s.cfg.Organizer.SmartClassify {
		return true
	}
	return true
}

func downloadDefaultSaveRoot(ctx context.Context, repo *repository.Container) string {
	if repo != nil && repo.Setting != nil {
		if base, _ := repo.Setting.Get(ctx, "qbittorrent.savepath"); strings.TrimSpace(base) != "" {
			return strings.TrimSpace(base)
		}
	}
	for _, key := range []string{"MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "MEDIASTATION_DOWNLOAD_DIR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func downloadSmartClassifyEnabled(ctx context.Context, repo *repository.Container, organizer *OrganizerService) bool {
	if repo != nil && repo.Setting != nil {
		val, err := repo.Setting.Get(ctx, DownloadSmartClassifySettingKey)
		if err == nil && val != "" {
			return parseBoolSetting(val, true)
		}
		val, err = repo.Setting.Get(ctx, "organizer.smart_classify")
		if err == nil && parseBoolSetting(val, false) {
			return true
		}
	}
	if organizer != nil && organizer.cfg != nil && organizer.cfg.Organizer.SmartClassify {
		return true
	}
	return true
}

func downloadCategoryMap(organizer *OrganizerService) map[string]string {
	if organizer == nil {
		return nil
	}
	return organizer.categoryMap()
}

func downloadSavePathCategoryRoot(root, category string) string {
	root = strings.TrimSpace(root)
	category = strings.TrimSpace(category)
	if root == "" || category == "" {
		return root
	}
	if isWindowsStyleClientPath(root) {
		cleanRoot := strings.ReplaceAll(root, "/", `\`)
		cleanRoot = strings.TrimRight(cleanRoot, `\`)
		if windowsPathBaseEqual(cleanRoot, category) {
			return cleanRoot
		}
		return cleanRoot + `\` + category
	}
	return categoryRoot(root, category)
}

func isWindowsStyleClientPath(path string) bool {
	path = strings.TrimSpace(path)
	return (len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':') ||
		strings.HasPrefix(path, `\\`)
}

func windowsPathBaseEqual(path, base string) bool {
	path = strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(path), "/", `\`), `\`)
	base = strings.Trim(strings.TrimSpace(base), `\/`)
	if path == "" || base == "" {
		return false
	}
	idx := strings.LastIndex(path, `\`)
	if idx >= 0 {
		path = path[idx+1:]
	}
	return strings.EqualFold(path, base)
}
