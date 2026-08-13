package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// jobScanLibraries re-walks every enabled library.
//
// 默认关闭：文件变更由 WatcherService 增量入库，无需周期性全量重扫。
// 仅当用户在设置中显式开启 scan.periodic_enabled 时才执行整库重扫，
// 避免对硬盘的高频反复读取造成损伤（用户明确要求）。
func (s *SchedulerService) jobScanLibraries(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	now := s.currentTime()
	if !manual && !s.periodicScanDue(ctx, now) {
		return nil
	}
	trigger := TaskTriggerScheduled
	name := "定时媒体库扫描"
	if manual {
		trigger = TaskTriggerManual
		name = "手动触发媒体库扫描"
	}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindScan, trigger, name, TaskUpdate{Stage: "scan", Message: "正在扫描已启用媒体库"})
		if task == nil {
			return errors.New("create scan task execution failed")
		}
	}
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		if task != nil {
			safeErr := sanitizeTaskLogError(err)
			task.Finish(safeErr, TaskUpdate{Stage: "scan", Message: "媒体库扫描失败", Details: []string{"读取媒体库列表失败: " + safeErr.Error()}})
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
				task.Update(TaskUpdate{Stage: "scan", Message: "正在扫描已启用媒体库", Metrics: metrics, Details: []string{fmt.Sprintf("媒体库 %s（%s）: 扫描失败: %v", l.Name, l.ID, sanitizeTaskLogError(err))}})
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
			task.Update(TaskUpdate{Stage: "scan", Message: "正在扫描已启用媒体库", Metrics: metrics, Details: []string{libraryScanTaskDetail(l, res)}})
		}
	}
	if !manual {
		_ = s.markPeriodicScanCompleted(ctx, now)
	}
	if task != nil {
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "媒体库扫描结束", Metrics: metrics})
	}
	return nil
}

func libraryScanTaskDetail(l model.Library, res *ScanResult) string {
	if res == nil {
		return fmt.Sprintf("媒体库 %s（%s）: 扫描完成，无结果统计", l.Name, l.ID)
	}
	return fmt.Sprintf("媒体库 %s（%s）: 访问 %d，新增 %d，更新 %d，移除 %d，跳过 %d，错误 %d", l.Name, l.ID, res.Visited, res.Added, res.Updated, res.Removed, res.Skipped, res.ErrorCount)
}

// periodicScanEnabled reports whether the operator opted into periodic full
// library re-scans. Defaults to false so the incremental watcher is the only
// thing touching the disk under normal operation.
func (s *SchedulerService) periodicScanEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, "scan.periodic_enabled")
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

func (s *SchedulerService) periodicScanDue(ctx context.Context, now time.Time) bool {
	if !s.periodicScanEnabled(ctx) {
		return false
	}
	if s.repo == nil || s.repo.Setting == nil {
		return true
	}
	last, err := s.repo.Setting.Get(ctx, localLastPeriodicScanDateKey)
	if err != nil {
		return true
	}
	return strings.TrimSpace(last) != now.In(time.Local).Format(periodicScanDateFormat)
}

func (s *SchedulerService) markPeriodicScanCompleted(ctx context.Context, now time.Time) error {
	if s.repo == nil || s.repo.Setting == nil {
		return nil
	}
	return s.repo.Setting.Set(ctx, localLastPeriodicScanDateKey, now.In(time.Local).Format(periodicScanDateFormat))
}

// jobOrganizeSource periodically organizes the configured staging/download
// source directory into the configured media destination. It is intentionally
// opt-in: manual file management remains available, but background disk walking
// only starts after the operator enables organize.auto.
func (s *SchedulerService) jobOrganizeSource(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	if s.organizer == nil || (!manual && !s.autoOrganizeSourceEnabled(ctx)) {
		return nil
	}
	taskName := "自动整理重命名刮削入库"
	if manual {
		taskName = "手动触发自动整理重命名刮削入库"
	}
	resWrap, err := s.ensureOrganizePipeline().Run(ctx, OrganizePipelineRequest{
		Scope:    OrganizeScopeDirectory,
		Trigger:  OrganizeTriggerScheduled,
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

func (s *SchedulerService) autoOrganizeSourceEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, "organize.auto")
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

func (s *SchedulerService) organizeSourceInterval(ctx context.Context) time.Duration {
	const fallback = 5 * time.Minute
	if s.repo == nil || s.repo.Setting == nil {
		return fallback
	}
	v, err := s.repo.Setting.Get(ctx, "organize.interval_seconds")
	if err != nil {
		return fallback
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || seconds <= 0 {
		return fallback
	}
	if seconds < 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

// jobPurgeRecycleBin permanently deletes media rows soft-deleted >30 days
// ago. The on-disk file is left untouched (delete is operator-driven).
func (s *SchedulerService) jobPurgeRecycleBin(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	trigger := TaskTriggerScheduled
	name := "定时清理回收站"
	if manual {
		trigger = TaskTriggerManual
		name = "手动触发回收站清理"
	}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindRecycle, trigger, name, TaskUpdate{Stage: "recycle", Message: "正在清理过期回收站记录"})
		if task == nil {
			return errors.New("create recycle task execution failed")
		}
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	res := s.repo.DB.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).
		Delete(&model.Media{})
	if res.Error != nil && !isMissingTableErr(res.Error) {
		if task != nil {
			safeErr := sanitizeTaskLogError(res.Error)
			task.Finish(safeErr, TaskUpdate{Stage: "recycle", Message: "回收站清理失败", Details: []string{fmt.Sprintf("清理 %s 之前的回收站记录失败: %v", cutoff.Format(time.RFC3339), safeErr)}})
		}
		return res.Error
	}
	err := pruneRecycleBinRows(ctx, s.repo.DB, maxRecycleBinRecords)
	if task != nil {
		detail := fmt.Sprintf("已清理 %s 之前的回收站记录，删除 %d 条；并保留最近最多 %d 条记录", cutoff.Format(time.RFC3339), res.RowsAffected, maxRecycleBinRecords)
		if err != nil {
			detail = fmt.Sprintf("已删除 %d 条过期记录，但裁剪回收站失败: %v", res.RowsAffected, sanitizeTaskLogError(err))
		}
		task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: "completed", Message: "回收站清理结束", Metrics: map[string]int64{"deleted": res.RowsAffected}, Details: []string{detail}})
	}
	return err
}

// isMissingTableErr lets the test harness ignore "no such table" errors
// that show up before AutoMigrate has run.
func isMissingTableErr(err error) bool {
	if err == nil {
		return false
	}
	return err == gorm.ErrInvalidDB
}
