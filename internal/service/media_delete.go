package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Delete 永久删除媒体数据库记录，不删除磁盘上的媒体文件。
func (s *MediaService) Delete(ctx context.Context, id string) error {
	err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}
