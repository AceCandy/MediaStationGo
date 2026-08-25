package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (o *OrganizerService) updateReclassifiedMediaRow(ctx context.Context, oldPath, newPath string, req organizeExistingReclassifyRequest) error {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return nil
	}
	updates := map[string]any{
		"path": newPath,
	}
	if strings.TrimSpace(req.TargetLibraryID) != "" {
		updates["library_id"] = strings.TrimSpace(req.TargetLibraryID)
	}
	if strings.TrimSpace(req.Title) != "" {
		updates["scan_title"] = strings.TrimSpace(req.Title)
	}
	if req.Year > 0 {
		updates["scan_year"] = req.Year
	}
	if normalizeOrganizeMediaType(req.MediaType) == "movie" {
		updates["season_num"] = 0
		updates["episode_num"] = 0
	} else if req.Season > 0 {
		updates["season_num"] = req.Season
		if req.Episode > 0 {
			updates["episode_num"] = req.Episode
		}
	} else if req.Episode > 0 {
		updates["episode_num"] = req.Episode
	}
	var metadataIDs []string
	if err := o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", oldPath).Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error; err != nil {
		return err
	}
	if err := o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", oldPath).Updates(updates).Error; err != nil {
		return err
	}
	o.repo.MediaView.RefreshMetadataIDs(ctx, metadataIDs...)
	return nil
}

func (o *OrganizerService) deleteMediaRowForPath(ctx context.Context, path string) {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return
	}
	var metadataIDs []string
	_ = o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", path).Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error
	if err := o.repo.DB.WithContext(ctx).Where("path = ?", path).Delete(&model.Media{}).Error; err == nil {
		o.repo.MediaView.RefreshMetadataIDs(ctx, metadataIDs...)
	}
}

func (o *OrganizerService) mediaPathExists(ctx context.Context, path string) bool {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return false
	}
	var count int64
	if err := o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", path).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}
