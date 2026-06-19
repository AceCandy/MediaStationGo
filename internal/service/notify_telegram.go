// Package service — Telegram 通知 Provider。
package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// TelegramProvider 通过 Telegram Bot API 发送通知。
type TelegramProvider struct{}

// Send 发送 Telegram 消息。
func (p *TelegramProvider) Send(ctx context.Context, cfg map[string]string, event NotifyEvent) error {
	botToken := cfg["bot_token"]
	chatIDs := telegramTargetChatIDs(cfg)
	parseMode := cfg["parse_mode"]
	if parseMode == "" {
		parseMode = "HTML"
	}

	if botToken == "" || len(chatIDs) == 0 {
		return fmt.Errorf("telegram: bot_token and group_chat_id/channel_chat_id are required")
	}

	text := formatTelegramMessage(event, parseMode)
	photoURL := telegramEventPhotoURL(event)

	var firstErr error
	for _, chatID := range chatIDs {
		if photoURL != "" && utf8.RuneCountInString(text) <= 1024 {
			payload := map[string]string{
				"chat_id":    chatID,
				"photo":      photoURL,
				"caption":    text,
				"parse_mode": parseMode,
			}
			if err := telegramPostJSON(ctx, cfg, "sendPhoto", payload, 15*time.Second); err == nil {
				continue
			} else if firstErr == nil {
				firstErr = err
			}
			if photo, _, err := telegramFetchRemotePhoto(ctx, cfg, photoURL, 15*time.Second); err == nil {
				fields := map[string]string{
					"chat_id":    chatID,
					"caption":    text,
					"parse_mode": parseMode,
				}
				if err := telegramPostMultipart(ctx, cfg, "sendPhoto", fields, "photo", "poster.jpg", photo, 20*time.Second); err == nil {
					continue
				} else if firstErr == nil {
					firstErr = err
				}
			} else if firstErr == nil {
				firstErr = err
			}
		}
		payload := map[string]string{
			"chat_id":    chatID,
			"text":       text,
			"parse_mode": parseMode,
		}
		if err := telegramPostJSON(ctx, cfg, "sendMessage", payload, 15*time.Second); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ValidateConfig 验证 Telegram 配置。
func (p *TelegramProvider) ValidateConfig(cfg map[string]string) error {
	if cfg["bot_token"] == "" {
		return fmt.Errorf("telegram: bot_token is required")
	}
	if len(telegramTargetChatIDs(cfg)) == 0 {
		return fmt.Errorf("telegram: group_chat_id or channel_chat_id is required")
	}
	return nil
}

// formatTelegramMessage 格式化消息内容。
func formatTelegramMessage(event NotifyEvent, parseMode string) string {
	text := formatTelegramNotification(event)
	if parseMode == "HTML" || parseMode == "" {
		return text
	}
	result := text
	result = strings.ReplaceAll(result, "<b>", "**")
	result = strings.ReplaceAll(result, "</b>", "**")
	result = strings.ReplaceAll(result, "<code>", "`")
	result = strings.ReplaceAll(result, "</code>", "`")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&amp;", "&")
	return result
}

func formatTelegramNotification(event NotifyEvent) string {
	if text := formatTelegramMediaNotification(event); text != "" {
		return text
	}

	var sb strings.Builder
	if tag := telegramEventTag(event); tag != "" {
		sb.WriteString(tag)
		if telegramShouldShowEventHeading(event) {
			sb.WriteString("\n")
		}
	}
	if telegramShouldShowEventHeading(event) {
		sb.WriteString(telegramEventHeading(event))
	}

	message := strings.TrimSpace(event.Message)
	if message != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(formatTelegramBody(message))
	}

	fields := telegramDisplayData(event.Data)
	if len(fields) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		for _, field := range fields {
			sb.WriteString(formatTelegramField(field.key, field.value))
			sb.WriteString("\n")
		}
	}
	if links := telegramExternalLinks(event.Data); links != "" {
		sb.WriteString("\n")
		sb.WriteString(links)
	}

	return strings.TrimSpace(sb.String())
}

const (
	telegramMediaTemplateHeader    = "🐈‍⬛🐈‍⬛ MediaStationGo 更新啦 🐈‍⬛🐈‍⬛"
	telegramMediaTemplateSeparator = "--------------------------------"
)

var telegramSeasonEpisodePattern = regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}(?:[\-.~_ ]?E?\d{1,3})?`)

func formatTelegramMediaNotification(event NotifyEvent) string {
	tag := telegramMediaTag(event.Data)
	if tag == "" {
		return ""
	}

	title := telegramFirstValue(event.Data, "chinese_title", "title", "name", "media_title")
	if title == "" {
		title = telegramMessageFieldValue(event.Message, "任务", "订阅", "媒体", "资源")
	}
	originalTitle := telegramFirstValue(event.Data, "original_title", "original_name")
	originalLanguage := telegramLanguageName(telegramFirstValue(event.Data, "original_language", "language", "languages"))
	year := telegramFirstValue(event.Data, "year", "release_year")
	category := telegramMediaCategory(event.Data)
	seasonEpisode := telegramSeasonEpisodeValue(event)
	size := telegramSizeValue(event.Data)
	version := telegramVersionValue(event, seasonEpisode)
	rating := telegramFirstValue(event.Data, "rating", "score")
	genres := telegramGenresValue(telegramFirstValue(event.Data, "genres", "genre", "type"))
	overview := telegramFirstValue(event.Data, "overview", "summary", "description")
	links := telegramExternalLinks(event.Data)

	topFields := []string{}
	if title != "" {
		topFields = append(topFields, "📺 中文片名："+escapeHTML(title))
	}
	if originalTitle != "" && !strings.EqualFold(originalTitle, title) {
		topFields = append(topFields, "🧿 原始片名："+escapeHTML(originalTitle))
	}
	if originalLanguage != "" {
		topFields = append(topFields, "🌐 原始语言："+escapeHTML(originalLanguage))
	}
	if year != "" && year != "0" {
		topFields = append(topFields, "📅 发行年份："+escapeHTML(year))
	}

	mediaFields := []string{}
	if category != "" {
		mediaFields = append(mediaFields, "🐈‍⬛ 类别："+escapeHTML(category))
	}
	if seasonEpisode != "" {
		mediaFields = append(mediaFields, "🫧 季集："+escapeHTML(seasonEpisode))
	}
	if size != "" {
		mediaFields = append(mediaFields, "🔎 大小："+escapeHTML(size))
	}
	if version != "" {
		mediaFields = append(mediaFields, "📁 版本："+escapeHTML(version))
	}

	infoFields := []string{}
	if rating != "" && rating != "0" {
		infoFields = append(infoFields, "⭐️ 评分："+escapeHTML(rating))
	}
	if genres != "" {
		infoFields = append(infoFields, "💎 类型："+escapeHTML(genres))
	}
	if overview != "" {
		infoFields = append(infoFields, "🪬 简介：\n"+escapeHTML(overview))
	}

	if len(topFields) == 0 && len(mediaFields) == 0 && len(infoFields) == 0 && links == "" {
		return ""
	}

	lines := []string{telegramMediaTemplateHeader, telegramMediaTemplateSeparator, tag}
	lines = append(lines, topFields...)
	if len(mediaFields) > 0 {
		if len(topFields) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, mediaFields...)
	}
	if len(infoFields) > 0 {
		if len(topFields) > 0 || len(mediaFields) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, infoFields...)
	}
	if links != "" {
		lines = append(lines, "", telegramMediaTemplateSeparator, links)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func telegramShouldShowEventHeading(event NotifyEvent) bool {
	if telegramMediaTag(event.Data) != "" {
		return false
	}
	switch strings.TrimSpace(event.Type) {
	case EventSubscriptionHit, EventDownloadComplete:
		return false
	default:
		return true
	}
}

func telegramEventTag(event NotifyEvent) string {
	if tag := telegramMediaTag(event.Data); tag != "" {
		return tag
	}
	switch strings.TrimSpace(event.Type) {
	case EventSubscriptionHit:
		return "#订阅"
	case EventDownloadComplete:
		return "#下载完成"
	case EventScrapeFailed:
		return "#刮削失败"
	case EventSystemAlert:
		return "#系统提醒"
	case EventLibraryIngest:
		return "#入库"
	default:
		title := strings.TrimSpace(strings.TrimPrefix(event.Title, "MediaStationGo "))
		if title == "" {
			return "#MediaStationGo"
		}
		return "#" + escapeHTML(strings.ReplaceAll(title, " ", ""))
	}
}

func telegramMediaTag(data map[string]interface{}) string {
	for _, key := range []string{"media_category", "category", "media_type"} {
		value := strings.TrimSpace(telegramDataString(data, key))
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		switch {
		case strings.Contains(value, "电影") || lower == "movie":
			return "#电影"
		case strings.Contains(value, "剧") || lower == "tv" || lower == "series" || lower == "show":
			return "#剧集"
		case strings.Contains(value, "动漫") || strings.Contains(value, "动画") || lower == "anime":
			return "#动漫"
		case strings.Contains(value, "综艺") || lower == "variety":
			return "#综艺"
		case strings.Contains(value, "纪录") || lower == "documentary":
			return "#纪录片"
		default:
			return "#" + escapeHTML(strings.ReplaceAll(value, " ", ""))
		}
	}
	return ""
}

func telegramEventHeading(event NotifyEvent) string {
	title := strings.TrimSpace(event.Title)
	title = strings.TrimSpace(strings.TrimPrefix(title, "MediaStationGo "))
	if title == "" {
		title = "MediaStationGo 通知"
	}
	icon := "🔔"
	switch strings.TrimSpace(event.Type) {
	case EventSubscriptionHit:
		icon = "🎯"
	case EventDownloadComplete:
		icon = "✅"
	case EventScrapeFailed:
		icon = "⚠️"
	case EventSystemAlert:
		icon = "🚨"
	case EventLibraryIngest:
		icon = "📚"
	}
	return fmt.Sprintf("%s <b>%s</b>", icon, escapeHTML(title))
}

func formatTelegramBody(message string) string {
	lines := strings.Split(message, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(line, "- ") {
			out = append(out, "- "+escapeHTML(strings.TrimSpace(strings.TrimPrefix(line, "- "))))
			continue
		}
		if key, ok := trimTelegramEmptyField(line); ok {
			out = append(out, fmt.Sprintf("%s <b>%s</b>：", telegramFieldIcon(telegramFieldLabel(key)), escapeHTML(telegramFieldLabel(key))))
			continue
		}
		if key, value, ok := splitTelegramField(line); ok {
			out = append(out, formatTelegramField(key, value))
			continue
		}
		out = append(out, escapeHTML(line))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func trimTelegramEmptyField(line string) (string, bool) {
	line = strings.TrimSpace(line)
	for _, suffix := range []string{"：", ":"} {
		if strings.HasSuffix(line, suffix) {
			key := strings.TrimSpace(strings.TrimSuffix(line, suffix))
			if key != "" && len([]rune(key)) <= 16 {
				return key, true
			}
		}
	}
	return "", false
}

func splitTelegramField(line string) (string, string, bool) {
	idx := strings.Index(line, "：")
	sepLen := len("：")
	if idx < 0 {
		idx = strings.Index(line, ":")
		sepLen = len(":")
	}
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+sepLen:])
	if key == "" || value == "" || len([]rune(key)) > 16 {
		return "", "", false
	}
	return key, value, true
}

func formatTelegramField(key, value string) string {
	key = telegramFieldLabel(key)
	escapedValue := escapeHTML(strings.TrimSpace(value))
	if telegramCodeField(key) {
		escapedValue = "<code>" + escapedValue + "</code>"
	}
	return fmt.Sprintf("%s <b>%s</b>：%s", telegramFieldIcon(key), escapeHTML(key), escapedValue)
}

func telegramCodeField(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "hash") ||
		strings.Contains(key, "路径") ||
		strings.Contains(key, "path") ||
		strings.Contains(key, "id")
}

func telegramFieldIcon(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "中文片名", "标题", "任务", "媒体", "资源":
		return "📺"
	case "原始片名":
		return "🧿"
	case "原始语言", "语言":
		return "🌐"
	case "发行年份", "年份":
		return "📅"
	case "类别", "分类", "媒体类型":
		return "🐈‍⬛"
	case "季集", "集数":
		return "🫧"
	case "大小", "质量", "规格":
		return "🔎"
	case "版本", "保存路径":
		return "📁"
	case "评分":
		return "⭐️"
	case "类型":
		return "💎"
	case "简介":
		return "🪬"
	case "订阅":
		return "🎯"
	case "新增资源":
		return "✨"
	case "Hash":
		return "🧿"
	case "错误":
		return "⚠️"
	default:
		return "•"
	}
}

func telegramFirstValue(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := telegramDataString(data, key); value != "" {
			return value
		}
	}
	return ""
}

func telegramMessageFieldValue(message string, keys ...string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	for _, line := range strings.Split(message, "\n") {
		key, value, ok := splitTelegramField(line)
		if !ok {
			continue
		}
		for _, want := range keys {
			if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(want)) {
				return value
			}
		}
	}
	return ""
}

func telegramMediaCategory(data map[string]interface{}) string {
	if category := telegramFirstValue(data, "media_category", "category"); category != "" {
		return category
	}
	switch strings.ToLower(telegramFirstValue(data, "media_type")) {
	case "movie":
		return "电影"
	case "tv", "series", "show":
		return "剧集"
	case "anime":
		return "动漫"
	case "variety":
		return "综艺"
	case "documentary":
		return "纪录片"
	default:
		return telegramFirstValue(data, "media_type")
	}
}

func telegramLanguageName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '/' || r == '|' || r == '、'
	})
	if len(parts) == 0 {
		parts = []string{raw}
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, "[]"))
		if part == "" {
			continue
		}
		lower := strings.ToLower(strings.ReplaceAll(part, "_", "-"))
		name := part
		switch {
		case strings.HasPrefix(lower, "zh") || lower == "cn" || lower == "cmn":
			name = "中文"
		case lower == "en" || strings.HasPrefix(lower, "en-"):
			name = "英语"
		case lower == "ja" || lower == "jp" || strings.HasPrefix(lower, "ja-"):
			name = "日语"
		case lower == "ko" || lower == "kr" || strings.HasPrefix(lower, "ko-"):
			name = "韩语"
		case lower == "fr" || strings.HasPrefix(lower, "fr-"):
			name = "法语"
		case lower == "de" || strings.HasPrefix(lower, "de-"):
			name = "德语"
		case lower == "es" || strings.HasPrefix(lower, "es-"):
			name = "西班牙语"
		case lower == "it" || strings.HasPrefix(lower, "it-"):
			name = "意大利语"
		case lower == "ru" || strings.HasPrefix(lower, "ru-"):
			name = "俄语"
		case lower == "th" || strings.HasPrefix(lower, "th-"):
			name = "泰语"
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return strings.Join(out, "、")
}

func telegramSeasonEpisodeValue(event NotifyEvent) string {
	if value := telegramFirstValue(event.Data, "season_episode", "episode_tag"); value != "" {
		return strings.ToUpper(value)
	}
	for _, raw := range []string{
		telegramFirstValue(event.Data, "resource_title", "torrent_title", "release_title"),
		telegramFirstValue(event.Data, "title", "name"),
		event.Message,
	} {
		if value := telegramExtractSeasonEpisode(raw); value != "" {
			return value
		}
	}
	season := telegramFirstValue(event.Data, "season")
	episode := telegramFirstValue(event.Data, "episode")
	if season != "" && episode != "" {
		return fmt.Sprintf("S%02dE%02d", telegramEpisodeNumber(season), telegramEpisodeNumber(episode))
	}
	return ""
}

func telegramEpisodeNumber(raw string) int {
	raw = strings.TrimSpace(strings.TrimLeft(strings.ToUpper(raw), "SE"))
	raw = strings.TrimLeft(raw, "0")
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func telegramExtractSeasonEpisode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.ToUpper(telegramSeasonEpisodePattern.FindString(raw))
}

func telegramSizeValue(data map[string]interface{}) string {
	size := telegramFirstValue(data, "size")
	bitrate := telegramFirstValue(data, "bitrate")
	if size != "" && bitrate != "" {
		return size + " / " + bitrate
	}
	if size != "" {
		return size
	}
	return bitrate
}

func telegramVersionValue(event NotifyEvent, seasonEpisode string) string {
	if version := telegramFirstValue(event.Data, "version", "release_group"); version != "" && !strings.EqualFold(version, "best") {
		return version
	}
	return telegramVersionFromResourceTitle(
		telegramFirstValue(event.Data, "resource_title", "torrent_title", "release_title"),
		seasonEpisode,
		telegramFirstValue(event.Data, "year", "release_year"),
	)
}

func telegramVersionFromResourceTitle(raw, seasonEpisode, year string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	tail := ""
	if seasonEpisode != "" {
		upperRaw := strings.ToUpper(raw)
		upperEpisode := strings.ToUpper(seasonEpisode)
		if idx := strings.Index(upperRaw, upperEpisode); idx >= 0 {
			tail = raw[idx+len(seasonEpisode):]
		}
	}
	if tail == "" && year != "" {
		if idx := strings.LastIndex(raw, year); idx >= 0 {
			tail = raw[idx+len(year):]
		}
	}
	tail = strings.Trim(tail, " \t\r\n._-[]()【】")
	if tail == "" {
		return ""
	}
	tail = strings.TrimSuffix(tail, ".torrent")
	tail = strings.TrimSuffix(tail, ".mkv")
	tail = strings.TrimSuffix(tail, ".mp4")
	tail = strings.Join(strings.Fields(tail), ".")
	if len([]rune(tail)) > 72 {
		tail = string([]rune(tail)[:72]) + "..."
	}
	return tail
}

func telegramGenresValue(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "[]"))
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '/' || r == '|' || r == '、'
	})
	if len(parts) <= 1 {
		return raw
	}
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, "、")
}

type telegramDataField struct {
	key   string
	value string
}

func telegramDisplayData(data map[string]interface{}) []telegramDataField {
	if len(data) == 0 {
		return nil
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		if telegramHiddenDataKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]telegramDataField, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(data[key]))
		if value == "" || value == "<nil>" {
			continue
		}
		fields = append(fields, telegramDataField{key: key, value: value})
	}
	return fields
}

func telegramHiddenDataKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "photo_url", "poster_url", "poster", "image_url", "backdrop_url",
		"tmdb_url", "imdb_url", "douban_url", "detail_url", "external_url",
		"resource_title", "torrent_title", "release_title":
		return true
	default:
		return false
	}
}

func telegramFieldLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "title", "name":
		return "标题"
	case "original_title":
		return "原始片名"
	case "original_language":
		return "原始语言"
	case "year", "release_year":
		return "发行年份"
	case "save_path":
		return "保存路径"
	case "hash":
		return "Hash"
	case "media_type":
		return "媒体类型"
	case "media_category":
		return "类别"
	case "season_episode":
		return "季集"
	case "size", "bitrate":
		return "大小"
	case "version", "release_group":
		return "版本"
	case "rating":
		return "评分"
	case "genres":
		return "类型"
	case "overview":
		return "简介"
	case "subscription":
		return "订阅"
	case "queued":
		return "新增资源"
	default:
		return strings.TrimSpace(key)
	}
}

func telegramExternalLinks(data map[string]interface{}) string {
	if len(data) == 0 {
		return ""
	}
	links := []string{}
	for _, item := range []struct {
		key  string
		name string
	}{
		{key: "tmdb_url", name: "TMDB"},
		{key: "imdb_url", name: "IMDB"},
		{key: "douban_url", name: "豆瓣"},
	} {
		value := telegramDataString(data, item.key)
		if isTelegramRemotePhotoURL(value) {
			links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, escapeHTML(value), escapeHTML(item.name)))
		}
	}
	if len(links) == 0 {
		return ""
	}
	return "🔗 外链：" + strings.Join(links, " / ")
}

func telegramEventPhotoURL(event NotifyEvent) string {
	for _, key := range []string{"photo_url", "poster_url", "poster", "image_url", "backdrop_url"} {
		value := telegramDataString(event.Data, key)
		if isTelegramRemotePhotoURL(value) {
			return value
		}
	}
	return ""
}

func telegramDataString(data map[string]interface{}, key string) string {
	if len(data) == 0 {
		return ""
	}
	for k, value := range data {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return telegramValueString(value)
		}
	}
	return ""
}

func telegramValueString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []string:
		return strings.TrimSpace(strings.Join(v, ","))
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := telegramValueString(item); s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, ",")
	case float32:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".")
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".")
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func isTelegramRemotePhotoURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
