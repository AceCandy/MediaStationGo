package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type tmdbAlternativeTitle struct {
	Country string `json:"iso_3166_1"`
	Title   string `json:"title"`
}

type tmdbTranslation struct {
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

func applyTMDbChineseTitle(match *Match, alternatives []tmdbAlternativeTitle, translations []tmdbTranslation) {
	if match == nil {
		return
	}
	aliases := make([]string, 0, 2+len(alternatives)+len(translations))
	aliases = append(aliases, match.Title, match.OriginalName)
	for _, alternative := range alternatives {
		aliases = append(aliases, alternative.Title)
	}
	for _, translation := range translations {
		aliases = append(aliases, firstNonEmpty(translation.Data.Title, translation.Data.Name))
	}
	match.Aliases = appendMetadataAliases(match.Aliases, aliases...)

	if !metadataTitleNeedsChineseLocalization(match) {
		return
	}
	localized := preferredTMDbChineseTitle(alternatives, translations)
	if localized == "" {
		return
	}
	if strings.TrimSpace(match.OriginalName) == "" {
		match.OriginalName = strings.TrimSpace(match.Title)
	}
	match.Title = localized
}

func preferredTMDbChineseTitle(alternatives []tmdbAlternativeTitle, translations []tmdbTranslation) string {
	for _, country := range []string{"CN", "SG", "HK", "TW"} {
		for _, alternative := range alternatives {
			if strings.EqualFold(strings.TrimSpace(alternative.Country), country) && chineseTitleCandidate(alternative.Title) {
				return strings.TrimSpace(alternative.Title)
			}
		}
		for _, translation := range translations {
			if !strings.EqualFold(strings.TrimSpace(translation.Language), "zh") ||
				!strings.EqualFold(strings.TrimSpace(translation.Country), country) {
				continue
			}
			if title := strings.TrimSpace(firstNonEmpty(translation.Data.Title, translation.Data.Name)); chineseTitleCandidate(title) {
				return title
			}
		}
	}
	return ""
}

func preferredTMDbEntityTitle(current string, translations []tmdbTranslation, kind string, number int) string {
	current = strings.TrimSpace(current)
	generated := tmdbEntityTitleIsGenerated(current, kind)
	if current != "" && !generated && !metadataTitleNeedsChineseLocalization(&Match{Title: current}) {
		return current
	}
	if translated := preferredTMDbTranslationText(translations, func(translation tmdbTranslation) string {
		return firstNonEmpty(translation.Data.Name, translation.Data.Title)
	}, func(value string) bool {
		return !tmdbEntityTitleIsGenerated(value, kind)
	}); translated != "" {
		return translated
	}
	if current != "" && !generated {
		return current
	}
	if kind == model.MetadataKindSeason {
		return seasonName(number)
	}
	return fmt.Sprintf("第 %d 集", number)
}

func preferredTMDbEntityOverview(current string, translations []tmdbTranslation) string {
	current = strings.TrimSpace(current)
	if current != "" && !metadataTitleNeedsChineseLocalization(&Match{Title: current}) {
		return current
	}
	if translated := preferredTMDbTranslationText(translations, func(translation tmdbTranslation) string {
		return translation.Data.Overview
	}, nil); translated != "" {
		return translated
	}
	return current
}

func preferredTMDbTranslationText(translations []tmdbTranslation, value func(tmdbTranslation) string, accept func(string) bool) string {
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

func tmdbEntityTitleIsGenerated(title, kind string) bool {
	title = strings.TrimSpace(title)
	switch kind {
	case model.MetadataKindSeason:
		return tmdbGeneratedSeasonTitleRE.MatchString(title)
	case model.MetadataKindEpisode:
		return tmdbGeneratedEpisodeTitleRE.MatchString(title)
	default:
		return false
	}
}

func metadataTitleNeedsChineseLocalization(match *Match) bool {
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return true
	}
	if !containsCJK(match.Title) {
		return true
	}
	for _, r := range match.Title {
		if (r >= '\u3040' && r <= '\u30ff') || (r >= '\uac00' && r <= '\ud7af') {
			return true
		}
	}
	return false
}

func chineseTitleCandidate(title string) bool {
	title = strings.TrimSpace(title)
	return title != "" && containsCJK(title)
}
