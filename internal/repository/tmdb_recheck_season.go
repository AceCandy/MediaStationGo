package repository

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tmdbRecheckSeasonLookupSQL = `SELECT COALESCE(CASE WHEN m.kind='episode' THEN m.parent_id END,j.metadata_id)
FROM tm_db_recheck_jobs j LEFT JOIN metadata_items m ON m.id=j.metadata_id
LEFT JOIN tm_db_recheck_season_leases l ON l.metadata_id=COALESCE(CASE WHEN m.kind='episode' THEN m.parent_id END,j.metadata_id)
WHERE j.due_at<=? AND (j.lease_until IS NULL OR j.lease_until<=statement_timestamp())
AND (l.lease_until IS NULL OR l.lease_until<=statement_timestamp())
ORDER BY j.due_at,j.metadata_id LIMIT 1 FOR UPDATE OF j SKIP LOCKED`

// ClaimTMDbRecheckSeason 由最早到期目标定位季，使用条件更新互斥领取；不修改逐集结果。
func (r *MetadataRepository) ClaimTMDbRecheckSeason(ctx context.Context, cutoff time.Time) (*model.TMDbRecheckSeasonLease, error) {
	for ctx.Err() == nil {
		var id string
		err := r.db.WithContext(ctx).Raw(tmdbRecheckSeasonLookupSQL, cutoff).Scan(&id).Error
		if err != nil || id == "" {
			return nil, err
		}
		lease, err := r.ClaimTMDbRecheckSeasonByID(ctx, id, cutoff)
		if err != nil || lease != nil {
			return lease, err
		}
	}
	return nil, ctx.Err()
}

const tmdbRecheckSeasonClaimSQL = `WITH targets AS MATERIALIZED (
SELECT ?::text AS id UNION ALL SELECT id FROM metadata_items WHERE parent_id=? AND kind='episode')
INSERT INTO tm_db_recheck_season_leases(metadata_id,lease_token,lease_until)
SELECT ?, ?, clock_timestamp()+interval '5 minutes'
WHERE EXISTS(SELECT 1 FROM targets t JOIN tm_db_recheck_jobs j ON j.metadata_id=t.id
 WHERE j.due_at<=? AND (j.lease_until IS NULL OR j.lease_until<=statement_timestamp()))
ON CONFLICT(metadata_id) DO UPDATE SET lease_token=EXCLUDED.lease_token,lease_until=EXCLUDED.lease_until
WHERE tm_db_recheck_season_leases.lease_until<=statement_timestamp() RETURNING *`

// ClaimTMDbRecheckSeasonByID 按本轮优先顺序领取季；已被其他执行者占用或不再到期时跳过。
func (r *MetadataRepository) ClaimTMDbRecheckSeasonByID(ctx context.Context, id string, cutoff time.Time) (*model.TMDbRecheckSeasonLease, error) {
	var lease model.TMDbRecheckSeasonLease
	err := r.db.WithContext(ctx).Raw(tmdbRecheckSeasonClaimSQL, id, id, id, uuid.NewString(), cutoff).Scan(&lease).Error
	if err != nil || lease.MetadataID == "" {
		return nil, err
	}
	return &lease, nil
}

// lockTMDbRecheckSeason 统一季租约到目标租约的锁顺序，拒绝过期执行者写入。
func lockTMDbRecheckSeason(tx *gorm.DB, id, token string) error {
	var lease model.TMDbRecheckSeasonLease
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("metadata_id=? AND lease_token=? AND lease_until>clock_timestamp()", id, token).Take(&lease).Error
	if err == gorm.ErrRecordNotFound {
		return ErrTMDbRecheckChanged
	}
	return err
}

const tmdbRecheckSeasonPageSQL = `WITH targets AS MATERIALIZED (
SELECT ?::text AS id UNION ALL SELECT id FROM metadata_items WHERE parent_id=? AND kind='episode')
SELECT j.* FROM targets t JOIN tm_db_recheck_jobs j ON j.metadata_id=t.id
WHERE j.due_at<=? AND (j.lease_until IS NULL OR j.lease_until<=statement_timestamp())
ORDER BY j.metadata_id LIMIT 200 FOR UPDATE OF j SKIP LOCKED`

// ClaimTMDbRecheckSeasonPage 分批领取同季目标，网络访问与结果保存不持有本事务。
func (r *MetadataRepository) ClaimTMDbRecheckSeasonPage(ctx context.Context, lease *model.TMDbRecheckSeasonLease, cutoff time.Time) ([]model.TMDbRecheckJob, error) {
	var jobs []model.TMDbRecheckJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTMDbRecheckSeason(tx, lease.MetadataID, lease.LeaseToken); err != nil {
			return err
		}
		if err := tx.Raw(tmdbRecheckSeasonPageSQL, lease.MetadataID, lease.MetadataID, cutoff).Scan(&jobs).Error; err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		ids := make([]string, len(jobs))
		for i := range jobs {
			ids[i] = jobs[i].MetadataID
			jobs[i].LeaseToken, jobs[i].SeasonLeaseID = lease.LeaseToken, lease.MetadataID
		}
		return tx.Model(&model.TMDbRecheckJob{}).Where("metadata_id=ANY(?)", &ids).
			Updates(map[string]any{"status": "running", "lease_token": lease.LeaseToken, "lease_until": gorm.Expr("clock_timestamp()+interval '5 minutes'")}).Error
	})
	return jobs, err
}

// RenewTMDbRecheckSeason 同时续租季及尚未完成的小批次，避免等待中的目标提前被重新领取。
func (r *MetadataRepository) RenewTMDbRecheckSeason(ctx context.Context, lease *model.TMDbRecheckSeasonLease) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTMDbRecheckSeason(tx, lease.MetadataID, lease.LeaseToken); err != nil {
			return err
		}
		if err := tx.Model(&model.TMDbRecheckSeasonLease{}).Where("metadata_id=?", lease.MetadataID).Update("lease_until", gorm.Expr("clock_timestamp()+interval '5 minutes'")).Error; err != nil {
			return err
		}
		return tx.Model(&model.TMDbRecheckJob{}).Where("lease_token<>'' AND lease_token=? AND lease_until>clock_timestamp()", lease.LeaseToken).
			Update("lease_until", gorm.Expr("clock_timestamp()+interval '5 minutes'")).Error
	})
}

// ReleaseTMDbRecheckSeason 释放已完成季；中断时仅重排自己尚未完成的目标，保留各集结果。
func (r *MetadataRepository) ReleaseTMDbRecheckSeason(ctx context.Context, lease *model.TMDbRecheckSeasonLease) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTMDbRecheckSeason(tx, lease.MetadataID, lease.LeaseToken); err != nil {
			return err
		}
		if err := tx.Model(&model.TMDbRecheckJob{}).Where("lease_token<>'' AND lease_token=?", lease.LeaseToken).
			Updates(map[string]any{"status": "pending", "due_at": gorm.Expr("clock_timestamp()+interval '5 minutes'"), "lease_token": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		return tx.Where("metadata_id=? AND lease_token=?", lease.MetadataID, lease.LeaseToken).Delete(&model.TMDbRecheckSeasonLease{}).Error
	})
}
