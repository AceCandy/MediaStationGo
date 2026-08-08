package service

import "strings"

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func detectResolutionScore(title string) int {
	title = strings.ToLower(title)
	switch {
	case titleMatchesResolution(title, "2160p"):
		return 4
	case titleMatchesResolution(title, "1080p"):
		return 3
	case titleMatchesResolution(title, "720p"):
		return 2
	default:
		return 1
	}
}

func titleMatchesResolution(title, resolution string) bool {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "2160p", "4k", "uhd":
		return strings.Contains(title, "2160p") || strings.Contains(title, "4k") || strings.Contains(title, "uhd")
	case "1080p":
		return strings.Contains(title, "1080p") || strings.Contains(title, "fhd")
	case "720p":
		return strings.Contains(title, "720p")
	default:
		return strings.Contains(title, strings.ToLower(strings.TrimSpace(resolution)))
	}
}
