package service

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Supplement 按首次本地发现时间选取新作品；只使用已确认资料，不抓资料或重试旧任务。
func (s *HuangGuoAIDownloadService) Supplement(ctx context.Context, count int) (DownloadSupplementResult, error) {
	result := DownloadSupplementResult{Requested: count}
	if count < 1 || count > 100 {
		return result, ErrSchedulerCountInvalid
	}
	if !s.supplementMu.TryLock() {
		return result, ErrSchedulerJobAlreadyRunning
	}
	defer s.supplementMu.Unlock()
	enabled, err := s.catalog.Enabled(ctx)
	if err != nil {
		return result, errors.New("读取黄果 AI 状态失败")
	}
	if !enabled {
		return result, ErrHuangGuoAIDisabled
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return result, errors.New("读取黄果 AI 下载配置失败")
	}
	if cfg.Root == "" {
		return result, errors.New("请先设置黄果 AI 下载目录")
	}
	var ids []string
	err = s.repo.DB.WithContext(ctx).Model(&model.HuangGuoAIWork{}).Select("huangguoai_works.source_id").
		Joins("LEFT JOIN huangguoai_discoveries d ON d.source_id = huangguoai_works.source_id").
		Where("huangguoai_works.source_id ~ ? AND projection_error = ''", `^[1-9][0-9]{0,31}$`).
		Where("(kind = 'series' AND huangguoai_works.source_category IN ('ai-duanju','ai-manju')) OR (kind = 'movie' AND huangguoai_works.source_category IN ('ai-huanlian','ai-mogai'))").
		Where("NOT EXISTS (SELECT 1 FROM huangguoai_download_works q WHERE q.source_id = huangguoai_works.source_id)").
		Where("NOT EXISTS (SELECT 1 FROM huangguoai_downloads q WHERE q.source_id = huangguoai_works.source_id)").
		Where("(SELECT COUNT(*) FROM huangguoai_episodes e WHERE e.work_id = huangguoai_works.id) BETWEEN 1 AND 10000").
		Where("kind <> 'movie' OR NOT EXISTS (SELECT 1 FROM huangguoai_episodes e WHERE e.work_id = huangguoai_works.id AND e.number <> 1)").
		Order("COALESCE(d.created_at, huangguoai_works.created_at) DESC, huangguoai_works.created_at DESC, huangguoai_works.id DESC").Limit(count).Scan(&ids).Error
	if err != nil {
		return result, errors.New("读取黄果 AI 待下载作品失败")
	}
	result.Candidates = len(ids)
	for _, id := range ids {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		added, err := s.enqueue(ctx, id, true)
		if err != nil {
			result.Failed++
			continue
		}
		if added == 0 {
			result.Skipped++
			continue
		}
		result.Works++
		result.Episodes += added
	}
	return result, nil
}
