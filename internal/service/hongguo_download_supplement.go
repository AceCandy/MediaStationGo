package service

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// HongGuoDownloadSupplementResult 区分实际新增作品、分集和未入队候选，不承诺下载完成。
type HongGuoDownloadSupplementResult struct {
	Requested  int `json:"requested"`
	Candidates int `json:"candidates"`
	Works      int `json:"works"`
	Episodes   int `json:"episodes"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

// Supplement 仅选取本地资料齐全且从未入队的源作品；不抓资料、不重试旧任务。
func (s *HongGuoDownloadService) Supplement(ctx context.Context, count int) (HongGuoDownloadSupplementResult, error) {
	result := HongGuoDownloadSupplementResult{Requested: count}
	if count < 1 || count > 100 {
		return result, errors.New("每次补充下载数量须为 1–100 部")
	}
	if !s.supplementMu.TryLock() {
		return result, ErrSchedulerJobAlreadyRunning
	}
	defer s.supplementMu.Unlock()
	enabled, err := s.catalog.Enabled(ctx)
	if err != nil {
		return result, errors.New("读取红果状态失败")
	}
	if !enabled {
		return result, ErrHongGuoDisabled
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return result, errors.New("读取下载配置失败")
	}
	if cfg.Root == "" {
		return result, errors.New("请先在下载空间设置存储目录")
	}
	var ids []string
	err = s.repo.DB.WithContext(ctx).Model(&model.HongGuoWork{}).Select("hongguo_works.source_id").
		Where("source_category <> ? AND source_id ~ ?", "comic", `^[1-9][0-9]{0,31}$`).
		Where("NOT EXISTS (SELECT 1 FROM hong_guo_download_works d WHERE d.source_id = hongguo_works.source_id)").
		Where("NOT EXISTS (SELECT 1 FROM hong_guo_downloads d WHERE d.source_id = hongguo_works.source_id)").
		Where("(SELECT COUNT(*) FROM hongguo_episodes e WHERE e.work_id = hongguo_works.id) BETWEEN 1 AND 10000").
		Where("NOT EXISTS (SELECT 1 FROM hongguo_episodes e WHERE e.work_id = hongguo_works.id AND (e.source_video_id IS NULL OR e.source_video_id !~ ?))", `^[1-9][0-9]{0,31}$`).
		Order("first_visible_at DESC NULLS LAST, created_at DESC, id DESC").Limit(count).Scan(&ids).Error
	if err != nil {
		return result, errors.New("读取待下载作品失败")
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
