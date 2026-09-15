package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordTMDbInventoryMissing 登记有效季清单未收录的本地集，不抢占复查租约或延长已有冷却。
func (r *MetadataRepository) RecordTMDbInventoryMissing(ctx context.Context, id, seriesID, tmdbID string, season, episode int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '100ms'").Error; err != nil {
			return err
		}
		state, err := New(tx).Metadata.TMDbRecheckState(ctx, id)
		if err != nil || state == nil {
			return err
		}
		ids := []string{id, state.ParentID, seriesID}
		var items []model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=ANY(?)", &ids).Order("id").Find(&items).Error; err != nil {
			return err
		}
		// 标识变更也登记这些版本行；避免校验后写入已经失效的身份。
		var changes []model.TMDbRecheckChange
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).Where("metadata_id=ANY(?)", &ids).Order("metadata_id").Find(&changes).Error; err != nil {
			return err
		}
		for _, item := range items {
			if err := tx.Exec(`INSERT INTO tm_db_recheck_changes(metadata_id,revision,pending,expand,cursor) VALUES (?,0,false,false,'') ON CONFLICT DO NOTHING`, item.ID).Error; err != nil {
				return err
			}
		}
		state, err = New(tx).Metadata.TMDbRecheckState(ctx, id)
		if err != nil || state == nil {
			return err
		}
		if !state.Playable || !state.IdentityValid || state.Kind != "episode" || state.ParentID != ids[1] || state.SeriesID != seriesID || state.SeriesTMDbID != tmdbID || state.SeasonNum != season || state.EpisodeNum != episode {
			return nil
		}
		var snapshot model.MetadataProviderSnapshot
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("metadata_id=? AND provider='tmdb'", state.ParentID).Take(&snapshot).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var inventory struct {
			ID       int `json:"id"`
			Season   int `json:"season_number"`
			Episodes []struct {
				ID     int `json:"id"`
				Number int `json:"episode_number"`
			} `json:"episodes"`
		}
		if json.Unmarshal([]byte(snapshot.Payload), &inventory) != nil || inventory.ID <= 0 || inventory.Season != season || inventory.Episodes == nil {
			return nil
		}
		seen := make(map[int]bool, len(inventory.Episodes))
		for _, item := range inventory.Episodes {
			if item.ID <= 0 || item.Number <= 0 || seen[item.Number] || item.Number == episode {
				return nil
			}
			seen[item.Number] = true
		}
		now := time.Now().UTC()
		due := now.Add(state.Cooldown(now))
		return tx.Exec(`INSERT INTO tm_db_recheck_jobs(metadata_id,status,due_at,not_found_identity,last_error)
VALUES (?, 'not_found', ?, ?, 'TMDb 整季清单未包含该集，请核对匹配及编号；勿据此删除文件')
ON CONFLICT(metadata_id) DO UPDATE SET status=EXCLUDED.status,due_at=EXCLUDED.due_at,
not_found_identity=EXCLUDED.not_found_identity,last_error=EXCLUDED.last_error,attempts=0,lease_token='',lease_until=NULL
WHERE (tm_db_recheck_jobs.lease_until IS NULL OR tm_db_recheck_jobs.lease_until<=clock_timestamp())
AND (tm_db_recheck_jobs.status<>'not_found' OR tm_db_recheck_jobs.not_found_identity<>EXCLUDED.not_found_identity)`, id, due, state.RequestIdentity()).Error
	})
}
