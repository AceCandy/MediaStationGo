package model

import (
	"regexp"
	"strings"
)

var generatedEpisodeTitle = regexp.MustCompile(`(?i)^(?:episode|ep\.?)[\s._-]*0*[0-9]+$|^第\s*[0-9]+\s*(?:集|话|話)$`)

// HasChineseText 与来源补全 SQL 使用相同字符范围；日文假名和韩文不视为中文本地化。
func HasChineseText(value string) bool {
	chinese := false
	for _, r := range value {
		if r >= '\u3040' && r <= '\u30ff' || r >= '\uac00' && r <= '\ud7af' {
			return false
		}
		chinese = chinese || r >= '\u4e00' && r <= '\u9fff'
	}
	return chinese
}

// CatalogBasicsComplete 只判断基础来源字段，评分和本地图片下载状态独立维护。
func CatalogBasicsComplete(kind, title, overview, image, releaseDate string) bool {
	if !HasChineseText(title) || strings.TrimSpace(overview) == "" || strings.TrimSpace(image) == "" {
		return false
	}
	return kind != MetadataKindEpisode || !generatedEpisodeTitle.MatchString(strings.TrimSpace(title)) && strings.TrimSpace(releaseDate) != ""
}
