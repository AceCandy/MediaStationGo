package service

import (
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func strmLibraryOutputSubdir(lib model.Library) string {
	parts := strmLibraryCategoryParts(lib)
	if len(parts) == 0 {
		return sanitizeFilename(lib.Name)
	}
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if safe := sanitizeFilename(part); safe != "" {
			clean = append(clean, safe)
		}
	}
	if len(clean) == 0 {
		return sanitizeFilename(lib.Name)
	}
	return filepath.Join(clean...)
}

func strmLibraryCategoryParts(lib model.Library) []string {
	if parts := strmCategoryPartsFromPath(strmLibraryPathParts(lib.Path)); len(parts) > 0 {
		return parts
	}
	if parts := strmCategoryPartsFromPath(strmNameParts(lib.Name)); len(parts) > 0 {
		return parts
	}
	if root := mediaTypeRootDir(lib.Type); root != "" {
		return []string{root}
	}
	return nil
}

func strmLibraryPathParts(raw string) []string {
	if info, ok := ParseCloudLibraryMount(raw); ok {
		return strmSlashParts(info.DisplayDir)
	}
	clean := cleanPathForVolumeMapping(raw)
	clean = strings.Trim(pathAfterWindowsDrivePrefix(clean), "/")
	return strmSlashParts(clean)
}

func strmNameParts(name string) []string {
	name = strings.NewReplacer("·", "/", ">", "/", "｜", "/", "|", "/", "\\", "/").Replace(name)
	return strmSlashParts(name)
}

func strmSlashParts(raw string) []string {
	raw = strings.Trim(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/")), "/")
	if raw == "" || raw == "." {
		return nil
	}
	fields := strings.Split(raw, "/")
	parts := make([]string, 0, len(fields))
	for _, part := range fields {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

func strmCategoryPartsFromPath(parts []string) []string {
	for i, part := range parts {
		if root := strmCanonicalRoot(part); root != "" {
			return append([]string{root}, strmSanitizedTail(parts[i+1:])...)
		}
		if root := strmCategoryRoot(part); root != "" {
			return []string{root, part}
		}
	}
	return nil
}

func strmSanitizedTail(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

func strmCanonicalRoot(part string) string {
	key := strings.ToLower(strings.TrimSpace(part))
	switch key {
	case "电影", "movie", "movies", "film", "films":
		return "电影"
	case "电视剧", "剧集", "tv", "tvs", "series", "show", "shows":
		return "电视剧"
	case "动漫", "动画", "anime", "bangumi":
		return "动漫"
	case "成人", "adult", "adults", "jav", "nsfw", "9kg":
		return "成人"
	default:
		return ""
	}
}

func strmCategoryRoot(part string) string {
	key := strings.ToLower(strings.TrimSpace(part))
	switch key {
	case "动画电影", "动漫电影", "华语电影", "国产电影", "外语电影", "欧美电影", "日韩电影":
		return "电影"
	case "国产剧", "欧美剧", "日韩剧", "日剧", "韩剧", "综艺", "真人秀", "纪录片", "纪录", "未分类":
		return "电视剧"
	case "国漫", "国产动漫", "日番", "番剧", "日漫", "日本动漫", "日本动画", "欧美动漫", "欧美动画", "西方动画", "儿童", "少儿":
		return "动漫"
	case "番号":
		return "成人"
	default:
		return ""
	}
}
