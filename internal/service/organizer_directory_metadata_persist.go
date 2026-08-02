package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (o *OrganizerService) persistOrganizedSourceMetadata(ctx context.Context, plan organizeSourceFilePlan) {
	if o == nil || o.repo == nil || o.repo.Media == nil || plan.MetadataMatch == nil {
		return
	}
	libraryID := strings.TrimSpace(plan.TargetLibraryID)
	if libraryID == "" {
		return
	}
	media := organizedSourceMediaFromPlan(libraryID, plan)
	if info, err := os.Stat(plan.Target.Path); err == nil && !info.IsDir() {
		media.SizeBytes = info.Size()
	}
	if fileID, ok := fileIdentity(plan.Target.Path); ok {
		media.FileID = fileID
	}
	if err := o.repo.Media.Upsert(ctx, media); err != nil {
		if o.log != nil {
			o.log.Warn("persist organized metadata failed",
				zap.String("path", plan.Target.Path),
				zap.String("library_id", libraryID),
				zap.Error(err))
		}
		return
	}
	lib, err := o.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return
	}
	if err := o.persistOrganizerMatch(ctx, media, lib, plan.MetadataMatch); err != nil && o.log != nil {
		o.log.Warn("persist organized shared metadata failed",
			zap.String("media_id", media.ID),
			zap.String("library_id", libraryID),
			zap.Error(err))
	}
}

func organizedSourceMediaFromPlan(libraryID string, plan organizeSourceFilePlan) *model.Media {
	match := plan.MetadataMatch
	media := &model.Media{
		LibraryID:    libraryID,
		Title:        strings.TrimSpace(firstNonEmpty(match.Title, plan.Identity.ParsedTitle, plan.Identity.Title)),
		Year:         firstPositiveInt(match.Year, plan.Identity.Year),
		Path:         plan.Target.Path,
		Container:    strings.TrimPrefix(strings.ToLower(filepath.Ext(plan.Target.Path)), "."),
		SeasonNum:    plan.Identity.Season,
		EpisodeNum:   plan.Identity.Episode,
		TMDbID:       match.TMDbID,
		BangumiID:    match.BangumiID,
		DoubanID:     strings.TrimSpace(match.DoubanID),
		TheTVDBID:    strings.TrimSpace(match.TheTVDBID),
		ScrapeStatus: "pending",
	}
	if normalizeOrganizeMediaType(plan.Layout.MediaType) == "movie" {
		media.SeasonNum = 0
		media.EpisodeNum = 0
	}
	if media.EpisodeNum > 0 {
		media.SeriesID = localSeriesIdentity(media)
	}
	return media
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
