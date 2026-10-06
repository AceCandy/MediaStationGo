package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var hongGuoDownloadSourceID = regexp.MustCompile(`^[0-9]+$`)

// LegacyHongGuoDownloadTaskGroups 一次扫描关联旧摘要，避免按作品反复扫描百万行历史。
// 仅供一次性整理使用，不在启动或定时任务中调用。
func (r *HongGuoRepository) LegacyHongGuoDownloadTaskGroups(ctx context.Context, cutoff time.Time) (map[string][]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id, substring(dest_path FROM '\[hongguo-([0-9]+)\]/')
		FROM task_executions WHERE kind = 'hongguo_download' AND COALESCE(source_path, '') NOT LIKE 'hongguo://%'
		AND status IN ('completed', 'failed', 'interrupted') AND deleted_at IS NULL
		AND created_at < ? AND finished_at < ? AND dest_path ~ '\[hongguo-[0-9]+\]/'`, cutoff, cutoff).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make(map[string][]string)
	for rows.Next() {
		var id, sourceID string
		if err := rows.Scan(&id, &sourceID); err != nil {
			return nil, err
		}
		groups[sourceID] = append(groups[sourceID], id)
	}
	return groups, rows.Err()
}

// CompactHongGuoDownloadTasks 原子保留作品摘要后清除截止前的终态分集历史；不改变业务队列。
func (r *HongGuoRepository) CompactHongGuoDownloadTasks(ctx context.Context, sourceID string, cutoff time.Time, ids []string) (int64, error) {
	if !hongGuoDownloadSourceID.MatchString(sourceID) || cutoff.IsZero() {
		return 0, errors.New("作品 ID 或历史截止时间无效")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 20000 {
		return 0, errors.New("单批历史执行超过整理上限")
	}
	var removed int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "hongguo_download:"+sourceID).Error; err != nil {
			return err
		}
		legacy := func() *gorm.DB {
			return tx.Model(&model.TaskExecution{}).
				Where("id IN ?", ids).
				Where("kind = ? AND COALESCE(source_path, '') NOT LIKE 'hongguo://%' AND status IN ? AND created_at < ? AND finished_at < ? AND dest_path LIKE ?",
					"hongguo_download", []string{"completed", "failed", "interrupted"}, cutoff, cutoff, "%[hongguo-"+sourceID+"]/%")
		}
		var stats struct {
			Total       int64
			First, Last *time.Time
		}
		if err := legacy().Select("COUNT(*) AS total, MIN(started_at) AS first, MAX(finished_at) AS last").Scan(&stats).Error; err != nil {
			return err
		}
		if stats.Total == 0 {
			return nil
		}
		// 同事务内复用队列汇总；新摘要始终以当前业务状态为准。
		row, err := (&HongGuoRepository{db: tx}).RefreshHongGuoDownloadTask(ctx, sourceID)
		if err != nil {
			return err
		}
		if row == nil {
			var last model.TaskExecution
			if err := legacy().Order("started_at DESC, id DESC").First(&last).Error; err != nil {
				return err
			}
			last.ID = HongGuoDownloadTaskID(sourceID)
			last.SourcePath = "hongguo://" + sourceID
			last.System = model.TaskSystemHongGuo
			last.Name = regexp.MustCompile(` E[0-9]+$`).ReplaceAllString(last.Name, "")
			last.DestPath = regexp.MustCompile(`/Season 01/[^/]+$`).ReplaceAllString(last.DestPath, "")
			last.Status, last.Stage = "interrupted", "legacy"
			last.Message, last.Error = "旧分集执行历史已整理，当前无下载队列", ""
			last.Metrics = "{}"
			last.StartedAt, last.FinishedAt = *stats.First, stats.Last
			last.UpdatedAt = time.Now()
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&last).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.TaskExecution{}).Where("id = ?", HongGuoDownloadTaskID(sourceID)).
			Update("started_at", gorm.Expr("LEAST(started_at, ?)", stats.First)).Error; err != nil {
			return err
		}
		result := legacy().Unscoped().Where("deleted_at IS NULL").Delete(&model.TaskExecution{})
		removed = result.RowsAffected
		return result.Error
	})
	return removed, err
}
