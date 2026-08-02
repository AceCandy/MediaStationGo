package service

import (
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type scanDerivedMetadata struct {
	Title        string
	ScrapeStatus string
	Year         int
	TMDbID       int
	BangumiID    int
	DoubanID     string
	TheTVDBID    string
	SeasonNum    int
	EpisodeNum   int
}

func cloudMetadataNeedsRefresh(existing existingCloudMedia, localMeta *LocalMetadata) bool {
	return existing.LocalMetadataHint != encodeLocalMetadataHint(localMeta)
}

func cloudPathHintNeedsRefresh(existing existingCloudMedia, localMeta *LocalMetadata) bool {
	if localMeta.TMDbID > 0 && existing.TMDbID != localMeta.TMDbID {
		return true
	}
	if localMeta.BangumiID > 0 && existing.BangumiID != localMeta.BangumiID {
		return true
	}
	if strings.TrimSpace(localMeta.DoubanID) != "" && strings.TrimSpace(existing.DoubanID) != strings.TrimSpace(localMeta.DoubanID) {
		return true
	}
	return strings.TrimSpace(localMeta.TheTVDBID) != "" && strings.TrimSpace(existing.TheTVDBID) != strings.TrimSpace(localMeta.TheTVDBID)
}

func cloudTrackMetadataMissing(existing existingCloudMedia) bool {
	return existing.DurationSec <= 0 ||
		existing.Width <= 0 ||
		existing.Height <= 0 ||
		strings.TrimSpace(existing.VideoCodec) == "" ||
		strings.TrimSpace(existing.AudioCodec) == ""
}

func localMetadataNeedsRefresh(existing existingLocalMedia, local *LocalMetadata) bool {
	if existing.LocalMetadataHint != encodeLocalMetadataHint(local) {
		return true
	}
	if local == nil {
		return false
	}
	if local.TMDbID > 0 && existing.TMDbID != local.TMDbID {
		return true
	}
	if local.BangumiID > 0 && existing.BangumiID != local.BangumiID {
		return true
	}
	if local.DoubanID != "" && strings.TrimSpace(existing.DoubanID) != strings.TrimSpace(local.DoubanID) {
		return true
	}
	if local.TheTVDBID != "" && strings.TrimSpace(existing.TheTVDBID) != strings.TrimSpace(local.TheTVDBID) {
		return true
	}
	if (local.SeasonNum > 0 || local.EpisodeNum > 0) && existing.SeasonNum != local.SeasonNum {
		return true
	}
	if local.EpisodeNum > 0 && existing.EpisodeNum != local.EpisodeNum {
		return true
	}
	return false
}

func cloudDerivedMetadataNeedsRefresh(existing existingCloudMedia, incoming *model.Media) bool {
	if incoming == nil {
		return false
	}
	return scanDerivedMetadataNeedsRefresh(scanDerivedMetadata{
		Title:        existing.Title,
		ScrapeStatus: existing.ScrapeStatus,
		Year:         existing.Year,
		TMDbID:       existing.TMDbID,
		BangumiID:    existing.BangumiID,
		DoubanID:     existing.DoubanID,
		TheTVDBID:    existing.TheTVDBID,
		SeasonNum:    existing.SeasonNum,
		EpisodeNum:   existing.EpisodeNum,
	}, incoming)
}

func localDerivedMetadataNeedsRefresh(existing existingLocalMedia, incoming *model.Media) bool {
	if incoming == nil {
		return false
	}
	if incoming.LibraryRootID != "" && incoming.LibraryRootID != existing.LibraryRootID {
		return true
	}
	if incoming.RelativePath != "" && incoming.RelativePath != existing.RelativePath {
		return true
	}
	return scanDerivedMetadataNeedsRefresh(scanDerivedMetadata{
		Title:        existing.Title,
		ScrapeStatus: existing.ScrapeStatus,
		Year:         existing.Year,
		TMDbID:       existing.TMDbID,
		BangumiID:    existing.BangumiID,
		DoubanID:     existing.DoubanID,
		TheTVDBID:    existing.TheTVDBID,
		SeasonNum:    existing.SeasonNum,
		EpisodeNum:   existing.EpisodeNum,
	}, incoming)
}

func scanDerivedMetadataNeedsRefresh(existing scanDerivedMetadata, incoming *model.Media) bool {
	status := strings.TrimSpace(existing.ScrapeStatus)
	enrichable := status == "" || status == "pending" || status == "no_match"
	if enrichable && strings.TrimSpace(incoming.Title) != "" && !strings.EqualFold(strings.TrimSpace(existing.Title), strings.TrimSpace(incoming.Title)) {
		return true
	}
	if enrichable && incoming.Year > 0 && existing.Year != incoming.Year {
		return true
	}
	if (incoming.SeasonNum > 0 || incoming.EpisodeNum > 0) && existing.SeasonNum != incoming.SeasonNum {
		return true
	}
	if incoming.EpisodeNum > 0 && existing.EpisodeNum != incoming.EpisodeNum {
		return true
	}
	if incoming.TMDbID > 0 && existing.TMDbID != incoming.TMDbID {
		return true
	}
	if incoming.BangumiID > 0 && existing.BangumiID != incoming.BangumiID {
		return true
	}
	if strings.TrimSpace(incoming.DoubanID) != "" && strings.TrimSpace(existing.DoubanID) != strings.TrimSpace(incoming.DoubanID) {
		return true
	}
	return strings.TrimSpace(incoming.TheTVDBID) != "" && strings.TrimSpace(existing.TheTVDBID) != strings.TrimSpace(incoming.TheTVDBID)
}

func cloudSeriesTitleFromMediaPath(mediaPath string) (string, int) {
	displayPath := strings.TrimSpace(mediaPath)
	if strings.HasPrefix(strings.ToLower(displayPath), "cloud://") {
		rest := strings.TrimPrefix(displayPath, "cloud://")
		if idx := strings.Index(rest, "/"); idx >= 0 {
			displayPath = rest[idx+1:]
		} else {
			return "", 0
		}
	}
	displayPath = strings.Trim(strings.ReplaceAll(displayPath, "\\", "/"), "/")
	if displayPath == "" {
		return "", 0
	}
	parts := strings.Split(displayPath, "/")
	if len(parts) < 2 {
		return "", 0
	}
	dirs := parts[:len(parts)-1]
	if len(dirs) == 0 {
		return "", 0
	}
	base := strings.TrimSpace(dirs[len(dirs)-1])
	usedSeasonFolder := false
	if _, ok := seasonFromDir(base); ok {
		usedSeasonFolder = true
		dirs = dirs[:len(dirs)-1]
		if len(dirs) == 0 {
			return "", 0
		}
		base = strings.TrimSpace(dirs[len(dirs)-1])
	}
	if base == "" || (!usedSeasonFolder && len(dirs) < 2) {
		return "", 0
	}
	title, year := CleanQuery(base)
	if title == "" {
		title = base
	}
	return strings.TrimSpace(title), year
}
