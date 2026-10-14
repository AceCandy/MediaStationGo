package model

import (
	"fmt"
	"regexp"
	"strings"
)

// TMDbTranslation 是来源响应中的本地化文字。
type TMDbTranslation struct {
	Country  string `json:"iso_3166_1"`
	Language string `json:"iso_639_1"`
	Data     struct {
		Title    string `json:"title"`
		Name     string `json:"name"`
		Overview string `json:"overview"`
	} `json:"data"`
}

var (
	tmdbGeneratedEpisodeTitleRE = regexp.MustCompile(`(?i)^(?:episode|ep\.?)[\s._-]*0*[0-9]+$|^第\s*[0-9]+\s*(?:集|话|話)$`)
	tmdbGeneratedSeasonTitleRE  = regexp.MustCompile(`(?i)^season[\s._-]*0*[0-9]+$|^第\s*[0-9]+\s*季$|^(?:specials?|特别篇|特別篇)$`)
)

// TMDbEntityTitle 优先选择来源提供的正式中英文季集名。
func TMDbEntityTitle(current string, translations []TMDbTranslation, kind string, number int) string {
	current = strings.TrimSpace(current)
	generated := TMDbEntityTitleIsGenerated(current, kind)
	if current != "" && !generated && HasChineseText(current) {
		return current
	}
	if translated := preferredTMDbTranslationText(translations, func(translation TMDbTranslation) string {
		// 标题只采用中英文翻译，不能把其他语言的通用季集名误当成正式标题。
		language := strings.ToLower(strings.TrimSpace(translation.Language))
		if language != "zh" && language != "en" {
			return ""
		}
		return firstTMDbText(translation.Data.Name, translation.Data.Title)
	}, func(value string) bool {
		return !TMDbEntityTitleIsGenerated(value, kind)
	}); translated != "" {
		return translated
	}
	if current != "" && !generated {
		return current
	}
	if kind == MetadataKindSeason {
		if number == 0 {
			return "特别篇"
		}
		return fmt.Sprintf("第 %d 季", number)
	}
	return fmt.Sprintf("第 %d 集", number)
}

// TMDbEntityOverview 按语言优先级选择来源简介。
func TMDbEntityOverview(current string, translations []TMDbTranslation) string {
	current = strings.TrimSpace(current)
	if current != "" && HasChineseText(current) {
		return current
	}
	if translated := preferredTMDbTranslationText(translations, func(translation TMDbTranslation) string {
		return translation.Data.Overview
	}, nil); translated != "" {
		return translated
	}
	return current
}

func preferredTMDbTranslationText(translations []TMDbTranslation, value func(TMDbTranslation) string, accept func(string) bool) string {
	locales := []struct {
		language string
		country  string
	}{
		{language: "zh", country: "CN"},
		{language: "zh", country: "SG"},
		{language: "zh", country: "HK"},
		{language: "zh", country: "TW"},
		{language: "zh"},
		{language: "en", country: "US"},
		{language: "en"},
		{},
	}
	for _, locale := range locales {
		for _, translation := range translations {
			if locale.language != "" && !strings.EqualFold(strings.TrimSpace(translation.Language), locale.language) {
				continue
			}
			if locale.country != "" && !strings.EqualFold(strings.TrimSpace(translation.Country), locale.country) {
				continue
			}
			candidate := strings.TrimSpace(value(translation))
			if candidate != "" && (accept == nil || accept(candidate)) {
				return candidate
			}
		}
	}
	return ""
}

// TMDbEntityTitleIsGenerated 判断来源标题是否仅是季集编号。
func TMDbEntityTitleIsGenerated(title, kind string) bool {
	title = strings.TrimSpace(title)
	switch kind {
	case MetadataKindSeason:
		return tmdbGeneratedSeasonTitleRE.MatchString(title)
	case MetadataKindEpisode:
		return tmdbGeneratedEpisodeTitleRE.MatchString(title)
	default:
		return false
	}
}

func firstTMDbText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
