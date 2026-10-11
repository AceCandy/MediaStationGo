package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const doubanDataRefreshCountKey = "metadata.douban_data_refresh_count"
const doubanDataRefreshDefaultCount = 100

// doubanDataRefreshLimit 与调度设置共用数量规则，不改变启用状态和周期。
func (s *ScraperService) doubanDataRefreshLimit(ctx context.Context) (int, error) {
	value, err := s.repo.Setting.Get(ctx, doubanDataRefreshCountKey)
	if err != nil {
		return 0, err
	}
	if count, err := strconv.Atoi(value); err == nil && count >= 1 && count <= 100 {
		return count, nil
	}
	return doubanDataRefreshDefaultCount, nil
}

// runDoubanDataRefresh 限量更新到期基础资料，不请求网页、人物或图片。
func (s *ScraperService) runDoubanDataRefresh(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Douban == nil || s.douban == nil || s.douban.dataRepo == nil {
		return errors.New("douban data refresh dependencies unavailable")
	}
	ctx = withDoubanTaskRouteReset(ctx)
	ctx, requests := withDoubanRequestStats(ctx)
	metrics := map[string]int64{"total": 0, "selected": 0, "processed": 0, "updated": 0, "changed": 0, "unchanged": 0, "skipped": 0, "failed": 0, "remaining": 0, "requested": 0}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindDoubanDataRefresh, trigger, "豆瓣资料刷新", TaskUpdate{Stage: "refresh", Message: "正在限量刷新到期的豆瓣基础资料与评分（仅 API）", Metrics: metrics})
	}
	now := time.Now().UTC()
	before := now.Add(-s.douban.detailTTL(ctx))
	finish := func(err error, message string) error {
		metrics["requested"] = requests.Load()
		if ctx.Err() == nil {
			remaining, countErr := s.repo.Douban.CountRefreshCandidates(ctx, before, now)
			if countErr == nil {
				metrics["remaining"] = remaining
			} else if err == nil {
				err, message = countErr, "豆瓣基础资料刷新统计失败"
			}
		}
		if task != nil {
			stage := "completed"
			if err != nil {
				stage = "failed"
			}
			task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: stage, Message: message, Metrics: metrics})
		}
		return err
	}
	limit, err := s.doubanDataRefreshLimit(ctx)
	if err != nil {
		return finish(err, "豆瓣刷新配置读取失败")
	}
	metrics["limit"] = int64(limit)
	total, err := s.repo.Douban.CountRefreshCandidates(ctx, before, now)
	if err != nil {
		return finish(err, "豆瓣基础资料刷新失败")
	}
	metrics["total"], metrics["remaining"] = total, total
	candidates, err := s.repo.Douban.ListRefreshCandidates(ctx, before, now, limit)
	if err != nil {
		return finish(err, "豆瓣基础资料刷新失败")
	}
	metrics["selected"] = int64(len(candidates))
	for _, candidate := range candidates {
		metrics["processed"]++
		metrics["scanned"]++
		outcome, err := s.douban.refreshBaseDetail(ctx, candidate.DoubanID, candidate.EntityKind)
		if errors.Is(err, ErrDoubanTemporarilyUnavailable) {
			metrics["failed"]++
			metrics["upstream_paused"] = 1
			return finish(err, "豆瓣基础接口异常，已保留旧资料并暂停本轮刷新")
		}
		if err != nil && !errors.Is(err, ErrDoubanSubjectNotFound) {
			metrics["failed"]++
			return finish(err, "豆瓣基础资料刷新失败")
		}
		message := fmt.Sprintf("✅ 豆瓣 %s：基础资料已更新", candidate.DoubanID)
		switch {
		case err != nil:
			metrics["failed"]++
			message = fmt.Sprintf("⚠️ 豆瓣 %s：条目不存在，已记录退避", candidate.DoubanID)
		case outcome.Cached || outcome.Deferred || !outcome.Saved:
			metrics["skipped"]++
			message = fmt.Sprintf("⏭️ 豆瓣 %s：已有更新资料，本次跳过", candidate.DoubanID)
			if outcome.Deferred {
				message = fmt.Sprintf("⏭️ 豆瓣 %s：已进入失败退避，本次跳过", candidate.DoubanID)
			}
		default:
			metrics["updated"]++
			if outcome.Changed {
				metrics["changed"]++
			} else {
				metrics["unchanged"]++
				message = fmt.Sprintf("✅ 豆瓣 %s：基础资料未变化，已更新检查时间", candidate.DoubanID)
			}
		}
		metrics["requested"] = requests.Load()
		if task != nil {
			task.Update(TaskUpdate{Stage: "refresh", Metrics: metrics, Details: []string{message}})
		}
	}
	if len(candidates) == 0 {
		return finish(nil, "没有到期的豆瓣基础资料，未请求接口")
	}
	return finish(nil, "豆瓣基础资料本轮刷新完成，未抓取网页补充")
}
