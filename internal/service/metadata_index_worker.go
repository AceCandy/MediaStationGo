package service

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

// StartMetadataIndexWorker 仅更新搜索索引和页面缓存，不复制来源资料。
func (s *ScraperService) StartMetadataIndexWorker(ctx context.Context) {
	s.backgroundWG.Add(1)
	go func() {
		defer s.backgroundWG.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.refreshPendingMetadataIndex(ctx); err != nil && ctx.Err() == nil && s.log != nil {
					s.log.Warn("metadata index refresh failed", zap.Error(sanitizeTaskLogError(err)))
				}
			}
		}
	}()
}

func (s *ScraperService) refreshPendingMetadataIndex(ctx context.Context) error {
	var jobs []model.MetadataIndexJob
	if err := s.repo.DB.WithContext(ctx).Where("next_at<=?", time.Now()).Order("next_at,metadata_id").Limit(100).Find(&jobs).Error; err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	s.invalidateMediaCache(ctx)
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.MetadataID)
	}
	// 一次刷新整批，由仓储去重共同的整剧；失败保留整批，完成仍逐条核对修订号。
	refreshErr := s.repo.MediaView.RefreshMetadataIDs(ctx, ids...)
	runErr := refreshErr
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		q := s.repo.DB.WithContext(ctx).Where("metadata_id=? AND revision=?", job.MetadataID, job.Revision)
		if refreshErr != nil {
			runErr = errors.Join(runErr, q.Model(&model.MetadataIndexJob{}).UpdateColumn("next_at", time.Now().Add(time.Minute)).Error)
		} else {
			runErr = errors.Join(runErr, q.Delete(&model.MetadataIndexJob{}).Error)
		}
	}
	return runErr
}
