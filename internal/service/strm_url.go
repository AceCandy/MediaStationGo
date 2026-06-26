package service

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *STRMService) strmPlaybackURL(ctx context.Context, media model.Media, baseURL, playbackToken string) string {
	if media.ID == "" {
		return ""
	}
	query := url.Values{}
	token := strings.TrimSpace(playbackToken)
	if token == "" {
		token = s.defaultSTRMPlaybackToken(ctx)
	}
	if token != "" {
		query.Set("token", token)
	}
	return buildAbsoluteSTRMAPIURL(firstNonEmpty(baseURL, PublicServerURL(ctx, s.repo, s.cfg)), "/api/stream/"+url.PathEscape(media.ID), query)
}

func (s *STRMService) defaultSTRMPlaybackToken(ctx context.Context) string {
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
	token, err := signSTRMPlaybackToken(admin, s.cfg.Secrets.JWTSecret)
	if err != nil {
		if s.log != nil {
			s.log.Warn("sign strm playback token failed", zap.Error(err))
		}
		return ""
	}
	return token
}

func signSTRMPlaybackToken(u *model.User, secret string) (string, error) {
	if u == nil || strings.TrimSpace(u.ID) == "" || strings.TrimSpace(secret) == "" {
		return "", ErrSTRMURLInvalid
	}
	claims := Claims{
		UserID: u.ID,
		Role:   u.Role,
		Tier:   u.Tier,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(EmbyTokenDuration)),
			Issuer:    "mediastationgo",
			Subject:   u.ID,
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
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
