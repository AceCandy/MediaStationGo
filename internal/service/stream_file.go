package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ServeFile streams the file backing the given media ID using
// http.ServeContent so HEAD / Range / If-Modified-Since are handled for free.
//
// When the media row has a STRMURL set we redirect (302) to that URL
// instead of opening a local file. This lets WebDAV / Alist / S3 / HTTP
// direct links flow through the rest of the player UI unchanged.
func (s *StreamService) ServeFile(w http.ResponseWriter, r *http.Request, mediaID string) error {
	return s.ServeFileWithCloudMode(w, r, mediaID, "")
}

func (s *StreamService) ServeFileWithCloudMode(w http.ResponseWriter, r *http.Request, mediaID, cloudMode string) error {
	m, err := s.repo.Media.FindByID(r.Context(), mediaID)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrMediaNotFound
	}
	if target := localSTRMFileTarget(m); target != "" {
		if handled, err := s.redirectMappedPlaybackPath(w, r, mediaID, target); err != nil {
			return err
		} else if handled {
			return nil
		}
		return s.serveLocalMediaFile(w, r, mediaID, target, "local_strm")
	}
	if strmURL := strings.TrimSpace(m.STRMURL); strmURL != "" && playableSTRMTarget(r.Context(), s.repo, strmURL, m) {
		if !cloudPlaybackModeEnabled(r.Context(), s.repo, cloudMode) {
			return ErrCloudPlaybackDisabled
		}
		// 云盘播放 URL 先规范化为相对路径，免疫扫描时固化的旧 host。
		target := normalizeCloudPlayTarget(strmURL)
		target = withAuthTokenForInternalRedirect(target, r, PublicServerURL(r.Context(), s.repo, s.cfg))
		redirectTarget := absoluteInternalRedirect(target, r)
		setCloudRedirectNoStore(w)
		if !isCloudPlaybackTarget(strmURL) && s.log != nil {
			fields := []zap.Field{
				zap.String("media_id", mediaID),
				zap.String("playback_source", "remote_redirect"),
				zap.String("resolve_source", "configured"),
				zap.String("method", r.Method),
				zap.String("range", r.Header.Get("Range")),
			}
			s.log.Info("media playback redirect", append(fields, PlaybackURLLogFields(redirectTarget)...)...)
		}
		http.Redirect(w, r, redirectTarget, http.StatusFound)
		return nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Path)), "cloud://") {
		// 云盘媒体没有本地文件可回退；走到这里说明 STRM 播放被关闭或
		// STRMURL 缺失。返回明确错误而不是笼统的「文件不存在」，
		// 处理器据此回 502 + 原因，方便用户在播放器/日志里定位。
		return ErrCloudPlaybackUnavailable
	}
	if handled, err := s.redirectMappedPlaybackPath(w, r, mediaID, m.Path); err != nil {
		return err
	} else if handled {
		return nil
	}
	return s.serveLocalMediaFile(w, r, mediaID, m.Path, "local_file")
}

func (s *StreamService) redirectMappedPlaybackPath(w http.ResponseWriter, r *http.Request, mediaID, localPath string) (bool, error) {
	target := s.playbackPathRedirectURL(r.Context(), localPath)
	if target == "" {
		return false, nil
	}
	if s.storageCfg == nil {
		return true, errors.New("playback redirect resolver unavailable")
	}
	resolved, cacheHit, err := s.storageCfg.ResolveHTTPRedirectWithCacheStatus(r.Context(), target, r.UserAgent())
	if err != nil {
		return true, err
	}
	setCloudRedirectNoStore(w)
	if s.log != nil {
		fields := []zap.Field{
			zap.String("media_id", mediaID),
			zap.String("playback_source", "remote_redirect"),
			zap.String("resolve_source", "path_mapping"),
			zap.String("path", localPath),
			zap.String("method", r.Method),
			zap.String("range", r.Header.Get("Range")),
			zap.Bool("cache_hit", cacheHit),
		}
		s.log.Info("media playback redirect", append(fields, PlaybackURLLogFields(resolved)...)...)
	}
	http.Redirect(w, r, resolved, http.StatusFound)
	return true, nil
}

// playbackPathRedirectURL maps a local media path to a configured remote URL.
// Each setting line uses `local path => remote URL`; the longest matching path wins.
func (s *StreamService) playbackPathRedirectURL(ctx context.Context, localPath string) string {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return ""
	}
	raw, err := s.repo.Setting.Get(ctx, PlaybackPathMappingsSettingKey)
	if err != nil {
		return ""
	}
	cleanPath := filepath.Clean(strings.TrimSpace(localPath))
	bestPrefix, bestTarget := "", ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=>", 2)
		if len(parts) != 2 {
			continue
		}
		prefix := filepath.Clean(strings.TrimSpace(parts[0]))
		if prefix == "" || prefix == "." || len(prefix) <= len(bestPrefix) {
			continue
		}
		rel, err := filepath.Rel(prefix, cleanPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		target, ok := joinPlaybackRedirectURL(parts[1], rel)
		if !ok {
			continue
		}
		bestPrefix, bestTarget = prefix, target
	}
	return bestTarget
}

func joinPlaybackRedirectURL(rawBase, relativePath string) (string, bool) {
	target, err := url.Parse(strings.TrimSpace(rawBase))
	if err != nil || target.Host == "" || !isHTTPPlaybackTarget(target.String()) {
		return "", false
	}
	if relativePath != "" && relativePath != "." {
		target.Path = strings.TrimRight(target.Path, "/") + "/" + strings.TrimLeft(filepath.ToSlash(relativePath), "/")
		target.RawPath = ""
	}
	return target.String(), true
}

func (s *StreamService) serveLocalMediaFile(w http.ResponseWriter, r *http.Request, mediaID, path, source string) error {
	f, err := os.Open(path)
	if err != nil {
		return ErrMediaNotFound
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.log != nil {
		s.log.Info("media playback local",
			zap.String("media_id", mediaID),
			zap.String("playback_source", source),
			zap.String("path", path),
			zap.String("method", r.Method),
			zap.String("range", r.Header.Get("Range")),
		)
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}

// PlaybackURLLogFields returns diagnostics that identify a URL without exposing query values.
func PlaybackURLLogFields(raw string) []zap.Field {
	raw = strings.TrimSpace(raw)
	sum := sha256.Sum256([]byte(raw))
	fields := []zap.Field{zap.String("target_hash", hex.EncodeToString(sum[:]))}
	u, err := url.Parse(raw)
	if err != nil {
		return fields
	}
	queryKeys := make([]string, 0, len(u.Query()))
	for key := range u.Query() {
		queryKeys = append(queryKeys, key)
	}
	sort.Strings(queryKeys)
	return append(fields,
		zap.String("target_scheme", u.Scheme),
		zap.String("target_host", u.Host),
		zap.String("target_path", u.Path),
		zap.Strings("target_query_keys", queryKeys),
	)
}

func setCloudRedirectNoStore(w http.ResponseWriter) {
	if w == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func isCloudPlaybackTarget(raw string) bool {
	_, _, ok := parseCloudMediaPlaybackURL(raw)
	return ok
}

func playableSTRMTarget(ctx context.Context, repo *repository.Container, raw string, m *model.Media) bool {
	if isCloudPlaybackTarget(raw) || isHTTPPlaybackTarget(raw) {
		return true
	}
	if m != nil && strings.EqualFold(strings.TrimSpace(m.Container), "strm") {
		return true
	}
	return STRMPlaybackEnabled(ctx, repo)
}

func isHTTPPlaybackTarget(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || !u.IsAbs() {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	return scheme == "http" || scheme == "https"
}
