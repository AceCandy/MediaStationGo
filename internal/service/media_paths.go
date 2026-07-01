package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func inferLibraryKind(name, path, requested string) string {
	requested = normalizeOrganizeMediaType(requested)
	text := strings.ToLower(name + " " + filepath.ToSlash(path))
	switch {
	case containsAnyText(text, "成人", "番号", "jav", "9kg", "adult", "nsfw"):
		return "adult"
	case containsAnyText(text, "综艺", "真人秀", "variety"):
		return "variety"
	case containsAnyText(text, "国漫", "日漫", "日番", "动漫", "动画", "anime", "bangumi") && !containsAnyText(text, "动画电影"):
		return "anime"
	case containsAnyText(text, "电视剧", "剧集", "连续剧", "短剧", "国产剧", "国剧", "大陆剧", "华语剧", "国产电视剧", "大陆电视剧", "华语电视剧", "欧美剧", "欧美电视剧", "美剧", "英剧", "日韩剧", "日韩电视剧", "日剧", "韩剧", "港剧", "台剧", "港台剧", "泰剧", "tv", "series"):
		return "tv"
	case containsAnyText(text, "电影", "movie", "film"):
		return "movie"
	}
	if requested != "" {
		return requested
	}
	return "movie"
}

func resolveAccessibleLibraryPath(path string) (string, error) {
	input := strings.TrimSpace(path)
	if input == "" {
		return "", errors.New("path required")
	}
	for _, candidate := range mappedPathCandidates(input) {
		if isAccessibleDir(candidate) {
			return filepath.Clean(candidate), nil
		}
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	return "", fmt.Errorf("path is not an accessible directory: %s", abs)
}

func resolveAccessibleMappedPath(path string) (string, os.FileInfo, error) {
	input := strings.TrimSpace(path)
	if input == "" {
		return "", nil, errors.New("path required")
	}
	candidates := mappedPathCandidates(input)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate), info, nil
		}
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", nil, fmt.Errorf("invalid path: %w", err)
	}
	return "", nil, fmt.Errorf("path is not accessible: %s", abs)
}

func resolveMappedDestinationPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	clean := filepath.Clean(path)
	if _, err := os.Stat(clean); err == nil && !isRelativeVolumeMarkerPath(clean) {
		return clean
	}
	for _, candidate := range mappedPathCandidates(clean) {
		if candidate == clean {
			continue
		}
		return filepath.Clean(candidate)
	}
	return clean
}

func mappedPathCandidates(input string) []string {
	var candidates []string
	add := func(candidate string) {
		candidate = filepath.Clean(filepath.FromSlash(strings.TrimSpace(candidate)))
		if candidate == "" || candidate == "." {
			return
		}
		for _, existing := range candidates {
			if sameLibraryPath(existing, candidate) {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	clean := filepath.Clean(input)
	if isRelativeVolumeMarkerPath(clean) {
		for _, candidate := range dockerVolumePathCandidates(clean) {
			add(candidate)
		}
	}
	add(clean)
	for _, candidate := range dockerVolumePathCandidates(input) {
		add(candidate)
	}
	for _, candidate := range dockerVolumePathCandidates(clean) {
		add(candidate)
	}
	if slashClean := cleanPathForVolumeMapping(input); slashClean != "" {
		add(slashClean)
	}
	if abs, err := filepath.Abs(input); err == nil {
		add(abs)
		for _, candidate := range dockerVolumePathCandidates(abs) {
			add(candidate)
		}
	}
	return candidates
}

func isAccessibleDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func dockerVolumePathCandidates(path string) []string {
	normalized := cleanPathForVolumeMapping(path)
	var candidates []string
	addCandidate := func(candidate string) {
		candidate = filepath.Clean(filepath.FromSlash(candidate))
		for _, existing := range candidates {
			if sameLibraryPath(existing, candidate) {
				return
			}
		}
		candidates = append(candidates, candidate)
	}

	for _, mapping := range []struct {
		env       string
		container string
	}{
		{env: "MEDIASTATION_MEDIA_DIR", container: envOrDefault("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media")},
		{env: "MEDIASTATION_DOWNLOAD_DIR", container: envOrDefault("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "/downloads")},
	} {
		container := cleanPathForVolumeMapping(mapping.container)
		if container == "." || container == "" || strings.HasPrefix(container, ".") {
			continue
		}
		relativeMarker := strings.TrimPrefix(normalized, "./")
		if !strings.HasPrefix(relativeMarker, "/") {
			containerBase := strings.TrimSpace(filepath.Base(container))
			if relativeMarker == containerBase {
				addCandidate(container)
				continue
			}
			if strings.HasPrefix(relativeMarker, containerBase+"/") {
				addCandidate(container + strings.TrimPrefix(relativeMarker, containerBase))
				continue
			}
		}
		host := cleanPathForVolumeMapping(os.Getenv(mapping.env))
		if host == "." || host == "" || strings.HasPrefix(host, ".") {
			continue
		}
		if normalized == host {
			addCandidate(mapping.container)
			continue
		}
		if strings.HasPrefix(normalized, host+"/") {
			addCandidate(mapping.container + strings.TrimPrefix(normalized, host))
		}
		if normalized == container {
			addCandidate(host)
			continue
		}
		if strings.HasPrefix(normalized, container+"/") {
			addCandidate(host + strings.TrimPrefix(normalized, container))
		}
	}

	for _, marker := range []struct {
		part      string
		container string
	}{
		{part: "/media", container: envOrDefault("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media")},
		{part: "/downloads", container: envOrDefault("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "/downloads")},
	} {
		part := strings.TrimRight(marker.part, "/")
		container := strings.TrimRight(filepath.ToSlash(marker.container), "/")
		markerPath := pathAfterWindowsDrivePrefix(normalized)
		if markerPath == part {
			addCandidate(container)
			continue
		}
		if strings.HasPrefix(markerPath, part+"/") {
			addCandidate(container + strings.TrimPrefix(markerPath, part))
		}
	}

	return candidates
}

func isRelativeVolumeMarkerPath(path string) bool {
	normalized := cleanPathForVolumeMapping(path)
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" || strings.HasPrefix(normalized, "/") || filepath.IsAbs(path) {
		return false
	}
	for _, marker := range []string{"media", "downloads"} {
		if normalized == marker || strings.HasPrefix(normalized, marker+"/") {
			return true
		}
	}
	return false
}

func cleanPathForVolumeMapping(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, "\\", "/")
	path = trimEmbeddedWindowsDrive(path)
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
}

func pathAfterWindowsDrivePrefix(path string) string {
	if len(path) >= 3 && path[1] == ':' && path[2] == '/' && isASCIIAlpha(path[0]) {
		return path[2:]
	}
	return path
}

func trimEmbeddedWindowsDrive(path string) string {
	for i := 0; i+2 < len(path); i++ {
		if !isASCIIAlpha(path[i]) || path[i+1] != ':' || path[i+2] != '/' {
			continue
		}
		if i == 0 || path[i-1] == '/' {
			return path[i:]
		}
	}
	return path
}

func isASCIIAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func sameLibraryPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
