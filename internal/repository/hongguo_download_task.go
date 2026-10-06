package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoDownloadTaskID 让传输、校验、重试和跨进程恢复共用一条作品摘要。
func HongGuoDownloadTaskID(sourceID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("hongguo_download:"+sourceID)).String()
}

// RefreshHongGuoDownloadTask 只读取分集业务状态；摘要从不参与领取、重试或恢复。
func (r *HongGuoRepository) RefreshHongGuoDownloadTask(ctx context.Context, sourceID string) (*model.TaskExecution, error) {
	return refreshDownloadWorkTask(ctx, r.db, sourceID, model.TaskSystemHongGuo)
}

// refreshDownloadWorkTask 共用两套下载队列的摘要规则；表名和身份只由内部体系决定。
func refreshDownloadWorkTask(ctx context.Context, db *gorm.DB, sourceID, system string) (*model.TaskExecution, error) {
	table, predicate := "hongguo_downloads", `source_id COLLATE "C" = ? COLLATE "C"`
	kind, sourcePath := "hongguo_download", "hongguo://"+sourceID
	if system == model.TaskSystemHuangGuoAI {
		table, predicate = "huangguoai_downloads", "source_id = ?"
		kind, sourcePath = "huangguoai_download", "huangguoai://"+sourceID
	}
	var task model.TaskExecution
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 等待前一份摘要提交后再读取队列，防止并发执行者用旧快照覆盖新状态。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", kind+":"+sourceID).Error; err != nil {
			return err
		}
		var counts struct {
			Title, Directory                             string
			Total, Completed, Failed, Cancelled, Pending int64
			First, Last                                  *time.Time
		}
		if err := tx.Raw(`SELECT MAX(title) AS title,
		MAX(regexp_replace(relative_path, '/Season 01/[^/]+$', '')) AS directory,
		COUNT(*) AS total, COUNT(*) FILTER (WHERE status = 'completed') AS completed,
		COUNT(*) FILTER (WHERE status = 'failed') AS failed,
		COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled,
		COUNT(*) FILTER (WHERE status NOT IN ('completed', 'failed', 'cancelled')) AS pending,
		MIN(created_at) AS first, MAX(updated_at) AS last
		FROM `+table+` WHERE `+predicate, sourceID).Scan(&counts).Error; err != nil {
			return err
		}
		now := time.Now()
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(kind+":"+sourceID)).String()
		if counts.Total == 0 {
			// 资料核实下架后队列可能全部移除，保留既有摘要，不能伪报下载完成。
			result := tx.Model(&model.TaskExecution{}).Where("id = ?", id).Updates(map[string]any{
				"status": "interrupted", "stage": "removed", "message": "下载队列已移除",
				"error": "", "metrics": "{}", "finished_at": now, "updated_at": now,
			})
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
			return tx.First(&task, "id = ?", id).Error
		}
		status := "running"
		var finished *time.Time
		if counts.Pending == 0 {
			finished = counts.Last
			switch {
			case counts.Completed == counts.Total:
				status = "completed"
			case counts.Failed > 0:
				status = "failed"
			default:
				status = "interrupted"
			}
		}
		metrics, err := json.Marshal(map[string]int64{
			"total": counts.Total, "completed": counts.Completed, "failed": counts.Failed,
			"cancelled": counts.Cancelled, "remaining": counts.Pending,
		})
		if err != nil {
			return err
		}
		name, directory := "红果下载："+counts.Title, counts.Directory
		if system == model.TaskSystemHuangGuoAI {
			// 黄果来源标题和含标题路径不进入执行摘要或诊断日志。
			name, directory = "黄果 AI 下载 "+sourceID, ""
		}
		task = model.TaskExecution{
			Base: model.Base{ID: id}, SourcePath: sourcePath,
			System: system, Kind: kind, Trigger: "manual",
			Name: name, Status: status, Stage: status,
			DestPath: directory, StartedAt: *counts.First, FinishedAt: finished,
			Message: fmt.Sprintf("完成 %d/%d 集，失败 %d 集，待处理 %d 集，取消 %d 集",
				counts.Completed, counts.Total, counts.Failed, counts.Pending, counts.Cancelled),
			Metrics: string(metrics),
		}
		// 恢复/补集清空终态结束时间；首次开始时间始终保留。
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"source_path", "system", "name", "status", "stage", "dest_path",
				"message", "error", "metrics", "finished_at", "updated_at",
			}),
		}).Create(&task).Error; err != nil {
			return err
		}
		return tx.First(&task, "id = ?", id).Error
	})
	if err != nil {
		return nil, err
	}
	if task.ID == "" {
		return nil, nil
	}
	return &task, nil
}
