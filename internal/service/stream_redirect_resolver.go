package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
	expiresAt time.Time
}

type playbackRedirectFlight struct {
	done   chan struct{}
	target string
	err    error
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

func (r *playbackRedirectResolver) Resolve(ctx context.Context, rawURL, userAgent string) (string, bool, error) {
	source := strings.TrimSpace(rawURL)
	base, ok := absolutePlaybackRedirectURL(source)
	if r == nil || !ok {
		return "", false, errPlaybackRedirectSource
	}
	key := source + "\x00" + userAgent
	now := r.currentTime()

	r.mu.Lock()
	r.removeExpiredLocked(now)
	if cached, found := r.cache[key]; found {
		r.mu.Unlock()
		return cached.target, true, nil
	}
	if flight, found := r.flights[key]; found {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-flight.done:
			if flight.err != nil {
				return "", false, flight.err
			}
			return flight.target, true, nil
		}
	}
	flight := &playbackRedirectFlight{done: make(chan struct{})}
	r.flights[key] = flight
	r.mu.Unlock()

	target, err := r.resolve(ctx, base, userAgent)
	r.mu.Lock()
	if err == nil {
		ttl := r.ttl
		if ttl <= 0 {
			ttl = playbackRedirectCacheTTL
		}
		r.cache[key] = playbackRedirectCacheEntry{target: target, expiresAt: r.currentTime().Add(ttl)}
	}
	flight.target, flight.err = target, err
	close(flight.done)
	delete(r.flights, key)
	r.mu.Unlock()
	return target, false, err
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
		return "", fmt.Errorf("playback redirect response status %d", resp.StatusCode)
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
}

func (s *StreamService) resolveConfiguredPlaybackRedirect(ctx context.Context, rawURL, userAgent string) playbackRedirectResolution {
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
	resolved, cacheHit, err := s.redirectResolver.Resolve(ctx, result.target, userAgent)
	if err != nil {
		if s.log != nil {
			fields := append(PlaybackURLLogFields(result.target), zap.Error(err))
			s.log.Warn("media playback redirect resolve failed", fields...)
		}
		return result
	}
	result.target = resolved
	result.resolved = true
	result.cacheHit = cacheHit
	return result
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
