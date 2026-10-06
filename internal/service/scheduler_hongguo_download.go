package service

import (
	"context"
	"errors"
	"fmt"
)

const TaskKindHongGuoSupplement = "hongguo_download_supplement"
const TaskKindHuangGuoAISupplement = "huangguoai_download_supplement"

type downloadSupplementCountContextKey struct{}

// RunHongGuoSupplementNowAsync 仅覆盖本轮数量，不修改已保存的周期参数。
func (s *SchedulerService) RunHongGuoSupplementNowAsync(ctx context.Context, count int) error {
	return s.RunDownloadSupplementNowAsync(ctx, TaskKindHongGuoSupplement, count)
}

// RunDownloadSupplementNowAsync 仅覆盖指定来源本轮数量，不修改周期参数。
func (s *SchedulerService) RunDownloadSupplementNowAsync(ctx context.Context, kind string, count int) error {
	if kind != TaskKindHongGuoSupplement && kind != TaskKindHuangGuoAISupplement {
		return ErrSchedulerJobNotFound
	}
	if count < 1 || count > 100 {
		return ErrSchedulerCountInvalid
	}
	return s.runNowAsync(context.WithValue(ctx, downloadSupplementCountContextKey{}, count), kind)
}

func (s *SchedulerService) jobHongGuoSupplement(ctx context.Context) error {
	return s.runDownloadSupplement(ctx, TaskKindHongGuoSupplement, "红果补充下载", s.hongguoDownloads.Supplement)
}

func (s *SchedulerService) jobHuangGuoAISupplement(ctx context.Context) error {
	return s.runDownloadSupplement(ctx, TaskKindHuangGuoAISupplement, "黄果 AI 补充下载", s.huangguoDownloads.Supplement)
}

// runDownloadSupplement 为两来源记录相同的实际入队指标，队列规则仍由各来源服务负责。
func (s *SchedulerService) runDownloadSupplement(ctx context.Context, kind, name string, supplement func(context.Context, int) (DownloadSupplementResult, error)) error {
	count, ok := ctx.Value(downloadSupplementCountContextKey{}).(int)
	if !ok {
		s.mu.Lock()
		count = s.jobByNameLocked(kind).count
		s.mu.Unlock()
	}
	if s.tasks == nil {
		return errors.New("任务记录服务不可用")
	}
	task := s.tasks.StartTriggered(kind, schedulerTaskTrigger(ctx), name, TaskUpdate{Stage: "enqueue", Message: fmt.Sprintf("准备补充 %d 部作品", count)})
	if task == nil {
		return errors.New("创建补充下载执行记录失败")
	}
	result, err := supplement(ctx, count)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && result.Failed > 0 {
		err = errors.New("部分作品入队失败，请查看本轮结果")
	}
	task.Finish(err, TaskUpdate{Stage: "enqueue", Message: fmt.Sprintf("本轮请求 %d 部，找到 %d 部；已入队 %d 部/%d 集，跳过 %d 部，失败 %d 部（入队不代表下载完成）", count, result.Candidates, result.Works, result.Episodes, result.Skipped, result.Failed), Metrics: map[string]int64{"requested": int64(count), "candidates": int64(result.Candidates), "works": int64(result.Works), "episodes": int64(result.Episodes), "skipped": int64(result.Skipped), "failed": int64(result.Failed)}})
	return err
}
