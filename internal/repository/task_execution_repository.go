package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// TaskExecutionRepository 管理任务中心的一次性执行记录。
type TaskExecutionRepository struct{ db *gorm.DB }

func (r *TaskExecutionRepository) Create(ctx context.Context, row *model.TaskExecution) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *TaskExecutionRepository) Update(ctx context.Context, id string, values map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.TaskExecution{}).Where("id = ?", id).Updates(values).Error
}

func (r *TaskExecutionRepository) Find(ctx context.Context, id string) (*model.TaskExecution, error) {
	var row model.TaskExecution
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *TaskExecutionRepository) List(ctx context.Context, offset, limit int) ([]model.TaskExecution, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.TaskExecution{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.TaskExecution
	err := r.db.WithContext(ctx).Order("started_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func (r *TaskExecutionRepository) MarkRunningInterrupted(ctx context.Context, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("status = ?", "running").
		Updates(map[string]any{"status": "interrupted", "message": "服务重启，任务执行已中断", "finished_at": at, "updated_at": at}).Error
}
