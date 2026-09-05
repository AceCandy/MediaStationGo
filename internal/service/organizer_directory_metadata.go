package service

import (
	"context"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (o *OrganizerService) lookupOrganizeMetadata(ctx context.Context, src, sourceRoot, mediaType, title string, year, season, episode int) *Match {
	normalizedType := normalizeOrganizeMediaType(mediaType)
	lookupSeason, lookupEpisode := season, episode
	if normalizedType == "movie" {
		lookupSeason, lookupEpisode = 0, 0
	}
	seriesLike := isSeriesLibraryType(mediaType) || (normalizedType != "movie" && (season > 0 || episode > 0))
	if local, err := ReadLocalMetadata(src, sourceRoot, seriesLike); err == nil && local != nil {
		if match := organizeMatchFromLocalMetadata(local); match != nil {
			return match
		}
	} else if err != nil && o.log != nil {
		o.log.Debug("organize read local metadata before rename failed", zap.String("path", src), zap.Error(err))
	}
	if match := o.lookupOrganizeAdultMetadata(ctx, src, mediaType, title); match != nil {
		return match
	}
	if o == nil || o.scraper == nil || !o.scraper.AnyEnabled() {
		return nil
	}
	return o.lookupOrganizeMetadataByPathHints(ctx, src, sourceRoot, normalizedType, title, year, lookupSeason, lookupEpisode, seriesLike)
}

func (o *OrganizerService) lookupOrganizeMetadataByPathHints(ctx context.Context, src, sourceRoot, mediaType, title string, year, season, episode int, seriesLike bool) *Match {
	if o == nil || o.scraper == nil {
		return nil
	}
	meta, hints := pathHintMetadata(src, seriesLike)
	if !hints.useful() {
		return nil
	}
	if meta != nil {
		if strings.TrimSpace(title) == "" {
			title = strings.TrimSpace(meta.Title)
		}
		if year <= 0 {
			year = meta.Year
		}
	}
	libType := normalizeOrganizeMediaType(mediaType)
	if libType == "" {
		libType = organizeLibraryModelType(mediaType)
	}
	lib := &model.Library{Path: sourceRoot, Type: libType, Enabled: true}
	media := &model.Media{
		Title:      title,
		Year:       year,
		Path:       src,
		SeasonNum:  season,
		EpisodeNum: episode,
		TMDbID:     hints.TMDbID,
		BangumiID:  hints.BangumiID,
		DoubanID:   strings.TrimSpace(hints.DoubanID),
		TheTVDBID:  strings.TrimSpace(hints.TheTVDBID),
	}
	match := o.scraper.matchFromMediaExternalIDs(ctx, media, lib)
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return nil
	}
	if o.log != nil {
		o.log.Info("organize metadata matched by path id before rename",
			zap.String("source", src),
			zap.String("title", match.Title),
			zap.String("media_type", match.MediaType),
			zap.Int("tmdb_id", match.TMDbID),
			zap.Int("bangumi_id", match.BangumiID),
			zap.String("douban_id", match.DoubanID),
			zap.String("thetvdb_id", match.TheTVDBID))
	}
	return match
}

func (o *OrganizerService) lookupOrganizeAdultMetadata(ctx context.Context, src, mediaType, title string) *Match {
	if o == nil || o.scraper == nil || o.scraper.adult == nil || !o.scraper.adult.Enabled() || normalizeOrganizeMediaType(mediaType) != "adult" {
		return nil
	}
	candidates := []string{src, filepath.Base(src), title}
	outCodes := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		code := normalizeAdultCode(candidate)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		outCodes = append(outCodes, code)
	}
	if len(outCodes) == 0 {
		return nil
	}
	for _, code := range outCodes {
		match, err := o.scraper.adult.Search(ctx, code)
		if err != nil {
			if o.log != nil {
				o.log.Debug("organize adult metadata search failed", zap.String("source", src), zap.String("code", code), zap.Error(err))
			}
			continue
		}
		if match != nil && strings.TrimSpace(match.Title) != "" {
			if o.log != nil {
				o.log.Info("organize adult metadata matched before rename",
					zap.String("source", src),
					zap.String("code", code),
					zap.String("title", match.Title))
			}
			return match
		}
	}
	return nil
}
