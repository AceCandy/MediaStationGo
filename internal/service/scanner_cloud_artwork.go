package service

import (
	"net/url"
	"strings"
)

func ParseCloudArtworkURL(raw string) (string, string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	typ := ""
	for _, prefix := range []string{"api/img/cloud/", "api/cloud/play/"} {
		if strings.HasPrefix(strings.ToLower(path), prefix) {
			typ = strings.TrimSpace(path[len(prefix):])
			break
		}
	}
	if typ == "" {
		return "", "", false
	}
	ref := strings.TrimSpace(u.Query().Get("ref"))
	if typ == "" || ref == "" || !isCloudArtworkRef(ref) {
		return "", "", false
	}
	return typ, ref, true
}

func CloudArtworkURL(typ, ref string) string {
	typ = strings.Trim(strings.ReplaceAll(strings.TrimSpace(typ), "\\", "/"), "/")
	ref = strings.TrimSpace(ref)
	if typ == "" || ref == "" {
		return ""
	}
	return "/api/img/cloud/" + url.PathEscape(typ) + "?ref=" + url.QueryEscape(ref)
}

func isCloudArtworkRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, suffix := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".tbn"} {
		if strings.HasSuffix(ref, suffix) {
			return true
		}
	}
	return false
}
