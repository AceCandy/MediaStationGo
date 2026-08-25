package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *ScraperService) matchFromMediaExternalIDs(ctx context.Context, m *model.Media, lib *model.Library) *Match {
	return s.matchFromMediaExternalIDsWithOutcome(ctx, m, lib).Match
}

func (s *ScraperService) matchFromMediaExternalIDsWithOutcome(ctx context.Context, m *model.Media, lib *model.Library) providerLookupResult {
	result := providerLookupResult{}
	if s == nil || m == nil {
		return result
	}
	var lookupErrors []error
	mediaType := ""
	if lib != nil {
		mediaType = lib.Type
	}
	if m.TMDbID > 0 {
		if mediaIsEpisodic(m, lib) {
			mediaType = "tv"
		}
		if s.tmdb != nil && s.tmdb.Enabled() {
			result.Tried = true
			match, err := s.tmdbMatchByID(ctx, m.TMDbID, normalizeMediaType(mediaType, m.Title, ""))
			if err != nil {
				lookupErrors = append(lookupErrors, fmt.Errorf("tmdb id %d: %w", m.TMDbID, err))
			}
			if match != nil {
				if s.mediaExternalIDMatchTrusted(m, lib, match, "tmdb") {
					match.Source = "tmdb"
					match.AllowIdentifierMerge = true
					if !mediaIsEpisodic(m, lib) {
						match.DoubanID = strings.TrimSpace(m.DoubanID)
					}
					preferExistingLocalizedEpisodeTitle(m, lib, match)
					result.Match = match
					return result
				}
			}
		}
	}
	if strings.TrimSpace(m.DoubanID) != "" && s.douban != nil && s.douban.Enabled() {
		result.Tried = true
		if match, err := s.douban.GetMatchByID(ctx, strings.TrimSpace(m.DoubanID)); err == nil && match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "douban") {
				match.Source = "douban"
				match.AllowIdentifierMerge = true
				preferExistingLocalizedEpisodeTitle(m, lib, match)
				result.Match = match
				return result
			}
		} else if err != nil {
			s.log.Debug("douban id lookup failed", zap.String("media_id", m.ID), zap.String("douban_id", m.DoubanID), zap.Error(err))
			lookupErrors = append(lookupErrors, fmt.Errorf("douban id %s: %w", m.DoubanID, err))
		}
	}
	if m.BangumiID > 0 && s.bangumi != nil && s.bangumi.Enabled() {
		result.Tried = true
		if match, err := s.bangumi.GetSubject(ctx, m.BangumiID); err == nil && match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "bangumi") {
				match.Source = "bangumi"
				match.AllowIdentifierMerge = true
				preferExistingLocalizedEpisodeTitle(m, lib, match)
				result.Match = match
				return result
			}
		} else if err != nil {
			s.log.Debug("bangumi id lookup failed", zap.String("media_id", m.ID), zap.Int("bangumi_id", m.BangumiID), zap.Error(err))
			lookupErrors = append(lookupErrors, fmt.Errorf("bangumi id %d: %w", m.BangumiID, err))
		}
	}
	if strings.TrimSpace(m.TheTVDBID) != "" && s.thetvdb != nil && s.thetvdb.Enabled() {
		result.Tried = true
		if match, err := s.thetvdb.GetSeriesMatchByID(ctx, strings.TrimSpace(m.TheTVDBID)); err == nil && match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "thetvdb") {
				match.Source = "thetvdb"
				match.AllowIdentifierMerge = true
				preferExistingLocalizedEpisodeTitle(m, lib, match)
				result.Match = match
				return result
			}
		} else if err != nil {
			s.log.Debug("thetvdb id lookup failed", zap.String("media_id", m.ID), zap.String("thetvdb_id", m.TheTVDBID), zap.Error(err))
			lookupErrors = append(lookupErrors, fmt.Errorf("thetvdb id %s: %w", m.TheTVDBID, err))
		}
	}
	result.Err = errors.Join(lookupErrors...)
	return result
}

func (s *ScraperService) tmdbMatchByID(ctx context.Context, id int, mediaType string) (*Match, error) {
	if s == nil || s.tmdb == nil || !s.tmdb.Enabled() || id <= 0 {
		return nil, nil
	}
	if normalizeMediaType(mediaType, "", "") == "tv" {
		return s.tmdb.GetTVMatch(ctx, id)
	}
	return s.tmdb.GetMovieMatch(ctx, id)
}

func (s *ScraperService) mediaExternalIDMatchTrusted(m *model.Media, lib *model.Library, match *Match, source string) bool {
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return false
	}
	if mediaPathHintMatchesExternalID(m, lib, match, source) {
		return true
	}
	if !mediaIsEpisodic(m, lib) {
		return true
	}
	if mediaExternalIDSourceMatches(m, match, source) && mediaExternalIDLanguageFallbackTrusted(m, match) {
		return true
	}
	for _, candidate := range scrapeQueryCandidates(m, lib) {
		if unsafeAutomaticEpisodeQuery(candidate) {
			continue
		}
		if automaticMetadataTitleTrusted(candidate, match) {
			return true
		}
	}
	if s != nil && s.log != nil && m != nil {
		s.log.Warn("episode external id match rejected",
			zap.String("media_id", m.ID),
			zap.String("source", source),
			zap.String("title", match.Title),
			zap.String("original_name", match.OriginalName),
			zap.Int("tmdb_id", match.TMDbID),
			zap.Int("bangumi_id", match.BangumiID),
			zap.String("douban_id", match.DoubanID),
			zap.String("thetvdb_id", match.TheTVDBID))
	}
	return false
}

func mediaExternalIDSourceMatches(m *model.Media, match *Match, source string) bool {
	if m == nil || match == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "tmdb":
		return m.TMDbID > 0 && match.TMDbID == m.TMDbID
	case "bangumi":
		return m.BangumiID > 0 && match.BangumiID == m.BangumiID
	case "douban":
		return strings.TrimSpace(m.DoubanID) != "" &&
			strings.TrimSpace(match.DoubanID) == strings.TrimSpace(m.DoubanID)
	case "thetvdb":
		return strings.TrimSpace(m.TheTVDBID) != "" &&
			strings.TrimSpace(match.TheTVDBID) == strings.TrimSpace(m.TheTVDBID)
	default:
		return false
	}
}

func mediaPathHintMatchesExternalID(m *model.Media, lib *model.Library, match *Match, source string) bool {
	if m == nil || match == nil {
		return false
	}
	_, hints := pathHintMetadata(m.Path, mediaIsEpisodic(m, lib))
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "tmdb":
		return hints.TMDbID > 0 && hints.TMDbID == match.TMDbID
	case "bangumi":
		return hints.BangumiID > 0 && hints.BangumiID == match.BangumiID
	case "douban":
		return strings.TrimSpace(hints.DoubanID) != "" &&
			strings.TrimSpace(hints.DoubanID) == strings.TrimSpace(match.DoubanID)
	case "thetvdb":
		return strings.TrimSpace(hints.TheTVDBID) != "" &&
			strings.TrimSpace(hints.TheTVDBID) == strings.TrimSpace(match.TheTVDBID)
	default:
		return false
	}
}

func mediaExternalIDLanguageFallbackTrusted(m *model.Media, match *Match) bool {
	if m == nil || match == nil || containsCJK(match.Title) {
		return false
	}
	title := strings.TrimSpace(m.Title)
	return title != "" &&
		containsCJK(title) &&
		!unsafeAutomaticEpisodeQuery(title) &&
		!organizeMediaTitleLooksLikeRelease(title)
}

func preferExistingLocalizedEpisodeTitle(m *model.Media, lib *model.Library, match *Match) {
	if !mediaIsEpisodic(m, lib) || !mediaExternalIDLanguageFallbackTrusted(m, match) {
		return
	}
	title := strings.TrimSpace(m.Title)
	if strings.TrimSpace(match.OriginalName) == "" {
		match.OriginalName = strings.TrimSpace(match.Title)
	}
	match.Title = title
}

func (s *ScraperService) applyFanartArtwork(ctx context.Context, match *Match) {
	if s == nil || s.fanart == nil || !s.fanart.Enabled() || match == nil {
		return
	}
	apply := func(a *Artwork) {
		if a == nil {
			return
		}
		if a.Poster != "" {
			match.PosterURL = a.Poster
		}
		if a.Backdrop != "" {
			match.BackdropURL = a.Backdrop
		}
	}
	if match.TMDbID > 0 {
		if a, err := s.fanart.MovieArtwork(ctx, match.TMDbID); err == nil {
			apply(a)
		} else {
			s.log.Debug("fanart movie artwork failed", zap.Int("tmdb_id", match.TMDbID), zap.Error(err))
		}
	}
	if strings.TrimSpace(match.TheTVDBID) != "" {
		if a, err := s.fanart.TVArtwork(ctx, strings.TrimSpace(match.TheTVDBID)); err == nil {
			apply(a)
		} else {
			s.log.Debug("fanart tv artwork failed", zap.String("thetvdb_id", match.TheTVDBID), zap.Error(err))
		}
	}
}
