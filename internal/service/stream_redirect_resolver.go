package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	playbackRedirectCacheTTL       = time.Hour
	playbackRedirectResolveTimeout = 15 * time.Second
)

var (
	errPlaybackRedirectSource   = errors.New("invalid playback redirect source")
	errPlaybackRedirectRequest  = errors.New("playback redirect request failed")
	errPlaybackRedirectLocation = errors.New("invalid playback redirect location")
)

type playbackRedirectCacheEntry struct {
	target    string
	local     bool
	expiresAt time.Time
}

type playbackRedirectFlight struct {
	done   chan struct{}
	result playbackRedirectResolveResult
	err    error
}

type playbackRedirectResolveResult struct {
	target         string
	local          bool
	cacheHit       bool
	fallbackStatus int
}

type playbackRedirectResponseError struct {
	status int
}

func (e *playbackRedirectResponseError) Error() string {
	return fmt.Sprintf("playback redirect response status %d", e.status)
}

type playbackRedirectResolver struct {
	client  *http.Client
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	cache   map[string]playbackRedirectCacheEntry
	flights map[string]*playbackRedirectFlight
}

func newPlaybackRedirectResolver() *playbackRedirectResolver {
	return &playbackRedirectResolver{
		client:  &http.Client{Timeout: playbackRedirectResolveTimeout},
		ttl:     playbackRedirectCacheTTL,
		now:     time.Now,
		cache:   make(map[string]playbackRedirectCacheEntry),
		flights: make(map[string]*playbackRedirectFlight),
	}
}

func (r *playbackRedirectResolver) Resolve(ctx context.Context, mediaID, rawURL, userAgent string, localFallback func() string) (playbackRedirectResolveResult, error) {
	source := strings.TrimSpace(rawURL)
	base, ok := absolutePlaybackRedirectURL(source)
	if r == nil || !ok {
		return playbackRedirectResolveResult{}, errPlaybackRedirectSource
	}
	key := mediaID + "\x00" + userAgent
	now := r.currentTime()

	r.mu.Lock()
	r.removeExpiredLocked(now)
	if cached, found := r.cache[key]; found {
		r.mu.Unlock()
		return playbackRedirectResolveResult{target: cached.target, local: cached.local, cacheHit: true}, nil
	}
	if flight, found := r.flights[key]; found {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return playbackRedirectResolveResult{}, ctx.Err()
		case <-flight.done:
			if flight.err != nil {
				return playbackRedirectResolveResult{}, flight.err
			}
			result := flight.result
			result.cacheHit = true
			return result, nil
		}
	}
	flight := &playbackRedirectFlight{done: make(chan struct{})}
	r.flights[key] = flight
	r.mu.Unlock()

	target, err := r.resolve(ctx, base, userAgent)
	result := playbackRedirectResolveResult{target: target}
	var statusErr *playbackRedirectResponseError
	if errors.As(err, &statusErr) && statusErr.status == http.StatusInternalServerError && localFallback != nil {
		if localPath := localFallback(); localPath != "" {
			result.target = localPath
			result.local = true
			result.fallbackStatus = statusErr.status
			err = nil
		}
	}
	r.mu.Lock()
	if err == nil {
		ttl := r.ttl
		if ttl <= 0 {
			ttl = playbackRedirectCacheTTL
		}
		r.cache[key] = playbackRedirectCacheEntry{target: result.target, local: result.local, expiresAt: r.currentTime().Add(ttl)}
	}
	flight.result, flight.err = result, err
	close(flight.done)
	delete(r.flights, key)
	r.mu.Unlock()
	return result, err
}

func (r *playbackRedirectResolver) resolve(ctx context.Context, source *url.URL, userAgent string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return "", errPlaybackRedirectSource
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header["User-Agent"] = []string{userAgent}

	baseClient := r.client
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	client := *baseClient
	if client.Timeout <= 0 || client.Timeout > playbackRedirectResolveTimeout {
		client.Timeout = playbackRedirectResolveTimeout
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errPlaybackRedirectRequest
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusMultipleChoices || resp.StatusCode >= http.StatusBadRequest {
		return "", &playbackRedirectResponseError{status: resp.StatusCode}
	}
	location, err := url.Parse(strings.TrimSpace(resp.Header.Get("Location")))
	if err != nil || location.String() == "" {
		return "", errPlaybackRedirectLocation
	}
	resolved := source.ResolveReference(location)
	if _, ok := absolutePlaybackRedirectURL(resolved.String()); !ok {
		return "", errPlaybackRedirectLocation
	}
	return resolved.String(), nil
}

func (r *playbackRedirectResolver) currentTime() time.Time {
	if r != nil && r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *playbackRedirectResolver) removeExpiredLocked(now time.Time) {
	for key, entry := range r.cache {
		if !now.Before(entry.expiresAt) {
			delete(r.cache, key)
		}
	}
}

type playbackRedirectResolution struct {
	target   string
	matched  bool
	resolved bool
	cacheHit bool
	local    bool
}

func (s *StreamService) resolveConfiguredPlaybackRedirect(ctx context.Context, mediaID, rawURL, userAgent string) playbackRedirectResolution {
	result := playbackRedirectResolution{target: strings.TrimSpace(rawURL)}
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return result
	}
	prefixes, err := s.repo.Setting.Get(ctx, PlaybackRedirectResolvePrefixesSettingKey)
	if err != nil || !matchesPlaybackRedirectPrefix(result.target, parsePlaybackRedirectResolvePrefixes(prefixes)) {
		return result
	}
	result.matched = true
	if s.redirectResolver == nil {
		return result
	}
	resolved, err := s.redirectResolver.Resolve(ctx, mediaID, result.target, userAgent, func() string {
		return s.mappedRemotePlaybackPath(ctx, result.target)
	})
	if err != nil {
		if s.log != nil {
			fields := append(PlaybackURLLogFields(result.target), zap.Error(err))
			s.log.Warn("media playback redirect resolve failed", fields...)
		}
		return result
	}
	if resolved.fallbackStatus != 0 && s.log != nil {
		fields := append(PlaybackURLLogFields(result.target), zap.Int("status", resolved.fallbackStatus))
		s.log.Warn("media playback redirect resolve fell back to local file", fields...)
	}
	result.target = resolved.target
	result.resolved = true
	result.cacheHit = resolved.cacheHit
	result.local = resolved.local
	return result
}

func (s *StreamService) mappedRemotePlaybackPath(ctx context.Context, rawURL string) string {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return ""
	}
	rawMappings, err := s.repo.Setting.Get(ctx, FFprobePathMappingsSettingKey)
	if err != nil {
		return ""
	}
	path := mapRemoteProbePath(rawMappings, rawURL)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func (r playbackRedirectResolution) logFields() []zap.Field {
	if !r.matched {
		return nil
	}
	source := "fallback"
	if r.resolved {
		source = "upstream"
		if r.cacheHit {
			source = "cache"
		}
	}
	return []zap.Field{
		zap.String("redirect_resolve_source", source),
		zap.Bool("cache_hit", r.cacheHit),
	}
}

func parsePlaybackRedirectResolvePrefixes(raw string) []string {
	prefixes := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		prefix := strings.TrimSpace(line)
		if _, ok := absolutePlaybackRedirectURL(prefix); ok {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}

func matchesPlaybackRedirectPrefix(target string, prefixes []string) bool {
	if _, ok := absolutePlaybackRedirectURL(target); !ok {
		return false
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

func absolutePlaybackRedirectURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || !u.IsAbs() || u.Host == "" || u.User != nil {
		return nil, false
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "http", "https":
		return u, true
	default:
		return nil, false
	}
}
