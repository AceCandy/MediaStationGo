package service

import (
	"net/url"
	"strings"
)

func validRemoteArtworkURL(raw string) bool {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
