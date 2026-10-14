package service

import (
	"context"
	"errors"
	"time"
)

const (
	TaskKindTMDbRank   = "tmdb_data_rank"
	TaskKindDoubanRank = "douban_data_rank"
)

var discoverRankTasks = []struct{ key, name, provider string }{
	{TaskKindTMDbRank, "TMDb 排行榜刷新", "tmdb"},
	{TaskKindDoubanRank, "豆瓣排行榜刷新", "douban"},
}

func (s *SchedulerService) runDiscoverRanks(ctx context.Context, kind, name, provider string) error {
	if s.discover == nil {
		return errors.New("发现服务不可用")
	}
	task := s.tasks.StartTriggeredIfKindIdle(kind, schedulerTaskTrigger(ctx), name, TaskUpdate{Message: "正在刷新榜单"})
	if task == nil {
		return errors.New("榜单任务已在运行或记录创建失败")
	}
	metrics := map[string]int64{"processed": 0, "updated": 0, "failed": 0, "items": 0}
	var runErr error
	defer func() {
		task.Finish(sanitizeTaskLogError(runErr), TaskUpdate{Message: "榜单刷新结束，摘要由资料定时任务补齐", Metrics: metrics})
	}()
	var config *APIConfigService
	if provider == "tmdb" && s.discover.tmdb != nil {
		config = s.discover.tmdb.apiConfig
	}
	if provider == "douban" && s.scraper != nil && s.scraper.douban != nil {
		config = s.scraper.douban.apiConfig
	}
	if config != nil {
		resolved, err := config.Resolve(ctx, provider)
		if err != nil {
			runErr = errors.New("来源配置读取失败")
			return runErr
		}
		if !resolved.Enabled {
			runErr = errors.New("来源已停用")
			return runErr
		}
	}
	if provider == "tmdb" && !s.discover.RankingsAvailable(ctx) {
		runErr = errors.New("TMDb 尚未配置")
		return runErr
	}
	for _, section := range DiscoverSections {
		if section.Provider != provider {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		items, _, err := s.discover.RankingPage(requestCtx, section.Key, 1, true, func(ctx context.Context) ([]ExternalMediaResult, error) {
			if provider == "tmdb" {
				return s.discover.TMDbSection(ctx, section.Key, 1)
			}
			if s.scraper == nil || s.scraper.douban == nil {
				return nil, errors.New("豆瓣服务不可用")
			}
			return s.scraper.douban.Discover(ctx, section.Key, 1)
		})
		cancel()
		metrics["processed"]++
		if err != nil {
			metrics["failed"]++
			runErr = errors.New("部分榜单刷新失败，已保留原榜单")
		} else {
			metrics["updated"]++
			metrics["items"] += int64(len(items))
		}
		message := section.Label + "刷新成功"
		if err != nil {
			message = section.Label + "刷新失败，保留原榜单"
		}
		task.Update(TaskUpdate{Message: message, Metrics: metrics})
		if ctx.Err() != nil {
			runErr = ctx.Err()
			return runErr
		}
	}
	return runErr
}
