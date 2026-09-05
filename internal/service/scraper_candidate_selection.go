package service

import "strings"

func metadataMatchCompatibleWithType(expectedType string, match *Match) bool {
	if match == nil {
		return false
	}
	expectedType = normalizeOrganizeMediaType(expectedType)
	matchType := normalizeOrganizeMediaType(match.MediaType)
	if expectedType == "" || matchType == "" {
		return true
	}
	switch expectedType {
	case "tv", "anime", "variety":
		return matchType == "tv" || matchType == "anime" || matchType == "variety"
	case "movie", "adult":
		return matchType == "movie" || matchType == "adult"
	default:
		return expectedType == matchType
	}
}

func appendMetadataAliases(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(values))
	out := make([]string, 0, len(existing)+len(values))
	add := func(value string) {
		value = strings.TrimSpace(value)
		key := metadataTrustKey(value)
		if value == "" || key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	for _, value := range existing {
		add(value)
	}
	for _, value := range values {
		add(value)
	}
	return out
}
