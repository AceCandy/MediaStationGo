package service

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func mediaYearHint(m *model.Media) int {
	if m == nil {
		return 0
	}
	if m.SeasonNum > 0 || m.EpisodeNum > 0 {
		return seriesPathYearHint(m.Path)
	}
	if season, episode := ParseEpisode(m.Path); season > 0 || episode > 0 {
		return seriesPathYearHint(m.Path)
	}
	if m.Year > 0 {
		return m.Year
	}
	if _, year := CleanQuery(filepath.Base(m.Path)); year > 0 {
		return year
	}
	return yearFromText(m.Path)
}

func seriesPathYearHint(path string) int {
	if showDir := showDirFromEpisodePath(path); showDir != "" {
		if _, year := CleanQuery(showDir); year > 0 {
			return year
		}
	}
	return 0
}

func yearFromText(raw string) int {
	if raw == "" {
		return 0
	}
	matches := yearPattern.FindStringSubmatch(strings.ToLower(raw))
	if len(matches) < 2 {
		return 0
	}
	year, _ := strconv.Atoi(matches[1])
	return year
}

func localAdultCode(local *LocalMetadata) string {
	if local == nil {
		return ""
	}
	return local.AdultCode
}

// mergeLocalMetadataIntoMatch 仅用本地信息补齐在线缺失字段，不覆盖在线有效值。
func mergeLocalMetadataIntoMatch(match *Match, local *LocalMetadata) {
	if match == nil || local == nil {
		return
	}
	if match.TMDbID > 0 && local.TMDbID > 0 && match.TMDbID != local.TMDbID {
		return
	}
	if local.PathHint {
		mergePathHintIDsIntoMatch(match, local)
		return
	}
	if match.Title == "" && local.Title != "" {
		match.Title = local.Title
	}
	if match.OriginalName == "" && local.OriginalName != "" {
		match.OriginalName = local.OriginalName
	}
	if match.OriginalName == "" && local.AdultCode != "" {
		match.OriginalName = local.AdultCode
		match.NSFW = true
	}
	if match.Overview == "" && local.Overview != "" {
		match.Overview = local.Overview
	}
	if match.PosterURL == "" && local.PosterURL != "" {
		match.PosterURL = local.PosterURL
	}
	if match.BackdropURL == "" && local.BackdropURL != "" {
		match.BackdropURL = local.BackdropURL
	}
	if match.Rating <= 0 && local.Rating > 0 {
		match.Rating = local.Rating
	}
	if match.Year <= 0 && local.Year > 0 {
		match.Year = local.Year
	}
	if match.ReleaseDate == "" && local.ReleaseDate != "" {
		match.ReleaseDate = local.ReleaseDate
	}
	if match.TMDbID <= 0 && local.TMDbID > 0 {
		match.TMDbID = local.TMDbID
	}
	if match.BangumiID <= 0 && local.BangumiID > 0 {
		match.BangumiID = local.BangumiID
	}
	if match.DoubanID == "" && local.DoubanID != "" {
		match.DoubanID = local.DoubanID
	}
	if match.TheTVDBID == "" && local.TheTVDBID != "" {
		match.TheTVDBID = local.TheTVDBID
	}
	if len(match.Genres) == 0 && local.Genres != "" {
		match.Genres = splitNFOList(local.Genres)
	}
	if len(match.Countries) == 0 && local.Countries != "" {
		match.Countries = splitNFOList(local.Countries)
	}
	if len(match.Languages) == 0 && local.Languages != "" {
		match.Languages = splitNFOList(local.Languages)
	}
	if local.NSFW {
		match.NSFW = true
	}
}

func mergePathHintIDsIntoMatch(match *Match, local *LocalMetadata) {
	if match == nil || local == nil {
		return
	}
	if match.TMDbID <= 0 && local.TMDbID > 0 {
		match.TMDbID = local.TMDbID
	}
	if match.BangumiID <= 0 && local.BangumiID > 0 {
		match.BangumiID = local.BangumiID
	}
	if match.DoubanID == "" && local.DoubanID != "" {
		match.DoubanID = local.DoubanID
	}
	if match.TheTVDBID == "" && local.TheTVDBID != "" {
		match.TheTVDBID = local.TheTVDBID
	}
	if match.Year <= 0 && local.Year > 0 {
		match.Year = local.Year
	}
}

func mergeScrapePathHintMetadata(dst, src *LocalMetadata) *LocalMetadata {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneLocalMetadata(src)
	}
	hasLocalMetadata := localMetadataMarksMatched(dst)
	if dst.Title == "" && src.Title != "" {
		dst.Title = src.Title
	}
	if dst.Year <= 0 && src.Year > 0 {
		dst.Year = src.Year
	}
	if dst.ReleaseDate == "" && src.ReleaseDate != "" {
		dst.ReleaseDate = src.ReleaseDate
	}
	if src.TMDbID > 0 {
		dst.TMDbID = src.TMDbID
	}
	if src.BangumiID > 0 {
		dst.BangumiID = src.BangumiID
	}
	if strings.TrimSpace(src.DoubanID) != "" {
		dst.DoubanID = strings.TrimSpace(src.DoubanID)
	}
	if strings.TrimSpace(src.TheTVDBID) != "" {
		dst.TheTVDBID = strings.TrimSpace(src.TheTVDBID)
	}
	if !hasLocalMetadata {
		dst.PathHint = dst.PathHint || src.PathHint
	}
	return dst
}

func cloneLocalMetadata(src *LocalMetadata) *LocalMetadata {
	if src == nil {
		return nil
	}
	copy := *src
	return &copy
}

func (s *ScraperService) applyLocalMetadataMatch(ctx context.Context, m *model.Media, local *LocalMetadata) error {
	next := *m
	applyLocalMetadata(&next, local)
	lib, err := s.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil {
		return err
	}
	persisted, err := s.persistLocalMetadata(ctx, m, lib, local)
	if err != nil {
		return s.markScrapeError(ctx, m.ID, err)
	}
	updates := map[string]any{
		"metadata_id":         persisted.Target.ID,
		"scrape_status":       "matched",
		"scrape_error":        "",
		"local_metadata_hint": "",
	}
	if m.SeasonNum > 0 || m.EpisodeNum > 0 {
		updates["season_num"] = m.SeasonNum
	}
	if m.EpisodeNum > 0 {
		updates["episode_num"] = m.EpisodeNum
	}
	if next.SeasonNum > 0 || next.EpisodeNum > 0 {
		updates["season_num"] = next.SeasonNum
	}
	if next.EpisodeNum > 0 {
		updates["episode_num"] = next.EpisodeNum
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("id = ?", m.ID).Updates(updates).Error; err != nil {
		return err
	}
	s.repo.MediaView.RefreshMetadataIDs(ctx, m.MetadataID, persisted.Target.ID)
	s.invalidateMediaCache(ctx)
	s.hub.Publish("scrape", map[string]any{
		"media_id": m.ID,
		"title":    persisted.Target.Title,
		"source":   "local_nfo",
	})
	return nil
}

func (s *ScraperService) invalidateMediaCache(ctx context.Context) {
	if s != nil && s.cache != nil {
		s.cache.DeletePrefix(ctx, "media:")
		s.cache.DeletePrefix(ctx, "stats:")
	}
}
