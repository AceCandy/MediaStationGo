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

var ErrDoubanQueueLimit = errors.New("douban pending request limit reached")

// SaveSummaries 只写入已取得的榜单摘要，不降低完整基础资料的质量或评分。
func (r *DoubanRepository) SaveSummaries(ctx context.Context, rows []model.DoubanSubject) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "douban_id"}, {Name: "entity_kind"}},
		DoUpdates: clause.Assignments(map[string]any{
			"summary": gorm.Expr("excluded.summary"),
			"quality": gorm.Expr("CASE WHEN douban_subjects.payload IS NULL THEN excluded.quality ELSE douban_subjects.quality END"),
			"rating":  gorm.Expr("CASE WHEN douban_subjects.payload IS NULL THEN excluded.rating ELSE douban_subjects.rating END"),
		}),
	}).Create(&rows).Error
}

// QueueSupplement 持久登记精确来源身份，合并重复请求并限制每个请求者的待处理量。
func (r *DoubanRepository) QueueSupplement(ctx context.Context, id, kind, user string) (*model.DoubanSubject, error) {
	var row model.DoubanSubject
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "douban_ensure:"+user).Error; err != nil {
			return err
		}
		row = model.DoubanSubject{DoubanID: id, EntityKind: kind, Quality: model.DoubanQualityPartial}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "douban_id"}, {Name: "entity_kind"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		row = model.DoubanSubject{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("douban_id = ? AND entity_kind = ?", id, kind).First(&row).Error; err != nil {
			return err
		}
		if row.SupplementPending || (row.Payload != nil && row.Quality == model.DoubanQualityComplete && row.CreditsFetchedAt != nil) {
			return nil
		}
		var pending int64
		if err := tx.Model(&model.DoubanSubject{}).Where("supplement_pending AND supplement_requested_by = ?", user).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= 50 {
			return ErrDoubanQueueLimit
		}
		row.SupplementPending, row.SupplementRequestedBy = true, user
		return tx.Model(&row).Updates(map[string]any{"supplement_pending": true, "supplement_requested_by": user}).Error
	})
	return &row, err
}

func (r *DoubanRepository) DueSupplements(ctx context.Context, limit int) ([]model.DoubanSubject, error) {
	var rows []model.DoubanSubject
	err := r.db.WithContext(ctx).Where(`supplement_pending AND (supplement_lease_until IS NULL OR supplement_lease_until <= NOW()) AND (supplement_retry_at IS NULL OR supplement_retry_at <= NOW()) AND (next_retry_at IS NULL OR next_retry_at <= NOW())`).Order("created_at, id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *DoubanRepository) ClaimSupplement(ctx context.Context, id string) (*model.DoubanSubject, error) {
	var rows []model.DoubanSubject
	err := r.db.WithContext(ctx).Raw(`UPDATE douban_subjects SET supplement_token = ?, supplement_lease_until = NOW() + INTERVAL '5 minutes' WHERE id = ? AND supplement_pending AND (supplement_lease_until IS NULL OR supplement_lease_until <= NOW()) AND (supplement_retry_at IS NULL OR supplement_retry_at <= NOW()) AND (next_retry_at IS NULL OR next_retry_at <= NOW()) RETURNING *`, uuid.NewString(), id).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// SettleSupplement 只结算本次租约；取消时保留意图，失败时共享基础退避并独立重试演职员。
func (r *DoubanRepository) SettleSupplement(ctx context.Context, row *model.DoubanSubject, success bool, retryAt *time.Time) error {
	updates := map[string]any{"supplement_token": "", "supplement_lease_until": nil}
	if success {
		updates["supplement_pending"], updates["supplement_requested_by"], updates["supplement_retry_at"] = false, "", nil
	} else if retryAt != nil {
		updates["supplement_retry_at"] = retryAt
	}
	return r.db.WithContext(ctx).Model(&model.DoubanSubject{}).Where("id = ? AND supplement_token = ?", row.ID, row.SupplementToken).UpdateColumns(updates).Error
}

// SaveAPICredits 与头像下载共用版本比较，补全成功时间与人员资料原子落库。
func (r *DoubanRepository) SaveAPICredits(ctx context.Context, observed *model.DoubanSubject, payload []byte, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("id = ? AND rich_payload IS NOT DISTINCT FROM ?::jsonb AND fetched_at IS NOT DISTINCT FROM ?", observed.ID, observed.RichPayload, observed.FetchedAt).
		UpdateColumns(map[string]any{"rich_payload": string(payload), "credits_fetched_at": at})
	return result.RowsAffected > 0, result.Error
}
