package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (r *MetadataRepository) UpsertProviderSnapshot(ctx context.Context, metadataID, provider string, payload json.RawMessage, fetchedAt time.Time) error {
	if strings.TrimSpace(metadataID) == "" || strings.TrimSpace(provider) == "" || !json.Valid(payload) {
		return errors.New("valid metadata provider snapshot is required")
	}
	payloadText := string(payload)
	snapshot := model.MetadataProviderSnapshot{MetadataID: metadataID, Provider: strings.ToLower(strings.TrimSpace(provider)), Payload: payloadText, FetchedAt: fetchedAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "metadata_id"}, {Name: "provider"}},
		DoUpdates: clause.Assignments(map[string]any{"payload": payloadText, "fetched_at": fetchedAt, "updated_at": time.Now(), "deleted_at": nil}),
	}).Create(&snapshot).Error
}

func (r *MetadataRepository) FindProviderSnapshot(ctx context.Context, metadataID, provider string) (*model.MetadataProviderSnapshot, error) {
	var snapshot model.MetadataProviderSnapshot
	err := r.db.WithContext(ctx).Where("metadata_id = ? AND provider = ?", metadataID, strings.ToLower(strings.TrimSpace(provider))).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &snapshot, err
}

func (r *MetadataRepository) ListProviderSnapshotsAfter(ctx context.Context, provider string, kinds []string, afterID string, limit int) ([]model.MetadataProviderSnapshot, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || len(kinds) == 0 {
		return []model.MetadataProviderSnapshot{}, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var snapshots []model.MetadataProviderSnapshot
	q := r.db.WithContext(ctx).Model(&model.MetadataProviderSnapshot{}).
		Joins("JOIN metadata_items AS snapshot_metadata ON snapshot_metadata.id = metadata_provider_snapshots.metadata_id AND snapshot_metadata.deleted_at IS NULL").
		Where("metadata_provider_snapshots.provider = ? AND snapshot_metadata.source = ? AND snapshot_metadata.kind IN ?", provider, provider, kinds)
	if strings.TrimSpace(afterID) != "" {
		q = q.Where("metadata_provider_snapshots.id > ?", afterID)
	}
	err := q.Preload("Metadata").Order("metadata_provider_snapshots.id ASC").Limit(limit).Find(&snapshots).Error
	return snapshots, err
}

func (r *MetadataRepository) EnqueueCatalogJob(ctx context.Context, provider, entityKind, externalID string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	entityKind = strings.ToLower(strings.TrimSpace(entityKind))
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" || (entityKind != model.MetadataKindMovie && entityKind != model.MetadataKindSeries) {
		return errors.New("valid catalog hydration identity is required")
	}
	job := model.CatalogHydrationJob{Provider: provider, EntityKind: entityKind, ExternalID: externalID, Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}, {Name: "entity_kind"}, {Name: "external_id"}},
		DoNothing: true,
	}).Create(&job).Error
}

func (r *MetadataRepository) RecoverCatalogJobs(ctx context.Context) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).
		Where("status = ?", model.CatalogJobStatusRunning).
		Updates(map[string]any{"status": model.CatalogJobStatusRetry, "next_attempt_at": now, "started_at": nil, "updated_at": now}).Error
}

func (r *MetadataRepository) ClaimCatalogJob(ctx context.Context, stage string, now time.Time) (*model.CatalogHydrationJob, error) {
	var job model.CatalogHydrationJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("stage = ? AND status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", stage, []string{model.CatalogJobStatusPending, model.CatalogJobStatusRetry}, now).
			Order("created_at ASC, id ASC").First(&job).Error
		if err != nil {
			return err
		}
		return tx.Model(&job).Updates(map[string]any{"status": model.CatalogJobStatusRunning, "attempts": gorm.Expr("attempts + 1"), "started_at": now, "updated_at": now}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.Status = model.CatalogJobStatusRunning
	job.Attempts++
	job.StartedAt = &now
	return &job, nil
}

func (r *MetadataRepository) RequeueCatalogJob(ctx context.Context, id, stage string, metadataID *string) error {
	return r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).Where("id = ?", id).Updates(map[string]any{
		"status": model.CatalogJobStatusPending, "stage": stage, "metadata_id": metadataID,
		"attempts": 0, "next_attempt_at": nil, "last_error": "", "started_at": nil, "updated_at": time.Now().UTC(),
	}).Error
}

func (r *MetadataRepository) AdvanceRunningCatalogJob(ctx context.Context, id, stage, metadataID string) error {
	res := r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).
		Where("id = ? AND status = ?", id, model.CatalogJobStatusRunning).
		Updates(map[string]any{"stage": stage, "metadata_id": metadataID, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errors.New("catalog hydration job is no longer running")
	}
	return nil
}

func (r *MetadataRepository) RetryCatalogJob(ctx context.Context, id, message string, next time.Time) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	return r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).Where("id = ?", id).Updates(map[string]any{
		"status": model.CatalogJobStatusRetry, "next_attempt_at": next, "last_error": message, "started_at": nil, "updated_at": time.Now().UTC(),
	}).Error
}

func (r *MetadataRepository) CompleteCatalogJob(ctx context.Context, id, metadataID string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).Where("id = ?", id).Updates(map[string]any{
		"status": model.CatalogJobStatusCompleted, "metadata_id": metadataID, "completed_at": at,
		"next_attempt_at": nil, "last_error": "", "started_at": nil, "updated_at": at,
	}).Error
}

func (r *MetadataRepository) NextCatalogAttemptAt(ctx context.Context) (*time.Time, error) {
	var next sql.NullTime
	err := r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).
		Where("status IN ? AND next_attempt_at IS NOT NULL", []string{model.CatalogJobStatusPending, model.CatalogJobStatusRetry}).
		Select("MIN(next_attempt_at)").Scan(&next).Error
	if err != nil || !next.Valid {
		return nil, err
	}
	return &next.Time, nil
}

func (r *MetadataRepository) MarkCatalogCheckpoint(ctx context.Context, id, column string, at time.Time) error {
	switch column {
	case "catalog_metadata_hydrated_at", "catalog_artwork_hydrated_at", "catalog_hydrated_at":
	default:
		return errors.New("invalid catalog checkpoint")
	}
	return r.db.WithContext(ctx).Model(&model.MetadataItem{}).Where("id = ?", id).Update(column, at).Error
}

func (r *MetadataRepository) FindIncompleteCatalogChild(ctx context.Context, parentID, kind string) (*model.MetadataItem, error) {
	var item model.MetadataItem
	q := r.db.WithContext(ctx).Where("parent_id = ? AND kind = ? AND catalog_hydrated_at IS NULL", parentID, kind)
	if kind == model.MetadataKindSeason {
		q = q.Order("season_num ASC")
	} else {
		q = q.Order("episode_num ASC")
	}
	err := q.First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}
