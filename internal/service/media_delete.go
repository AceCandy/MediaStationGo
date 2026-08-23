package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Delete 永久删除媒体数据库记录，不删除磁盘上的媒体文件。
func (s *MediaService) Delete(ctx context.Context, id string) error {
	var metadataIDs []string
	if err := s.repo.DB.WithContext(ctx).Unscoped().Model(&model.Media{}).Where("id = ?", id).Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error; err != nil {
		return err
	}
	err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
	if err == nil {
		s.repo.MediaView.RefreshMetadataIDs(ctx, metadataIDs...)
		s.invalidateMediaCache(ctx)
	}
	return err
}
