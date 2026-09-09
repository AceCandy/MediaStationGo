package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const TMDbRecheckLease = 5 * time.Minute

var ErrTMDbRecheckChanged = errors.New("复查目标已变更或领取已过期")

// ScanTMDbRecheckFiles 每次核对一批文件，游标和补建登记共同提交。
func (r *MetadataRepository) ScanTMDbRecheckFiles(ctx context.Context) (bool, int, error) {
	more := false
	scanned := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.TMDbRecheckScan{ID: 1, NextAt: time.Unix(0, 0)}).Error; err != nil {
			return err
		}
		var scan model.TMDbRecheckScan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&scan, 1).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		if scan.NextAt.After(now) {
			return nil
		}
		var rows []struct {
			ID         string
			MetadataID string
		}
		if err := tx.Table("media").Select("id, COALESCE(metadata_id, '') AS metadata_id").Where("id > ?", scan.Cursor).Order("id").Limit(200).Scan(&rows).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.MetadataID)
		}
		var targets []string
		if err := tx.Raw(`SELECT id FROM metadata_items WHERE id=ANY(?) AND kind IN ('season','episode')
UNION SELECT parent_id FROM metadata_items WHERE id=ANY(?) AND kind='episode'`, &ids, &ids).Scan(&targets).Error; err != nil {
			return err
		}
		states, err := New(tx).Metadata.tmdbRecheckStates(ctx, targets)
		if err != nil {
			return err
		}
		pending := make([]string, 0, len(states))
		for _, state := range states {
			if state.Playable && state.NeedsData() {
				pending = append(pending, state.MetadataID)
			}
		}
		// 批量登记只唤醒缺资料目标，保留版本、冷却与运行租约。
		if len(pending) > 0 {
			if err := tx.Exec(`INSERT INTO tm_db_recheck_changes(metadata_id,revision,pending,expand,cursor)
SELECT id,0,true,false,'' FROM unnest(?::text[]) AS target(id) ORDER BY id
ON CONFLICT(metadata_id) DO UPDATE SET pending=true`, &pending).Error; err != nil {
				return err
			}
		}
		scanned = len(rows)
		if len(rows) == 200 {
			scan.Cursor = rows[len(rows)-1].ID
			more = true
		} else {
			scan.Cursor = ""
			scan.NextAt = now.Add(7 * 24 * time.Hour)
		}
		return tx.Save(&scan).Error
	})
	if err != nil {
		return false, 0, err
	}
	return more, scanned, nil
}

// ExpandTMDbRecheckChange 一次最多展开 200 个直接子项，不在触发器中扫描后代。
func (r *MetadataRepository) ExpandTMDbRecheckChange(ctx context.Context) (bool, error) {
	worked := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var change model.TMDbRecheckChange
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("pending").Order("metadata_id").First(&change).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		var item model.MetadataItem
		err = tx.Where("id = ?", change.MetadataID).First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Where("metadata_id = ?", change.MetadataID).Delete(&model.TMDbRecheckJob{}).Error; err != nil {
				return err
			}
			return tx.Model(&change).Updates(map[string]any{"pending": false, "expand": false, "cursor": ""}).Error
		}
		if err != nil {
			return err
		}
		if item.Kind == "season" || item.Kind == "episode" {
			var missing []model.TMDbRecheckJob
			if err := tx.Where("metadata_id=? AND status='not_found'", item.ID).Find(&missing).Error; err != nil {
				return err
			}
			if len(missing) != 0 {
				state, err := New(tx).Metadata.TMDbRecheckState(ctx, item.ID)
				if err != nil {
					return err
				}
				if state == nil || !state.Playable || !state.NeedsData() || state.RequestIdentity() != missing[0].NotFoundIdentity {
					if err := tx.Model(&model.TMDbRecheckJob{}).Where("metadata_id=? AND status='not_found'", item.ID).
						Updates(map[string]any{"status": "pending", "due_at": gorm.Expr("clock_timestamp()"), "last_error": "", "not_found_identity": ""}).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Exec(`INSERT INTO tm_db_recheck_jobs(metadata_id,status,due_at,attempts,last_error,lease_token)
VALUES (?, 'pending', clock_timestamp(), 0, '', '') ON CONFLICT(metadata_id) DO UPDATE
SET due_at = CASE WHEN tm_db_recheck_jobs.status = 'blocked' THEN EXCLUDED.due_at ELSE COALESCE(tm_db_recheck_jobs.due_at, EXCLUDED.due_at) END,
status = CASE WHEN tm_db_recheck_jobs.status IN ('done','blocked') THEN 'pending' ELSE tm_db_recheck_jobs.status END`, item.ID).Error; err != nil {
				return err
			}
		}
		if change.Expand && (item.Kind == "series" || item.Kind == "season") {
			childKind := "episode"
			if item.Kind == "series" {
				childKind = "season"
			}
			var children []string
			if err := tx.Model(&model.MetadataItem{}).Where("parent_id = ? AND kind = ? AND id > ?", item.ID, childKind, change.Cursor).Order("id").Limit(200).Pluck("id", &children).Error; err != nil {
				return err
			}
			for _, id := range children {
				if err := tx.Exec("SELECT tmdb_recheck_mark(?, ?)", id, childKind == "season").Error; err != nil {
					return err
				}
			}
			if len(children) == 200 {
				return tx.Model(&change).Update("cursor", children[len(children)-1]).Error
			}
		}
		return tx.Model(&change).Updates(map[string]any{"pending": false, "expand": false, "cursor": ""}).Error
	})
	return worked, err
}

func (r *MetadataRepository) ClaimTMDbRecheck(ctx context.Context) (*model.TMDbRecheckJob, error) {
	var job model.TMDbRecheckJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("due_at <= statement_timestamp() AND (lease_until IS NULL OR lease_until <= statement_timestamp())").Order("due_at, metadata_id").First(&job).Error
		if err != nil {
			return err
		}
		job.LeaseToken = uuid.NewString()
		return tx.Model(&job).Updates(map[string]any{"status": "running", "lease_token": job.LeaseToken, "lease_until": gorm.Expr("clock_timestamp() + interval '5 minutes'")}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &job, err
}

func (r *MetadataRepository) RenewTMDbRecheck(ctx context.Context, job *model.TMDbRecheckJob) error {
	res := r.db.WithContext(ctx).Model(&model.TMDbRecheckJob{}).
		Where("metadata_id = ? AND lease_token = ? AND lease_until > clock_timestamp()", job.MetadataID, job.LeaseToken).
		Update("lease_until", gorm.Expr("clock_timestamp() + interval '5 minutes'"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrTMDbRecheckChanged
	}
	return nil
}

// TMDbRecheckState 是单个待办的当前业务事实和并发快照。
type TMDbRecheckState struct {
	TMDbMetadataRecheckCandidate
	ParentID      string
	SeriesID      string
	Fingerprint   string
	Playable      bool
	IdentityValid bool
	CheckedAt     *time.Time
}

// RequestIdentity 只跟踪请求身份，普通资料/图片变化不得缩短 404 冷却。
func (s *TMDbRecheckState) RequestIdentity() string {
	return fmt.Sprintf("%s|%s|%s|%s|%d|%d|%s|%t", s.Kind, s.ParentID, s.SeriesID, s.SeriesTMDbID, s.SeasonNum, s.EpisodeNum, s.TMDbID, s.IdentityValid)
}

func (s *TMDbRecheckState) NeedsData() bool {
	return strings.TrimSpace(s.Overview) == "" || strings.TrimSpace(s.ReleaseDate) == "" || s.ArtworkMissing || (s.Kind == "season" && (s.TMDbID == "" || s.SnapshotMissing))
}

func (r *MetadataRepository) ExpandTMDbRecheckAsset(ctx context.Context) (bool, error) {
	worked := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var change model.TMDbRecheckAssetChange
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Order("asset_id").First(&change).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		var ids []string
		if err := tx.Model(&model.MetadataArtwork{}).Distinct("metadata_id").Where("asset_id=? AND artwork_type IN ('poster','still') AND metadata_id>?", change.AssetID, change.Cursor).Order("metadata_id").Limit(200).Pluck("metadata_id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Exec("SELECT tmdb_recheck_related(?,false)", id).Error; err != nil {
				return err
			}
		}
		if len(ids) == 200 {
			return tx.Model(&change).Update("cursor", ids[len(ids)-1]).Error
		}
		return tx.Delete(&change).Error
	})
	return worked, err
}

func (r *MetadataRepository) TMDbRecheckState(ctx context.Context, id string) (*TMDbRecheckState, error) {
	states, err := r.tmdbRecheckStates(ctx, []string{id})
	if err != nil || len(states) == 0 {
		return nil, err
	}
	return &states[0], nil
}

// tmdbRecheckStates 让初始化批量核对与领取后的单目标校验共用业务判断。
func (r *MetadataRepository) tmdbRecheckStates(ctx context.Context, ids []string) ([]TMDbRecheckState, error) {
	var states []TMDbRecheckState
	if len(ids) == 0 {
		return states, nil
	}
	err := r.db.WithContext(ctx).Raw(`SELECT mi.id AS metadata_id, mi.kind, mi.title, mi.overview, mi.release_date,
COALESCE(mi.parent_id,'') AS parent_id, series.id AS series_id, series.title AS series_title,
CASE WHEN mi.kind='season' THEN mi.season_num ELSE season.season_num END AS season_num, mi.episode_num,
sid.external_id AS series_tm_db_id, own.external_id AS tmdb_id,
(sid.n=1 AND COALESCE(sid.external_id,'') ~ '^[0-9]+$' AND (mi.kind='episode' OR own.n<=1)) AS identity_valid,
CASE WHEN mi.kind='season' THEN mi.tmdb_season_checked_at ELSE mi.tmdb_episode_checked_at END AS checked_at,
NOT EXISTS(SELECT 1 FROM metadata_provider_snapshots p WHERE p.metadata_id=mi.id AND p.provider='tmdb') AS snapshot_missing,
NOT EXISTS(SELECT 1 FROM metadata_artworks a JOIN artwork_assets aa ON aa.id=a.asset_id WHERE a.metadata_id=mi.id AND a.artwork_type=CASE WHEN mi.kind='season' THEN 'poster' ELSE 'still' END) AS artwork_missing,
(EXISTS(SELECT 1 FROM media m WHERE m.metadata_id=mi.id) OR (mi.kind='season' AND EXISTS(
 SELECT 1 FROM metadata_items e JOIN media m ON m.metadata_id=e.id WHERE e.parent_id=mi.id AND e.kind='episode'))) AS playable,
md5(concat_ws('|',mi.updated_at,mi.title,mi.overview,mi.release_date,season.updated_at,series.updated_at, COALESCE(c.revision,0),COALESCE(pc.revision,0),COALESCE(sc.revision,0))) AS fingerprint
FROM metadata_items mi
LEFT JOIN metadata_items season ON season.id=mi.parent_id AND season.kind='season' AND mi.kind='episode'
JOIN metadata_items series ON series.id=CASE WHEN mi.kind='season' THEN mi.parent_id ELSE season.parent_id END AND series.kind='series'
LEFT JOIN tm_db_recheck_changes c ON c.metadata_id=mi.id
LEFT JOIN tm_db_recheck_changes pc ON pc.metadata_id=mi.parent_id
LEFT JOIN tm_db_recheck_changes sc ON sc.metadata_id=series.id
JOIN LATERAL(SELECT count(*) AS n, min(btrim(external_id)) AS external_id FROM metadata_identifiers WHERE metadata_id=series.id AND provider='tmdb' AND entity_kind='series' AND btrim(external_id) ~ '^[0-9]+$') sid ON true
JOIN LATERAL(SELECT count(*) AS n, min(btrim(external_id)) AS external_id FROM metadata_identifiers WHERE metadata_id=mi.id AND provider='tmdb' AND entity_kind=mi.kind) own ON true
WHERE mi.id=ANY(?) AND mi.kind IN ('season','episode')`, &ids).Scan(&states).Error
	return states, err
}

// CommitTMDbRecheck 锁定快照涉及的层级和变更行，再检查租约；回调中不得访问网络。
func (r *MetadataRepository) CommitTMDbRecheck(ctx context.Context, job *model.TMDbRecheckJob, snapshot *TMDbRecheckState, save func(*Container) error) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := []string{job.MetadataID}
		// 保存阶段也向业务写入退让，避免持变更行等待对方的标识/图片行。
		if err := tx.Exec("SET LOCAL lock_timeout = '100ms'").Error; err != nil {
			return err
		}
		if snapshot != nil {
			ids = append(ids, snapshot.ParentID, snapshot.SeriesID)
		}
		var items []model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).Where("id = ANY(?)", &ids).Order("id").Find(&items).Error; err != nil {
			return err
		}
		// 后代展开按父到子锁变更行；提交遇到竞争立即重排，避免反向等待。
		var changes []model.TMDbRecheckChange
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).Where("metadata_id = ANY(?)", &ids).Order("metadata_id").Find(&changes).Error; err != nil {
			return err
		}
		for _, item := range items {
			if err := tx.Exec(`INSERT INTO tm_db_recheck_changes(metadata_id,revision,pending,expand,cursor) VALUES (?,0,false,false,'') ON CONFLICT DO NOTHING`, item.ID).Error; err != nil {
				return err
			}
		}
		var claimed model.TMDbRecheckJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("metadata_id=? AND lease_token=? AND lease_until > clock_timestamp()", job.MetadataID, job.LeaseToken).First(&claimed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTMDbRecheckChanged
		}
		if err != nil {
			return err
		}
		repos := New(tx)
		current, err := repos.Metadata.TMDbRecheckState(ctx, job.MetadataID)
		if err != nil {
			return err
		}
		if (current == nil) != (snapshot == nil) || (current != nil && (current.Fingerprint != snapshot.Fingerprint || current.Playable != snapshot.Playable || current.ArtworkMissing != snapshot.ArtworkMissing || current.IdentityValid != snapshot.IdentityValid || current.TMDbID != snapshot.TMDbID || current.SeriesTMDbID != snapshot.SeriesTMDbID || current.SnapshotMissing != snapshot.SnapshotMissing)) {
			return ErrTMDbRecheckChanged
		}
		return save(repos)
	})
	var stateErr interface{ SQLState() string }
	if errors.As(err, &stateErr) && stateErr.SQLState() == "55P03" {
		return ErrTMDbRecheckChanged
	}
	return err
}

// FinishTMDbRecheck 仅持有当前领取凭据的执行者能确认或延期。
func (r *MetadataRepository) FinishTMDbRecheck(ctx context.Context, job *model.TMDbRecheckJob, status, reason string, due *time.Time, attempts int) error {
	identity := ""
	if status == "not_found" {
		identity = job.NotFoundIdentity
	}
	res := r.db.WithContext(ctx).Model(&model.TMDbRecheckJob{}).Where("metadata_id=? AND lease_token=? AND lease_until > clock_timestamp()", job.MetadataID, job.LeaseToken).
		Updates(map[string]any{"status": status, "last_error": reason, "due_at": due, "attempts": attempts, "lease_token": "", "lease_until": nil, "not_found_identity": identity})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrTMDbRecheckChanged
	}
	return nil
}
