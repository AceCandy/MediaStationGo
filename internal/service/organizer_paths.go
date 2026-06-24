package service

import (
	"path/filepath"
	"strings"
)

// sanitizeFilename removes characters not safe for filesystem names.
func sanitizeFilename(s string) string {
	r := strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", "", "?", "",
		"\"", "", "<", "", ">", "", "|", "",
	)
	return strings.TrimSpace(r.Replace(s))
}

func (o *OrganizerService) organizeRoot(libraryPath, mediaType, category string) string {
	typeDir := o.mediaTypeRootDirForCategory(mediaType, category)
	if typeDir == "" || pathAlreadyEndsWith(libraryPath, typeDir) {
		return libraryPath
	}
	if isGenericMediaRoot(libraryPath) {
		return filepath.Join(libraryPath, typeDir)
	}
	return libraryPath
}

func (o *OrganizerService) mediaTypeRootDirForCategory(mediaType, category string) string {
	if root := o.categoryPhysicalRootDir(category); root != "" {
		return root
	}
	return mediaTypeRootDir(mediaType)
}

func (o *OrganizerService) categoryPhysicalRootDir(category string) string {
	key := normalizeOrganizeCategoryKey(category)
	if key == "" {
		return ""
	}
	categories := o.categoryMap()
	match := func(values ...string) bool {
		for _, value := range values {
			if key == normalizeOrganizeCategoryKey(value) {
				return true
			}
		}
		return false
	}
	switch {
	case match(
		categoryName(categories, "cn_anime", "国漫"),
		categoryName(categories, "jp_anime", "日番"),
		categoryName(categories, "euus_anime", "欧美动漫"),
		categoryName(categories, "children", "儿童"),
		"国漫", "国产动漫", "日番", "番剧", "日漫", "日本动漫", "日本动画", "欧美动漫", "欧美动画", "西方动画", "儿童", "少儿",
	):
		return "动漫"
	case match(
		categoryName(categories, "domestic_tv", "国产剧"),
		categoryName(categories, "euus_tv", "欧美剧"),
		categoryName(categories, "jk_tv", "日韩剧"),
		categoryName(categories, "variety", "综艺"),
		categoryName(categories, "documentary", "纪录片"),
		categoryName(categories, "uncategorized_tv", "未分类"),
		"国产剧", "欧美剧", "日韩剧", "日剧", "韩剧", "综艺", "真人秀", "纪录片", "纪录", "未分类",
	):
		return "电视剧"
	case match(
		categoryName(categories, "animation_movie", "动画电影"),
		categoryName(categories, "chinese_movie", "华语电影"),
		categoryName(categories, "foreign_movie", "外语电影"),
		categoryName(categories, "euus_movie", "欧美电影"),
		categoryName(categories, "jk_movie", "日韩电影"),
		"动画电影", "动漫电影", "华语电影", "国产电影", "外语电影", "欧美电影", "日韩电影",
	):
		return "电影"
	case match(categoryName(categories, "adult", "成人"), categoryName(categories, "adult_9kg", "9KG"), categoryName(categories, "adult_jav", "番号"), "成人", "9kg", "番号", "jav"):
		return "成人"
	default:
		return ""
	}
}

func categoryRoot(root, category string) string {
	if strings.TrimSpace(category) == "" || pathAlreadyEndsWith(root, category) {
		return root
	}
	return filepath.Join(root, category)
}

func pathWithin(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if strings.EqualFold(cleanPath, cleanRoot) {
		return true
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func mediaTypeRootDir(mediaType string) string {
	switch normalizeMediaType(mediaType, "", "") {
	case "movie":
		return "电影"
	case "anime":
		return "动漫"
	case "tv", "variety":
		return "电视剧"
	case "adult":
		return "成人"
	default:
		return ""
	}
}

func isGenericMediaRoot(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(filepath.Clean(path))))
	switch base {
	case "media", "medias", "library", "libraries", "organized", "整理":
		return true
	default:
		return false
	}
}

// organizeStagingFolderNames lists manual-organize style staging folder names.
// These workspaces should not remain as first-level category folders after an
// organize operation; redirectOrganizeStagingRoot lifts them to the media root.
func organizeStagingFolderNames() map[string]struct{} {
	return map[string]struct{}{
		"手动整理": {}, "手动整理入库": {}, "待整理": {}, "待分类": {},
		"manual": {}, "manual_organize": {}, "manualorganize": {}, "staging": {}, "inbox": {},
	}
}

func isOrganizeStagingDir(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(filepath.Clean(path))))
	if base == "" {
		return false
	}
	_, ok := organizeStagingFolderNames()[base]
	return ok
}

func redirectOrganizeStagingRoot(root string) string {
	cleaned := filepath.Clean(strings.TrimSpace(root))
	if cleaned == "" || cleaned == "." {
		return root
	}
	for isOrganizeStagingDir(cleaned) {
		parent := filepath.Dir(cleaned)
		if parent == cleaned || parent == "." || parent == string(filepath.Separator) {
			break
		}
		cleaned = parent
	}
	return cleaned
}

func pathAlreadyEndsWith(path, suffix string) bool {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(path)))
	return strings.EqualFold(base, suffix)
}

func isSeriesLibraryType(mediaType string) bool {
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		return true
	default:
		return false
	}
}
