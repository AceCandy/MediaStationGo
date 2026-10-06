package service

import (
	"context"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// refreshWorkTask 在业务事务提交后更新展示；超时或写入失败不能改变下载结果。
func (s *HongGuoDownloadService) refreshWorkTask(ctx context.Context, sourceID string) {
	if s.tasks == nil || s.tasks.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	row, err := s.repo.HongGuo.RefreshHongGuoDownloadTask(ctx, sourceID)
	if err != nil {
		s.tasks.logError("refresh HongGuo download work task failed", err)
		return
	}
	if row != nil {
		s.tasks.publish(backgroundFromTaskExecution(*row))
	}
}

// recoverWorkTasks 接续未结束摘要，也覆盖队列已提交终态但摘要尚未更新的崩溃窗口。
func (s *HongGuoDownloadService) recoverWorkTasks(ctx context.Context) {
	if s.tasks == nil || s.tasks.repo == nil {
		return
	}
	var sources []string
	if err := s.repo.DB.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("kind = ? AND status IN ? AND source_path LIKE 'hongguo://%'", TaskKindHongGuoDownload, []string{TaskStatusRunning, TaskStatusInterrupted}).
		Pluck("source_path", &sources).Error; err != nil {
		s.tasks.logError("recover HongGuo download work tasks failed", err)
		return
	}
	for _, source := range sources {
		if ctx.Err() != nil {
			return
		}
		if id := strings.TrimPrefix(source, "hongguo://"); hongguo.ValidID(id) {
			s.refreshWorkTask(ctx, id)
		}
	}
}
