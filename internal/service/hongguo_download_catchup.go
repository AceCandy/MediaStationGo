package service

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// catchUp 补入已入队剧集的真实缺集；缺少下载行本身保留重试依据，包括刚完结的最后几集。
func (s *HongGuoDownloadService) catchUp(ctx context.Context, sourceID string, report func(string, error)) error {
	after, failed := "", false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ids []string
		q := s.repo.DB.WithContext(ctx).Model(&model.HongGuoWork{}).Select("hongguo_works.source_id").
			Joins("JOIN hongguo_download_works p ON p.source_id = hongguo_works.source_id").
			Where("hongguo_works.source_id > ? AND kind = 'series' AND source_category <> 'comic'", after).
			Where("EXISTS (SELECT 1 FROM hongguo_episodes e WHERE e.work_id = hongguo_works.id AND e.source_video_id ~ '^[1-9][0-9]{0,31}$' AND NOT EXISTS (SELECT 1 FROM hongguo_downloads d WHERE d.source_id = hongguo_works.source_id AND d.episode = e.number))")
		if sourceID != "" {
			q = q.Where("hongguo_works.source_id = ?", sourceID)
		}
		if err := q.Order("hongguo_works.source_id").Limit(100).Scan(&ids).Error; err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("读取红果待补集作品失败")
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			_, err := s.enqueueWork(ctx, id, false, true)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed = failed || err != nil
			safeErr := err
			if err != nil {
				safeErr = errors.New("补集入队失败，等待下轮刷新重试")
			}
			report(id, safeErr)
			after = id
		}
	}
	if failed {
		return errors.New("部分红果补集入队失败，等待下轮刷新重试")
	}
	return nil
}
