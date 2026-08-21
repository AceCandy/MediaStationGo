package service

import (
	"net/http"
	"net/url"
	"strings"
)

// withExternalPlaybackTokenForInternalRedirect exchanges caller credentials
// for a short-lived token scoped to the persisted internal stream target.
func (s *StreamService) withExternalPlaybackTokenForInternalRedirect(target string, r *http.Request, publicBase, sourceMediaID string) string {
	if r == nil {
		return target
	}
	if strings.HasPrefix(target, "//") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	if !isInternalAPIURL(u, r, publicBase) {
		return target
	}
	targetMediaID, ok := internalStreamRedirectMediaID(u)
	if !ok {
		return target
	}
	q := u.Query()
	for _, key := range []string{"token", "api_key", "apiKey", "ApiKey"} {
		q.Del(key)
	}
	secret := ""
	if s != nil && s.cfg != nil {
		secret = s.cfg.Secrets.JWTSecret
	}
	claims, err := validateAccessToken(requestToken(r), secret)
	if err == nil && claims.UserID != "" && (claims.Purpose == "" || claims.Purpose == ExternalPlaybackTokenPurpose) {
		sourceMediaID = strings.TrimSpace(sourceMediaID)
		if claims.Purpose != ExternalPlaybackTokenPurpose || claims.MediaID == sourceMediaID {
			durationSec := 0
			if s != nil && s.repo != nil && s.repo.MediaProbe != nil {
				if probe, findErr := s.repo.MediaProbe.FindByMediaID(r.Context(), targetMediaID); findErr == nil && probe != nil {
					durationSec = int(probe.DurationMS / 1000)
				}
			}
			if token, signErr := signExternalPlaybackToken(*claims, targetMediaID, durationSec, secret); signErr == nil {
				q.Set("token", token)
			}
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func internalStreamRedirectMediaID(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "api") || !strings.EqualFold(parts[1], "stream") {
		return "", false
	}
	mediaID := strings.TrimSpace(parts[2])
	return mediaID, mediaID != ""
}

func absoluteInternalRedirect(target string, r *http.Request) string {
	if r == nil || target == "" || strings.HasPrefix(target, "//") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || !strings.HasPrefix(target, "/") {
		return target
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return target
	}
	u.Scheme = scheme
	u.Host = host
	return u.String()
}

func isInternalAPIURL(u *url.URL, r *http.Request, publicBase string) bool {
	if u == nil || !strings.HasPrefix(strings.ToLower(u.Path), "/api/") {
		return false
	}
	targetHost := strings.ToLower(strings.TrimSpace(u.Host))
	targetScheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if targetHost == "" {
		return targetScheme == ""
	}
	if targetScheme != "http" && targetScheme != "https" {
		return false
	}
	if r != nil {
		requestScheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
		if requestScheme == "" {
			if r.TLS != nil {
				requestScheme = "https"
			} else {
				requestScheme = "http"
			}
		}
		if host := strings.ToLower(strings.TrimSpace(r.Host)); host != "" && targetHost == host && strings.EqualFold(targetScheme, requestScheme) {
			return true
		}
		if host := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))); host != "" && targetHost == host && strings.EqualFold(targetScheme, requestScheme) {
			return true
		}
	}
	if publicBase != "" {
		if base, err := url.Parse(publicBase); err == nil && strings.EqualFold(strings.TrimSpace(base.Host), targetHost) && strings.EqualFold(strings.TrimSpace(base.Scheme), targetScheme) {
			return true
		}
	}
	return false
}

// requestToken extracts the bearer JWT from the incoming request the same way
// the auth middleware does (Authorization header, Emby token headers, or the
// token / api_key query params used by <video>.src).
func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	for _, hk := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if v := strings.TrimSpace(r.Header.Get(hk)); v != "" {
			return v
		}
	}
	for _, hk := range []string{"X-Emby-Authorization", "X-MediaBrowser-Authorization"} {
		if token := streamTokenFromAuthHeader(r.Header.Get(hk)); token != "" {
			return token
		}
	}
	if token := streamTokenFromAuthHeader(r.Header.Get("Authorization")); token != "" {
		return token
	}
	for _, k := range []string{"token", "api_key", "apiKey", "ApiKey"} {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func streamTokenFromAuthHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, prefix := range []string{"Bearer ", "Emby "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	if strings.HasPrefix(value, "MediaBrowser ") || strings.Contains(value, "Token=") {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "MediaBrowser "))
			if !strings.HasPrefix(part, "Token=") {
				continue
			}
			token := strings.TrimSpace(strings.TrimPrefix(part, "Token="))
			return strings.Trim(token, `"`)
		}
		return ""
	}
	return value
}
