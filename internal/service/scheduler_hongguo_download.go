package service

import (
	"context"
	"errors"
	"fmt"
)

const TaskKindHongGuoSupplement = "hongguo_download_supplement"
const hongGuoSupplementCountKey = "hongguo.download_supplement.count"

type hongGuoSupplementCountContextKey struct{}

// RunHongGuoSupplementNowAsync 仅覆盖本轮数量，不修改已保存的周期参数。
func (s *SchedulerService) RunHongGuoSupplementNowAsync(ctx context.Context, count int) error {
	if count < 1 || count > 100 {
		return ErrSchedulerCountInvalid
	}
	return s.runNowAsync(context.WithValue(ctx, hongGuoSupplementCountContextKey{}, count), TaskKindHongGuoSupplement)
}

func (s *SchedulerService) jobHongGuoSupplement(ctx context.Context) error {
	count, ok := ctx.Value(hongGuoSupplementCountContextKey{}).(int)
	if !ok {
		s.mu.Lock()
		count = s.jobByNameLocked(TaskKindHongGuoSupplement).count
		s.mu.Unlock()
	}
	if s.tasks == nil {
		return errors.New("任务记录服务不可用")
	}
	task := s.tasks.StartTriggered(TaskKindHongGuoSupplement, schedulerTaskTrigger(ctx), "红果补充下载", TaskUpdate{Stage: "enqueue", Message: fmt.Sprintf("准备补充 %d 部作品", count)})
	if task == nil {
		return errors.New("创建补充下载执行记录失败")
	}
	result, err := s.hongguoDownloads.Supplement(ctx, count)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && result.Failed > 0 {
		err = errors.New("部分作品入队失败，请查看本轮结果")
	}
	task.Finish(err, TaskUpdate{Stage: "enqueue", Message: fmt.Sprintf("本轮请求 %d 部，找到 %d 部；已入队 %d 部/%d 集，跳过 %d 部，失败 %d 部（入队不代表下载完成）", count, result.Candidates, result.Works, result.Episodes, result.Skipped, result.Failed), Metrics: map[string]int64{"requested": int64(count), "candidates": int64(result.Candidates), "works": int64(result.Works), "episodes": int64(result.Episodes), "skipped": int64(result.Skipped), "failed": int64(result.Failed)}})
	return err
}
