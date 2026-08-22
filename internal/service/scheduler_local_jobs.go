package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// jobScanLibraries re-walks every enabled library.
//
// 定时开关和周期由 SchedulerService 统一管理；手动调用始终执行。
func (s *SchedulerService) jobScanLibraries(ctx context.Context) error {
	trigger := schedulerTaskTrigger(ctx)
	name := "定时媒体库扫描"
	if trigger == TaskTriggerManual {
		name = "手动触发媒体库扫描"
	}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindScan, trigger, name, TaskUpdate{Stage: "scan", Message: "正在扫描已启用媒体库"})
		if task == nil {
			return errors.New("create scan task execution failed")
		}
	}
	libs, err := s.librariesForScanRun(ctx)
	if err != nil {
		if task != nil {
			safeErr := sanitizeTaskLogError(err)
			task.Finish(safeErr, TaskUpdate{Stage: "scan", Message: "媒体库扫描失败", Details: []string{"❌ 读取媒体库列表失败: " + safeErr.Error()}, DetailsWithoutLevel: true})
		}
		return err
	}
	metrics := map[string]int64{}
	for _, l := range libs {
		if !l.Enabled {
			continue
		}
		if isRetiredCloudPath(l.Path) {
			continue
		}
		metrics["libraries"]++
		res, err := s.scanner.ScanLibrary(ctx, l.ID)
		if err != nil {
			metrics["errors"]++
			if task != nil {
				task.Update(TaskUpdate{Stage: "scan", Metrics: metrics, Details: []string{fmt.Sprintf("❌ 媒体库 %s（%s）: 扫描失败: %v", l.Name, l.ID, sanitizeTaskLogError(err))}, DetailsWithoutLevel: true})
			}
			s.log.Warn("scheduled scan failed",
				zap.String("library", l.ID), zap.Error(err))
			continue
		}
		if res != nil {
			metrics["visited"] += int64(res.Visited)
			metrics["added"] += int64(res.Added)
			metrics["updated"] += int64(res.Updated)
			metrics["removed"] += res.Removed
		}
		if task != nil {
			task.Update(TaskUpdate{Stage: "scan", Metrics: metrics, Details: libraryScanTaskDetails(l, res), DetailsWithoutLevel: true})
		}
	}
	if task != nil {
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "媒体库扫描结束", Metrics: metrics})
	}
	return nil
}

func (s *SchedulerService) librariesForScanRun(ctx context.Context) ([]model.Library, error) {
	libraryID, _ := ctx.Value(schedulerLibraryScanIDKey{}).(string)
	if libraryID == "" {
		return s.repo.Library.List(ctx)
	}
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, errors.New("library not found")
	}
	return []model.Library{*lib}, nil
}

func libraryScanTaskDetail(l model.Library, res *ScanResult) string {
	if res == nil {
		return fmt.Sprintf("ℹ️ 媒体库 %s（%s）: 扫描完成，无结果统计", l.Name, l.ID)
	}
	return fmt.Sprintf("ℹ️ 媒体库 %s（%s）: 访问 %d，新增 %d，更新 %d，移除 %d，跳过 %d，错误 %d", l.Name, l.ID, res.Visited, res.Added, res.Updated, res.Removed, res.Skipped, res.ErrorCount)
}

func libraryScanTaskDetails(l model.Library, res *ScanResult) []string {
	details := []string{libraryScanTaskDetail(l, res)}
	return append(details, res.ChangeDetails()...)
}

// jobOrganizeSource periodically organizes the configured staging/download
// source directory into the configured media destination. It is intentionally
// opt-in: manual file management remains available, but background disk walking
// only starts after the operator enables organize.auto.
func (s *SchedulerService) jobOrganizeSource(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	if s.organizer == nil {
		return nil
	}
	taskName := "自动整理重命名刮削入库"
	trigger := OrganizeTriggerScheduled
	if manual {
		taskName = "手动触发自动整理重命名刮削入库"
		trigger = OrganizeTriggerManual
	}
	resWrap, err := s.ensureOrganizePipeline().Run(ctx, OrganizePipelineRequest{
		Scope:    OrganizeScopeDirectory,
		Trigger:  trigger,
		TaskName: taskName,
	})
	if err != nil {
		return err
	}
	res := resWrap.Result
	if res == nil {
		res = &OrganizeResult{}
	}
	if s.log != nil && res != nil {
		s.log.Info("scheduled source organize finished",
			zap.String("source", res.SourcePath),
			zap.String("dest", res.DestPath),
			zap.Int("organized", res.Organized),
			zap.Int("replaced", res.Replaced),
			zap.Int("skipped", res.Skipped),
			zap.Int("scrapes", len(res.Scrapes)),
			zap.Int("errors", len(res.Errors)),
		)
	}
	return nil
}

func (s *SchedulerService) ensureOrganizePipeline() *OrganizePipelineService {
	if s.organizePipeline != nil {
		return s.organizePipeline
	}
	return NewOrganizePipelineService(s.log, s.repo, s.organizer, s.scanner, s.tasks)
}

func (s *SchedulerService) jobPeopleBackfill(ctx context.Context) error {
	if s.scraper == nil {
		return nil
	}
	return s.scraper.runPeopleBackfillPass(ctx, schedulerTaskTrigger(ctx))
}

func (s *SchedulerService) jobPeopleTranslation(ctx context.Context) error {
	if s.scraper == nil {
		return nil
	}
	return s.scraper.translatePendingPeople(ctx, schedulerTaskTrigger(ctx))
}

func (s *SchedulerService) jobAccountCleanup(ctx context.Context) error {
	if s.device == nil {
		return nil
	}
	if s.tasks == nil {
		return errors.New("task tracker unavailable")
	}
	metrics := map[string]int64{"removed": 0}
	task := s.tasks.StartTriggered(TaskKindCleanup, schedulerTaskTrigger(ctx), "账号清理巡检", TaskUpdate{
		Stage: "cleanup", Message: "账号清理巡检已启动", Metrics: metrics,
	})
	if task == nil {
		return errors.New("create account cleanup task execution failed")
	}
	removed, err := s.device.SweepAccountCleanup(ctx)
	metrics["removed"] = int64(removed)
	if err != nil {
		safeErr := sanitizeTaskLogError(err)
		task.Finish(safeErr, TaskUpdate{Stage: "cleanup", Message: "账号清理巡检失败", Metrics: metrics})
		return err
	}
	detail := "ℹ️ 账号清理巡检完成，未删除账号"
	if removed > 0 {
		detail = fmt.Sprintf("🗑️ 账号清理巡检删除 %d 个账号", removed)
	}
	task.Finish(nil, TaskUpdate{Stage: "completed", Message: "账号清理巡检完成", Metrics: metrics, Details: []string{detail}})
	return nil
}

func schedulerTaskTrigger(ctx context.Context) string {
	if manual, _ := ctx.Value(schedulerManualRunKey{}).(bool); manual {
		return TaskTriggerManual
	}
	return TaskTriggerScheduled
}

// isMissingTableErr lets the test harness ignore "no such table" errors
// that show up before AutoMigrate has run.
func isMissingTableErr(err error) bool {
	if err == nil {
		return false
	}
	return err == gorm.ErrInvalidDB
}
