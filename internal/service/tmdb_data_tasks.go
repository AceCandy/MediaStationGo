package service

import (
	"context"
	"errors"
	"strconv"
)

const TaskKindTMDbDataRefresh = "tmdb_data_refresh"
const TaskKindTMDbDataSupplement = "tmdb_data_supplement"
const tmdbDataRefreshCountKey = "metadata.tmdb_data_refresh_count"

func (s *SchedulerService) jobTMDbDataRefresh(ctx context.Context) error {
	if s.tmdbCenter == nil {
		return errors.New("TMDb center unavailable")
	}
	value, err := s.repo.Setting.Get(ctx, tmdbDataRefreshCountKey)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(value)
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindTMDbDataRefresh, schedulerTaskTrigger(ctx), "TMDb 资料刷新", TaskUpdate{Stage: "refresh", Message: "按到期组件刷新来源资料", Metrics: map[string]int64{"limit": int64(limit)}})
	}
	metrics := map[string]int64{"limit": int64(limit)}
	attempts, updated, err := s.tmdbCenter.RunDue(ctx, true, limit, metrics)
	metrics["requested"], metrics["updated"] = attempts, int64(updated)
	if task != nil {
		stage := "completed"
		if err != nil {
			stage = "failed"
		}
		task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: stage, Message: "本轮来源刷新结束；剩余到期项下轮继续", Metrics: metrics})
	}
	return err
}
