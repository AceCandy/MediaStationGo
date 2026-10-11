package service

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

const TaskKindTMDbDataRefresh = "tmdb_data_refresh"
const TaskKindTMDbCenterMigration = "tmdb_center_migration"
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
func (s *SchedulerService) jobTMDbCenterMigration(ctx context.Context) error {
	if s.tmdbCenter == nil {
		return errors.New("TMDb center unavailable")
	}
	return s.tmdbCenter.Migrate(ctx, s.tasks, schedulerTaskTrigger(ctx))
}

func (s *TMDbDataCenter) Migrate(ctx context.Context, tasks *TaskTrackerService, trigger string) error {
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()
	const key = "metadata.tmdb_center_migration_v1"
	state, err := s.repo.Setting.Get(ctx, key)
	if err != nil {
		return err
	}
	if state == "complete" {
		return nil
	}
	var task *TaskHandle
	if tasks != nil {
		task = tasks.StartTriggered(TaskKindTMDbCenterMigration, trigger, "TMDb 历史资料迁移", TaskUpdate{Stage: "migration", Message: "离线导入旧快照、人物与图片引用"})
	}
	var processed, failed int64
	cursor := ""
	for {
		next, n, bad, e := s.repo.TMDb.MigrateLegacyBatch(ctx, cursor, 100)
		processed += int64(n)
		failed += int64(len(bad))
		if task != nil {
			task.Update(TaskUpdate{Stage: "migration", Message: "导入旧来源快照", Metrics: map[string]int64{"processed": processed, "failed": failed}})
			for _, id := range bad {
				task.Update(TaskUpdate{Message: "❌ 无法迁移来源资料：" + id})
			}
		}
		if e != nil {
			err = e
			break
		}
		if next == "" {
			break
		}
		cursor = next
	}

	if err == nil {
		cursor = ""
		for {
			next, n, bad, e := s.repo.TMDb.SeedLegacySubjects(ctx, cursor, 100)
			processed += int64(n)
			failed += int64(len(bad))
			if task != nil {
				for _, id := range bad {
					task.Update(TaskUpdate{Message: "❌ 无法迁移来源身份：" + id})
				}
			}
			if e != nil {
				err = e
				break
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	if err == nil {
		cursor = ""
		for {
			next, _, e := s.repo.TMDb.SeedLegacyPeople(ctx, cursor, 100)
			if e != nil {
				err = e
				break
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	if err == nil {
		err = s.repo.TMDb.ReuseLegacyArtwork(ctx)
	}

	if err == nil {
		base, e := url.Parse(s.provider.resolveBaseURL(ctx))
		err = e
		if e == nil {
			configKey := tmdbDigest(base.Scheme + "://" + base.Host + base.Path)
			cursor = ""
			for {
				next, e := s.repo.TMDb.SeedMaintenanceJobs(ctx, configKey, cursor, 100)
				if e != nil {
					err = e
					break
				}
				if next == "" {
					break
				}
				cursor = next
			}
		}
	}
	if err == nil && failed > 0 {
		err = errors.New("部分历史资料身份或父链无效，旧快照已保留")
	}
	if err == nil {
		err = s.repo.Setting.Set(ctx, key, "complete")
	}
	if task != nil {
		stage := "completed"
		if err != nil {
			stage = "failed"
		}
		task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: stage, Message: "历史资料迁移结束", Metrics: map[string]int64{"processed": processed, "failed": failed}})
	}
	return err
}
