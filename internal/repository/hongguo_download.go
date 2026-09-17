package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ClaimHongGuoDownload 获取一个待执行或租约过期的分集；已校验状态用于发布中断恢复。
func (r *HongGuoRepository) ClaimHongGuoDownload(ctx context.Context) (*model.HongGuoDownload, error) {
	return r.claimHongGuoDownload(ctx, false)
}

// ClaimHongGuoVerification 独立领取校验/发布任务，不占网络传输名额。
func (r *HongGuoRepository) ClaimHongGuoVerification(ctx context.Context) (*model.HongGuoDownload, error) {
	return r.claimHongGuoDownload(ctx, true)
}

func (r *HongGuoRepository) claimHongGuoDownload(ctx context.Context, verification bool) (*model.HongGuoDownload, error) {
	var row model.HongGuoDownload
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		until := now.Add(time.Minute)
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ? OR (status IN ? AND lease_until < ?)", []string{"queued", "waiting_verify"}, []string{"downloading", "verifying", "publishing"}, now)
		if verification {
			query = query.Where("raw_size > 0 OR COALESCE(sha256, '') <> ''")
		} else {
			query = query.Where("raw_size = 0 AND COALESCE(sha256, '') = ''")
		}
		if err := query.Order("created_at, id").First(&row).Error; err != nil {
			return err
		}
		row.LeaseToken = uuid.NewString()
		row.LeaseUntil = &until
		if !verification {
			row.Attempts++
		}
		if row.SHA256 != "" {
			row.Status = "publishing"
		} else if row.RawSize > 0 {
			row.Status = "verifying"
		} else {
			row.Status = "downloading"
			row.Bytes = 0
			row.TotalBytes = 0
		}
		row.Error = ""
		return tx.Save(&row).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// UpdateHongGuoDownload 条件写入防止取消和过期执行者覆盖新状态。
func (r *HongGuoRepository) UpdateHongGuoDownload(ctx context.Context, id, token string, values map[string]any) error {
	result := r.db.WithContext(ctx).Model(&model.HongGuoDownload{}).Where("id = ? AND lease_token = ? AND status IN ?", id, token, []string{"downloading", "verifying", "publishing"}).Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("下载已取消或执行权已变更")
	}
	return nil
}

// PublishHongGuoDownload 在行锁保护下执行发布，取消和新执行者不能穿越最终检查。
func (r *HongGuoRepository) PublishHongGuoDownload(ctx context.Context, row model.HongGuoDownload, publish func() error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.HongGuoDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", row.ID).Error; err != nil {
			return err
		}
		if current.LeaseToken != row.LeaseToken || current.Status != "publishing" || current.LeaseUntil == nil || current.LeaseUntil.Before(time.Now()) {
			return errors.New("下载发布执行权已失效")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := publish(); err != nil {
			return err
		}
		return tx.Model(&current).Updates(map[string]any{"status": "completed", "error": "", "bytes": current.VerifiedSize, "total_bytes": current.VerifiedSize, "raw_size": 0, "lease_token": "", "lease_until": nil}).Error
	})
}
