package service

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// normalizeSTRMHTTPURL 兼容把 URL 路径中的 # 原样写入 STRM 的历史文件。
func normalizeSTRMHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if !isHTTPPlaybackTarget(raw) {
		return raw
	}
	return strings.ReplaceAll(raw, "#", "%23")
}

func (s *STRMService) strmPlaybackURL(ctx context.Context, media model.Media, baseURL, playbackToken string) string {
	if media.ID == "" {
		return ""
	}
	query := url.Values{}
	token := s.scopedSTRMPlaybackToken(ctx, media, playbackToken)
	if token != "" {
		query.Set("token", token)
	}
	return buildAbsoluteSTRMAPIURL(firstNonEmpty(baseURL, PublicServerURL(ctx, s.repo, s.cfg)), "/api/stream/"+url.PathEscape(media.ID), query)
}

func (s *STRMService) scopedSTRMPlaybackToken(ctx context.Context, media model.Media, playbackToken string) string {
	playbackToken = strings.TrimSpace(playbackToken)
	if playbackToken == "" {
		return s.defaultSTRMPlaybackToken(ctx, media)
	}
	if s == nil || s.cfg == nil {
		return ""
	}
	claims, err := validateAccessToken(playbackToken, s.cfg.Secrets.JWTSecret)
	if err != nil || claims.UserID == "" || (claims.Purpose != "" && claims.Purpose != ExternalPlaybackTokenPurpose) {
		return ""
	}
	if claims.Purpose == ExternalPlaybackTokenPurpose && claims.MediaID != media.ID {
		return ""
	}
	token, err := signExternalPlaybackToken(*claims, media.ID, s.probeDurationSec(ctx, media.ID), s.cfg.Secrets.JWTSecret)
	if err != nil {
		if s.log != nil {
			s.log.Warn("sign strm playback token failed", zap.Error(err))
		}
		return ""
	}
	return token
}

func (s *STRMService) defaultSTRMPlaybackToken(ctx context.Context, media model.Media) string {
	if s == nil || s.repo == nil || s.repo.User == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Secrets.JWTSecret) == "" {
		return ""
	}
	admin, err := s.repo.User.FirstAdmin(ctx)
	if err != nil || admin == nil {
		if err != nil && s.log != nil {
			s.log.Warn("generate strm playback token failed", zap.Error(err))
		}
		return ""
	}
	token, err := signExternalPlaybackToken(Claims{
		UserID: admin.ID,
		Role:   admin.Role,
		Tier:   admin.Tier,
	}, media.ID, s.probeDurationSec(ctx, media.ID), s.cfg.Secrets.JWTSecret)
	if err != nil {
		if s.log != nil {
			s.log.Warn("sign strm playback token failed", zap.Error(err))
		}
		return ""
	}
	return token
}

func (s *STRMService) probeDurationSec(ctx context.Context, mediaID string) int {
	if s == nil || s.repo == nil || s.repo.MediaProbe == nil {
		return 0
	}
	probe, _ := s.repo.MediaProbe.FindByMediaID(ctx, mediaID)
	if probe == nil {
		return 0
	}
	return int(probe.DurationMS / 1000)
}

func (s *STRMService) strmRelativePath(lib model.Library, media model.Media) string {
	title := strings.TrimSpace(media.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(media.Path), filepath.Ext(media.Path))
	}
	if title == "" {
		return ""
	}
	seriesLike := isSeriesLibraryType(lib.Type) || media.SeasonNum > 0 || media.EpisodeNum > 0
	if seriesLike {
		show := inferSeriesNameFromPath(media.Path)
		if show == "" {
			show = title
		}
		season := media.SeasonNum
		episode := media.EpisodeNum
		if season <= 0 || episode <= 0 {
			parsedSeason, parsedEpisode := ParseEpisode(media.Path)
			if season <= 0 {
				season = parsedSeason
			}
			if episode <= 0 {
				episode = parsedEpisode
			}
		}
		if season <= 0 {
			season = 1
		}
		name := strings.TrimSuffix(filepath.Base(media.Path), filepath.Ext(media.Path))
		if episode > 0 {
			name = fmt.Sprintf("%s - S%02dE%02d", show, season, episode)
		} else if strings.TrimSpace(name) == "" {
			name = title
		}
		return filepath.Join(sanitizeFilename(show), fmt.Sprintf("Season %02d", season), sanitizeFilename(name)+".strm")
	}
	folder := title
	if media.Year > 0 && !strings.Contains(folder, strconv.Itoa(media.Year)) {
		folder = fmt.Sprintf("%s (%d)", folder, media.Year)
	}
	safe := sanitizeFilename(folder)
	return filepath.Join(safe, safe+".strm")
}

func (s *STRMService) strmTreeRelativePath(media model.Media) string {
	parts := strmLibraryPathParts(media.Path)
	if len(parts) == 0 {
		return ""
	}
	parts = strmDropCategoryPrefix(parts)
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	ext := filepath.Ext(last)
	if ext == "" {
		return ""
	}
	parts[len(parts)-1] = strings.TrimSuffix(last, ext) + ".strm"
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if safe := sanitizeFilename(part); safe != "" {
			clean = append(clean, safe)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return filepath.Join(clean...)
}

func strmDropCategoryPrefix(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}
	for i, part := range parts {
		if strmCanonicalRoot(part) == "" && strmCategoryRoot(part) == "" {
			continue
		}
		next := i + 1
		if strmCanonicalRoot(part) != "" && next < len(parts) && strmCategoryRoot(parts[next]) != "" {
			next++
		}
		if next < len(parts) {
			return append([]string(nil), parts[next:]...)
		}
	}
	return append([]string(nil), parts...)
}

func absolutizeSTRMURL(raw, baseURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "//") {
		return raw
	}
	u, err := url.Parse(raw)
	if err == nil && u.IsAbs() {
		return raw
	}
	return buildAbsoluteSTRMAPIURL(baseURL, raw, nil)
}

func buildAbsoluteSTRMAPIURL(baseURL, apiPath string, query url.Values) string {
	apiPath = "/" + strings.TrimLeft(strings.TrimSpace(apiPath), "/")
	if query != nil && len(query) > 0 {
		apiPath += "?" + query.Encode()
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return apiPath
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return apiPath
	}
	target, err := url.Parse(apiPath)
	if err != nil {
		return apiPath
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(target.Path, "/")
	base.RawQuery = target.RawQuery
	base.Fragment = ""
	return base.String()
}
