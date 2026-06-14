package service

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type organizeTargetInput struct {
	Root      string
	MediaType string
	Category  string
	Title     string
	Ext       string
	Year      int
	Season    int
	Episode   int
	Series    bool
}

type organizeTargetPath struct {
	Dir        string
	Path       string
	EpisodeTag string
}

func (o *OrganizerService) buildOrganizeTargetPath(ctx context.Context, in organizeTargetInput) (organizeTargetPath, error) {
	root := filepath.Clean(strings.TrimSpace(in.Root))
	if root == "" || root == "." {
		return organizeTargetPath{}, fmt.Errorf("organize target root required")
	}
	title := sanitizeFilename(strings.TrimSpace(in.Title))
	if title == "" {
		title = "Unknown"
	}
	ext := strings.TrimSpace(in.Ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	episodeTag := ""
	if in.Series {
		season := in.Season
		if season <= 0 {
			season = 1
		}
		episode := in.Episode
		if episode <= 0 {
			episode = 1
		}
		episodeTag = fmt.Sprintf("S%02dE%02d", season, episode)
	}

	template := strings.TrimSpace(o.organizeNamingFormat(ctx, in.MediaType, in.Series))
	var rel string
	if template == "" {
		rel = defaultOrganizeRelativePath(title, ext, in.Year, in.Season, in.Episode, in.Series)
	} else {
		rel = renderOrganizeNamingTemplate(template, organizeNamingData{
			Title:      title,
			Year:       in.Year,
			Season:     in.Season,
			Episode:    in.Episode,
			Ext:        strings.TrimPrefix(ext, "."),
			FileExt:    ext,
			Category:   sanitizeFilename(in.Category),
			MediaType:  normalizeOrganizeMediaType(in.MediaType),
			EpisodeTag: episodeTag,
		})
		rel = cleanOrganizeRelativePath(rel)
		if rel == "" {
			rel = defaultOrganizeRelativePath(title, ext, in.Year, in.Season, in.Episode, in.Series)
		}
		if ext != "" && !strings.EqualFold(filepath.Ext(rel), ext) {
			rel += ext
		}
	}
	dst := filepath.Join(root, rel)
	return organizeTargetPath{
		Dir:        filepath.Dir(dst),
		Path:       dst,
		EpisodeTag: episodeTag,
	}, nil
}

func (o *OrganizerService) organizeNamingFormat(ctx context.Context, mediaType string, series bool) string {
	if o == nil || o.repo == nil || o.repo.Setting == nil {
		return ""
	}
	key := "organize.movie_format"
	if series {
		if normalizeOrganizeMediaType(mediaType) == "anime" {
			key = "organize.anime_format"
		} else {
			key = "organize.tv_format"
		}
	}
	value, err := o.repo.Setting.Get(ctx, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func defaultOrganizeRelativePath(title, ext string, year, season, episode int, series bool) string {
	if series {
		if season <= 0 {
			season = 1
		}
		if episode <= 0 {
			episode = 1
		}
		episodeTag := fmt.Sprintf("S%02dE%02d", season, episode)
		return filepath.Join(title, fmt.Sprintf("Season %02d", season), fmt.Sprintf("%s - %s%s", title, episodeTag, ext))
	}
	folder := title
	if year > 0 {
		folder = fmt.Sprintf("%s (%d)", title, year)
	}
	return filepath.Join(folder, folder+ext)
}

type organizeNamingData struct {
	Title      string
	Year       int
	Season     int
	Episode    int
	Ext        string
	FileExt    string
	Category   string
	MediaType  string
	EpisodeTag string
}

var organizeTemplateTokenRE = regexp.MustCompile(`\{([A-Za-z_]+)(?::([^}]+))?\}`)
var organizeTemplateMustacheRE = regexp.MustCompile(`\{\{\s*([A-Za-z_]+)(?::([^}]+))?\s*\}\}`)
var organizeTemplateIfRE = regexp.MustCompile(`(?s)\{%\s*if\s+([A-Za-z_]+)\s*%\}(.*?)\{%\s*endif\s*%\}`)

func renderOrganizeNamingTemplate(template string, data organizeNamingData) string {
	template = renderOrganizeConditionals(template, data)
	template = organizeTemplateMustacheRE.ReplaceAllStringFunc(template, func(token string) string {
		matches := organizeTemplateMustacheRE.FindStringSubmatch(token)
		if len(matches) == 0 {
			return token
		}
		format := ""
		if len(matches) > 2 {
			format = matches[2]
		}
		return organizeTemplateValue(matches[1], format, data, token)
	})
	return organizeTemplateTokenRE.ReplaceAllStringFunc(template, func(token string) string {
		matches := organizeTemplateTokenRE.FindStringSubmatch(token)
		if len(matches) == 0 {
			return token
		}
		format := ""
		if len(matches) > 2 {
			format = matches[2]
		}
		return organizeTemplateValue(matches[1], format, data, token)
	})
}

func renderOrganizeConditionals(template string, data organizeNamingData) string {
	for {
		next := organizeTemplateIfRE.ReplaceAllStringFunc(template, func(token string) string {
			matches := organizeTemplateIfRE.FindStringSubmatch(token)
			if len(matches) < 3 {
				return token
			}
			if organizeTemplateTruthy(matches[1], data) {
				return matches[2]
			}
			return ""
		})
		if next == template {
			return next
		}
		template = next
	}
}

func organizeTemplateTruthy(name string, data organizeNamingData) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "title", "name":
		return data.Title != ""
	case "year":
		return data.Year > 0
	case "season":
		return data.Season > 0
	case "episode", "ep":
		return data.Episode > 0
	case "ext", "extension":
		return data.Ext != ""
	case "fileext", "file_ext":
		return data.FileExt != ""
	case "category":
		return data.Category != ""
	case "type", "media_type":
		return data.MediaType != ""
	case "episode_tag", "episodetag", "season_episode":
		return data.EpisodeTag != ""
	case "part", "videoformat", "video_format":
		return false
	default:
		return false
	}
}

func organizeTemplateValue(name, format string, data organizeNamingData, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "title", "name":
		return data.Title
	case "year":
		if data.Year <= 0 {
			return ""
		}
		return strconv.Itoa(data.Year)
	case "season":
		return formatOrganizeNumber(data.Season, format, 1)
	case "episode", "ep":
		return formatOrganizeNumber(data.Episode, format, 1)
	case "ext", "extension":
		return data.Ext
	case "fileext", "file_ext":
		return data.FileExt
	case "category":
		return data.Category
	case "type", "media_type":
		return data.MediaType
	case "episode_tag", "episodetag", "season_episode":
		return data.EpisodeTag
	case "part", "videoformat", "video_format":
		return ""
	default:
		return fallback
	}
}

func formatOrganizeNumber(value int, format string, fallback int) string {
	if value <= 0 {
		value = fallback
	}
	format = strings.TrimSpace(strings.TrimSuffix(format, "d"))
	if strings.HasPrefix(format, "0") {
		width, err := strconv.Atoi(strings.TrimPrefix(format, "0"))
		if err == nil && width > 0 {
			return fmt.Sprintf("%0*d", width, value)
		}
	}
	return strconv.Itoa(value)
}

func cleanOrganizeRelativePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeFilename(strings.TrimSpace(part))
		part = strings.Trim(part, ". ")
		if part == "" || part == "." || part == ".." {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return ""
	}
	return filepath.Join(out...)
}
