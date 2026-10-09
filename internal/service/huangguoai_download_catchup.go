package service

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// catchUp 补入已入队剧集的真实缺集；缺少下载行本身保留重试依据，包括刚完结的最后几集。
func (s *HuangGuoAIDownloadService) catchUp(ctx context.Context, sourceID string, report func(string, error)) error {
	after, failed := "", false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ids []string
		q := s.repo.DB.WithContext(ctx).Model(&model.HuangGuoAIWork{}).Select("huangguoai_works.source_id").
			Joins("JOIN huangguoai_download_works p ON p.source_id = huangguoai_works.source_id").
			Where("huangguoai_works.source_id > ? AND kind = 'series' AND projection_error = '' AND source_category IN ('ai-duanju','ai-manju')", after).
			Where("EXISTS (SELECT 1 FROM huangguoai_episodes e WHERE e.work_id = huangguoai_works.id AND NOT EXISTS (SELECT 1 FROM huangguoai_downloads d WHERE d.source_id = huangguoai_works.source_id AND d.episode = e.number))")
		if sourceID != "" {
			q = q.Where("huangguoai_works.source_id = ?", sourceID)
		}
		if err := q.Order("huangguoai_works.source_id").Limit(100).Scan(&ids).Error; err != nil {
			return errors.New("读取黄果 AI 待补集作品失败")
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			_, err := s.Enqueue(ctx, id)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed = failed || err != nil
			report("补集 "+id, err)
			after = id
		}
	}
	if failed {
		return errors.New("部分黄果 AI 补集入队失败，等待下轮刷新重试")
	}
	return nil
}
