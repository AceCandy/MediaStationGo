package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// runDoubanDataRefresh 更新已绑定作品的共享资料，字段和图片由补齐任务处理。
func (s *ScraperService) runDoubanDataRefresh(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Douban == nil || s.douban == nil || s.douban.dataRepo == nil {
		return errors.New("douban data refresh dependencies unavailable")
	}
	ctx = withDoubanTaskRouteReset(ctx)
	ctx, count := withDoubanRequestStats(ctx)
	metrics := map[string]int64{}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "豆瓣资料刷新", TaskUpdate{Stage: "refresh", Message: "正在刷新到期的豆瓣资料与评分", Metrics: metrics})
	}
	finish := func(err error, message string) error {
		metrics["requested"] = count.Load()
		if task != nil {
			stage := "completed"
			if err != nil {
				stage = "failed"
			}
			task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: stage, Message: message, Metrics: metrics})
		}
		return err
	}
	now := time.Now().UTC()
	before := now.Add(-s.douban.detailTTL(ctx))
	afterID, afterKind := "", ""
	for {
		candidates, err := s.repo.Douban.ListRefreshCandidatesAfter(ctx, afterID, afterKind, before, now, 100)
		if err != nil {
			return finish(err, "豆瓣资料刷新失败")
		}
		for _, candidate := range candidates {
			metrics["scanned"]++
			_, degraded, err := s.douban.GetEnrichmentMatchByID(ctx, candidate.DoubanID, candidate.EntityKind)
			if errors.Is(err, ErrDoubanTemporarilyUnavailable) {
				metrics["failed"]++
				metrics["upstream_paused"] = 1
				return finish(err, "豆瓣接口异常，资料刷新已暂停，稍后重试")
			}
			if err != nil && !errors.Is(err, ErrDoubanSubjectNotFound) {
				return finish(err, "豆瓣资料刷新失败")
			}
			message := fmt.Sprintf("✅ 豆瓣 %s：资料已更新", candidate.DoubanID)
			if err != nil {
				metrics["failed"]++
				message = fmt.Sprintf("⚠️ 豆瓣 %s：条目不存在，已记录退避", candidate.DoubanID)
			} else if degraded {
				metrics["degraded"]++
				message = fmt.Sprintf("⚠️ 豆瓣 %s：完整接口受限，已获取降级资料", candidate.DoubanID)
			} else {
				metrics["updated"]++
			}
			metrics["requested"] = count.Load()
			if task != nil {
				task.Update(TaskUpdate{Stage: "refresh", Metrics: metrics, Details: []string{message}})
			}
			afterID, afterKind = candidate.DoubanID, candidate.EntityKind
		}
		if len(candidates) < 100 {
			break
		}
	}
	return finish(nil, "豆瓣资料刷新完成")
}
