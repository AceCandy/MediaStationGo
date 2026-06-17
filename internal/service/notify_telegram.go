// Package service — Telegram 通知 Provider。
package service

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
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
		if photoURL != "" && len(text) <= 1024 {
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
	var sb strings.Builder
	title := strings.TrimSpace(event.Title)
	if strings.HasPrefix(title, "MediaStationGo ") {
		sb.WriteString("<b>MediaStationGo</b>\n")
		sb.WriteString(fmt.Sprintf("<b>%s</b>", escapeHTML(strings.TrimSpace(strings.TrimPrefix(title, "MediaStationGo ")))))
	} else if title != "" {
		sb.WriteString(fmt.Sprintf("<b>%s</b>", escapeHTML(title)))
	} else {
		sb.WriteString("<b>MediaStationGo</b>")
	}

	message := strings.TrimSpace(event.Message)
	if message != "" {
		sb.WriteString("\n\n")
		sb.WriteString(formatTelegramBody(message))
	}

	fields := telegramDisplayData(event.Data)
	if len(fields) > 0 {
		if message != "" {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		for _, field := range fields {
			sb.WriteString(formatTelegramField(field.key, field.value))
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
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
		if key, value, ok := splitTelegramField(line); ok {
			out = append(out, formatTelegramField(key, value))
			continue
		}
		out = append(out, escapeHTML(line))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
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
	return fmt.Sprintf("<b>%s</b>: %s", escapeHTML(key), escapedValue)
}

func telegramCodeField(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(key, "hash") ||
		strings.Contains(key, "路径") ||
		strings.Contains(key, "path") ||
		strings.Contains(key, "id")
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
	case "photo_url", "poster_url", "poster", "image_url", "backdrop_url":
		return true
	default:
		return false
	}
}

func telegramFieldLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "title", "name":
		return "标题"
	case "save_path":
		return "保存路径"
	case "hash":
		return "Hash"
	case "media_type":
		return "媒体类型"
	case "media_category":
		return "分类"
	case "subscription":
		return "订阅"
	case "queued":
		return "新增资源"
	default:
		return strings.TrimSpace(key)
	}
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
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return ""
}

func isTelegramRemotePhotoURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
