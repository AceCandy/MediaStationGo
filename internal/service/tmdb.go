// Package service — TMDb metadata provider.
//
// TMDbProvider implements the (minimal) MetadataProvider interface and uses
// the public The Movie Database REST API. The API key is taken from
// secrets.tmdb_api_key; when empty the provider returns nil from every
// method so the scraper can no-op gracefully.
//
// We only call the two endpoints the scrape pipeline actually needs:
//
//	GET /search/movie?query=...&year=...
//	GET /movie/{id}?language=zh-CN
//
// TV / anime support follows the same pattern; for the bootstrap we expose
// a single SearchMovie path so that the home page and library gallery can
// show real posters as soon as a TMDb key is configured.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// TMDbProvider talks to https://api.themoviedb.org/3.
type TMDbProvider struct {
	cfg       *config.Config
	log       *zap.Logger
	client    *http.Client
	base      string
	imgCDN    string
	apiConfig *APIConfigService
}

type tmdbHTTPStatusError struct {
	Path       string
	StatusCode int
}

func (e *tmdbHTTPStatusError) Error() string {
	return fmt.Sprintf("tmdb endpoint %s returned status %d", e.Path, e.StatusCode)
}

func isTMDbHTTPStatus(err error, statusCode int) bool {
	var statusErr *tmdbHTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == statusCode
}

// NewTMDbProvider is the constructor. APIBase / image CDN can be overridden
// via secrets.tmdb_api_proxy + tmdb_image_proxy for users behind GFW.
// apiConfig is optional; when non-nil, the provider will also check the
// api_configs table for TMDB API key.
func NewTMDbProvider(cfg *config.Config, log *zap.Logger, apiConfig *APIConfigService) *TMDbProvider {
	base := cfg.Secrets.TMDbAPIProxy
	if base == "" {
		base = "https://api.themoviedb.org/3"
	}
	img := cfg.Secrets.TMDbImageProxy
	if img == "" {
		img = "https://image.tmdb.org/t/p"
	}
	return &TMDbProvider{
		cfg:       cfg,
		log:       log,
		apiConfig: apiConfig,
		base:      base,
		imgCDN:    img,
		// 默认 8s 超时：首页同时调 trending + popular，15s 太久会让用户感觉
		// 卡死。如果 TMDb 真有问题，handler 层会快速降级返回空列表。
		// 同时让 client 读取 HTTP(S)_PROXY 与 Windows 本机系统代理。
		client: NewExternalHTTPClient(8 * time.Second),
	}
}

// Enabled reports whether the operator has supplied an API key.
// It checks both the config file and the database (via apiConfig).
func (t *TMDbProvider) Enabled() bool {
	// Fast path: check config
	if t.cfg.Secrets.TMDbAPIKey != "" {
		return true
	}
	// Secondary check: if we have apiConfig, the key might be in the database
	// We can't query the database here (no ctx), so we rely on the caller
	// to check properly before making API calls.
	// The actual key resolution happens in resolveAPIKey(ctx).
	return t.apiConfig != nil
}

// resolveAPIKey returns the TMDb API key, checking config first, then database.
func (t *TMDbProvider) resolveAPIKey(ctx context.Context) string {
	// Check config first (fast path)
	if t.cfg.Secrets.TMDbAPIKey != "" {
		t.log.Debug("tmdb: using API key from config file")
		return t.cfg.Secrets.TMDbAPIKey
	}
	// Fall back to database
	if t.apiConfig != nil {
		resolved, err := t.apiConfig.Resolve(ctx, "tmdb")
		if err != nil {
			t.log.Warn("tmdb: failed to resolve API key from database", zap.Error(err))
		} else if resolved.APIKey == "" {
			t.log.Warn("tmdb: API key is empty in database")
		} else {
			return resolved.APIKey
		}
	} else {
		t.log.Warn("tmdb: apiConfig is nil, cannot resolve API key from database")
	}
	return ""
}

// resolveBaseURL returns the TMDb base URL, checking config first, then database.
func (t *TMDbProvider) resolveBaseURL(ctx context.Context) string {
	// Check config first
	base := t.cfg.Secrets.TMDbAPIProxy
	if base == "" {
		base = "https://api.themoviedb.org/3"
	}
	// Override from database if available
	if t.apiConfig != nil {
		resolved, err := t.apiConfig.Resolve(ctx, "tmdb")
		if err == nil && resolved.BaseURL != "" {
			base = resolved.BaseURL
		}
	}
	return base
}

func (t *TMDbProvider) getJSON(ctx context.Context, url string, out any) error {
	_, err := t.getJSONRaw(ctx, url, out)
	return err
}

func (t *TMDbProvider) getJSONRaw(ctx context.Context, rawURL string, out any) ([]byte, error) {
	const maxAttempts = 4
	for attempt := 0; attempt < maxAttempts; attempt++ {
		body, retryAfter, err := t.getJSONAttempt(ctx, rawURL, out)
		if err == nil {
			return body, nil
		}
		if retryAfter < 0 || attempt == maxAttempts-1 {
			return nil, err
		}
		if retryAfter == 0 {
			retryAfter = time.Second << attempt
		}
		if retryAfter > 30*time.Second {
			retryAfter = 30 * time.Second
		}
		timer := time.NewTimer(retryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, errors.New("tmdb request failed")
}

func (t *TMDbProvider) getJSONAttempt(ctx context.Context, rawURL string, out any) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, -1, errors.New("invalid tmdb request")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, -1, ctx.Err()
		}
		return nil, -1, fmt.Errorf("tmdb endpoint %s request failed", req.URL.Path)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		retryAfter := time.Duration(-1)
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		}
		return nil, retryAfter, &tmdbHTTPStatusError{Path: req.URL.Path, StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, -1, err
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(out); err != nil {
		return nil, -1, fmt.Errorf("tmdb endpoint %s returned invalid JSON: %w", req.URL.Path, err)
	}
	return body, -1, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := at.Sub(now); delay > 0 {
			return delay
		}
		return 0
	}
	return 0
}
