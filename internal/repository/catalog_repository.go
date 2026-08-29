package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type CatalogArtworkCandidate struct {
	MetadataID      string
	EntityKind      string
	ExternalID      string
	MissingPoster   bool
	MissingBackdrop bool
	MissingStill    bool
}

// TMDbSnapshotBackfillCandidate 是具有合法 TMDb 标识且缺少原始快照的元数据。
type TMDbSnapshotBackfillCandidate struct {
	MetadataID   string
	Title        string
	EntityKind   string
	TMDbID       int `gorm:"column:tmdb_id"`
	SeriesTMDbID int `gorm:"column:series_tmdb_id"`
	SeasonNum    int
	EpisodeNum   int
}

type CatalogJobEnsureResult string

const (
	CatalogJobCreated   CatalogJobEnsureResult = "created"
	CatalogJobRequeued  CatalogJobEnsureResult = "requeued"
	CatalogJobUnchanged CatalogJobEnsureResult = "unchanged"
)

func catalogArtworkPageLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	return min(limit, 1000)
}

// ListMissingCatalogArtworkAfter 按 metadata ID 返回缺少实体自有图片的 TMDb 元数据。
func (r *MetadataRepository) ListMissingCatalogArtworkAfter(ctx context.Context, afterID string, limit int) ([]CatalogArtworkCandidate, error) {
	var rows []CatalogArtworkCandidate
	err := r.db.WithContext(ctx).Raw(`
SELECT mi.id AS metadata_id,
       mi.kind AS entity_kind,
       (SELECT mid.external_id
        FROM metadata_identifiers AS mid
        WHERE mid.metadata_id = mi.id AND mid.provider = 'tmdb' AND mid.entity_kind = mi.kind
          AND mid.external_id ~ '^[1-9][0-9]*$'
        ORDER BY LENGTH(mid.external_id), mid.external_id, mid.id
        LIMIT 1) AS external_id,
       mi.kind IN ('movie', 'series', 'season') AND NOT EXISTS (
           SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id
           WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'poster'
       ) AS missing_poster,
       mi.kind IN ('movie', 'series') AND NOT EXISTS (
           SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id
           WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'backdrop'
       ) AS missing_backdrop,
       mi.kind = 'episode' AND NOT EXISTS (
           SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id
           WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'still'
       ) AS missing_still
FROM metadata_items AS mi
WHERE mi.id > ?
  AND mi.catalog_artwork_hydrated_at IS NULL
  AND EXISTS (
      SELECT 1 FROM metadata_identifiers AS mid
      WHERE mid.metadata_id = mi.id AND mid.provider = 'tmdb' AND mid.entity_kind = mi.kind
        AND mid.external_id ~ '^[1-9][0-9]*$'
  )
  AND (
      (mi.kind IN ('movie', 'series') AND (
          NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'poster')
          OR NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'backdrop')
      ))
      OR (mi.kind = 'season' AND NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'poster'))
      OR (mi.kind = 'episode' AND NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = mi.id AND ma.artwork_type = 'still'))
  )
ORDER BY mi.id
LIMIT ?`, strings.TrimSpace(afterID), catalogArtworkPageLimit(limit)).Scan(&rows).Error
	return rows, err
}

// ListMissingCatalogArtworkRootsAfter 将缺图子项归并到可由 catalog worker 处理的 Movie/Series root。
func (r *MetadataRepository) ListMissingCatalogArtworkRootsAfter(ctx context.Context, afterID string, limit int, manual bool) ([]CatalogArtworkCandidate, error) {
	var rows []CatalogArtworkCandidate
	err := r.db.WithContext(ctx).Raw(`
SELECT root.id AS metadata_id,
       root.kind AS entity_kind,
       (SELECT mid.external_id
        FROM metadata_identifiers AS mid
        WHERE mid.metadata_id = root.id AND mid.provider = 'tmdb' AND mid.entity_kind = root.kind
          AND mid.external_id ~ '^[1-9][0-9]*$'
        ORDER BY LENGTH(mid.external_id), mid.external_id, mid.id
        LIMIT 1) AS external_id
FROM metadata_items AS root
WHERE root.id > ?
  AND root.kind IN ('movie', 'series')
  AND EXISTS (
      SELECT 1 FROM metadata_identifiers AS mid
      WHERE mid.metadata_id = root.id AND mid.provider = 'tmdb' AND mid.entity_kind = root.kind
        AND mid.external_id ~ '^[1-9][0-9]*$'
  )
  AND NOT EXISTS (
      SELECT 1 FROM catalog_hydration_jobs job
      WHERE job.provider = 'tmdb' AND job.entity_kind = root.kind
        AND job.external_id = (SELECT mid.external_id FROM metadata_identifiers mid WHERE mid.metadata_id = root.id AND mid.provider = 'tmdb' AND mid.entity_kind = root.kind AND mid.external_id ~ '^[1-9][0-9]*$' ORDER BY LENGTH(mid.external_id), mid.external_id, mid.id LIMIT 1)
        AND (job.status IN ('pending', 'running', 'retry') OR (NOT ? AND job.status = 'failed'))
  )
  AND (
      (root.catalog_artwork_hydrated_at IS NULL AND (
          NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = root.id AND ma.artwork_type = 'poster')
          OR NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = root.id AND ma.artwork_type = 'backdrop')
      ))
      OR (root.kind = 'series' AND EXISTS (
          SELECT 1 FROM metadata_items season
          WHERE season.parent_id = root.id AND season.kind = 'season'
            AND EXISTS (SELECT 1 FROM metadata_identifiers mid WHERE mid.metadata_id = season.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'season' AND mid.external_id ~ '^[1-9][0-9]*$')
            AND season.catalog_artwork_hydrated_at IS NULL
            AND NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = season.id AND ma.artwork_type = 'poster')
      ))
      OR (root.kind = 'series' AND EXISTS (
          SELECT 1 FROM metadata_items season
          JOIN metadata_items episode ON episode.parent_id = season.id AND episode.kind = 'episode'
          WHERE season.parent_id = root.id AND season.kind = 'season'
            AND EXISTS (SELECT 1 FROM metadata_identifiers mid WHERE mid.metadata_id = episode.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'episode' AND mid.external_id ~ '^[1-9][0-9]*$')
            AND episode.catalog_artwork_hydrated_at IS NULL
            AND NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = episode.id AND ma.artwork_type = 'still')
      ))
  )
ORDER BY root.id
LIMIT ?`, strings.TrimSpace(afterID), manual, catalogArtworkPageLimit(limit)).Scan(&rows).Error
	return rows, err
}

// EnsureCatalogArtworkJob 原子创建缺图 job，或按触发方式复活 terminal job。
func (r *MetadataRepository) EnsureCatalogArtworkJob(ctx context.Context, candidate CatalogArtworkCandidate, manual bool) (CatalogJobEnsureResult, error) {
	if candidate.EntityKind != model.MetadataKindMovie && candidate.EntityKind != model.MetadataKindSeries {
		return "", errors.New("catalog artwork root must be movie or series")
	}
	if value, err := strconv.ParseUint(strings.TrimSpace(candidate.ExternalID), 10, 64); err != nil || value == 0 || strings.TrimSpace(candidate.MetadataID) == "" {
		return "", errors.New("valid catalog artwork identity is required")
	}
	now := time.Now().UTC()
	var result struct{ Created bool }
	query := r.db.WithContext(ctx).Raw(`
INSERT INTO catalog_hydration_jobs (
    id, created_at, updated_at, provider, entity_kind, external_id, metadata_id,
    status, stage, attempts, last_error
) VALUES (?, ?, ?, 'tmdb', ?, ?, ?, 'pending', 'root', 0, '')
ON CONFLICT (provider, entity_kind, external_id) DO UPDATE SET
    metadata_id = EXCLUDED.metadata_id,
    status = 'pending', stage = 'root', attempts = 0, next_attempt_at = NULL,
    last_error = '', started_at = NULL, completed_at = NULL, updated_at = EXCLUDED.updated_at
WHERE catalog_hydration_jobs.status = 'completed'
   OR (? AND catalog_hydration_jobs.status = 'failed')
RETURNING xmax = 0 AS created`, uuid.NewString(), now, now, candidate.EntityKind, strings.TrimSpace(candidate.ExternalID), strings.TrimSpace(candidate.MetadataID), manual)
	query = query.Scan(&result)
	if err := query.Error; err != nil {
		return "", err
	}
	if query.RowsAffected == 0 {
		return CatalogJobUnchanged, nil
	}
	if result.Created {
		return CatalogJobCreated, nil
	}
	return CatalogJobRequeued, nil
}

func (r *MetadataRepository) UpsertProviderSnapshot(ctx context.Context, metadataID, provider string, payload json.RawMessage, fetchedAt time.Time) error {
	if strings.TrimSpace(metadataID) == "" || strings.TrimSpace(provider) == "" || !json.Valid(payload) {
		return errors.New("valid metadata provider snapshot is required")
	}
	payloadText := string(payload)
	snapshot := model.MetadataProviderSnapshot{MetadataID: metadataID, Provider: strings.ToLower(strings.TrimSpace(provider)), Payload: payloadText, FetchedAt: fetchedAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "metadata_id"}, {Name: "provider"}},
		DoUpdates: clause.Assignments(map[string]any{"payload": payloadText, "fetched_at": fetchedAt, "updated_at": time.Now()}),
	}).Create(&snapshot).Error
}

// ReplaceIdentifierWithSnapshot 在同一事务中更新 provider 标识和对应原始快照。
func (r *MetadataRepository) ReplaceIdentifierWithSnapshot(ctx context.Context, metadataID, provider, entityKind, externalID string, payload json.RawMessage, fetchedAt time.Time) error {
	if !json.Valid(payload) {
		return errors.New("valid metadata provider snapshot is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := replaceMetadataIdentifier(tx, metadataID, provider, entityKind, externalID); err != nil {
			return err
		}
		txRepo := &MetadataRepository{db: tx}
		return txRepo.UpsertProviderSnapshot(ctx, metadataID, provider, payload, fetchedAt)
	})
}

func (r *MetadataRepository) missingTMDbSnapshotQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Table("metadata_items AS mi").
		Joins(`JOIN LATERAL (
            SELECT CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END AS tmdb_id
            FROM metadata_identifiers AS mid
            WHERE mid.metadata_id = mi.id
              AND mid.provider = 'tmdb'
              AND mid.entity_kind = mi.kind
              AND mid.external_id ~ '^[1-9][0-9]{0,9}$'
            ORDER BY LENGTH(mid.external_id), mid.external_id, mid.id
            LIMIT 1
        ) AS own_tmdb ON own_tmdb.tmdb_id BETWEEN 1 AND 2147483647`).
		Where("mi.kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries, model.MetadataKindSeason, model.MetadataKindEpisode}).
		Where(`NOT EXISTS (
            SELECT 1 FROM metadata_provider_snapshots AS snapshot
            WHERE snapshot.metadata_id = mi.id AND snapshot.provider = 'tmdb'
        )`)
}

// CountMissingTMDbSnapshots 返回当前全库缺失 TMDb 快照的合法候选数。
func (r *MetadataRepository) CountMissingTMDbSnapshots(ctx context.Context) (int64, error) {
	var total int64
	err := r.missingTMDbSnapshotQuery(ctx).Count(&total).Error
	return total, err
}

// ListMissingTMDbSnapshotsAfter 按 metadata ID 分页，且不依赖 media 关联。
func (r *MetadataRepository) ListMissingTMDbSnapshotsAfter(ctx context.Context, afterID string, limit int) ([]TMDbSnapshotBackfillCandidate, error) {
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	var candidates []TMDbSnapshotBackfillCandidate
	err := r.missingTMDbSnapshotQuery(ctx).
		Select(`mi.id AS metadata_id, mi.title, mi.kind AS entity_kind,
            own_tmdb.tmdb_id, COALESCE(series_tmdb.tmdb_id, 0) AS series_tmdb_id,
            CASE WHEN mi.kind = 'season' THEN mi.season_num WHEN mi.kind = 'episode' THEN season.season_num ELSE 0 END AS season_num,
            mi.episode_num`).
		Joins("LEFT JOIN metadata_items AS season ON mi.kind = 'episode' AND season.id = mi.parent_id AND season.kind = 'season'").
		Joins(`LEFT JOIN metadata_items AS series ON series.kind = 'series' AND series.id = CASE
            WHEN mi.kind = 'season' THEN mi.parent_id
            WHEN mi.kind = 'episode' THEN season.parent_id
            ELSE NULL
        END`).
		Joins(`LEFT JOIN LATERAL (
            SELECT CASE WHEN mid.external_id ~ '^[1-9][0-9]{0,9}$' THEN mid.external_id::bigint END AS tmdb_id
            FROM metadata_identifiers AS mid
            WHERE mid.metadata_id = series.id
              AND mid.provider = 'tmdb'
              AND mid.entity_kind = 'series'
              AND mid.external_id ~ '^[1-9][0-9]{0,9}$'
            ORDER BY LENGTH(mid.external_id), mid.external_id, mid.id
            LIMIT 1
        ) AS series_tmdb ON series_tmdb.tmdb_id BETWEEN 1 AND 2147483647`).
		Where("mi.id > ?", strings.TrimSpace(afterID)).
		Order("mi.id ASC").Limit(limit).Scan(&candidates).Error
	return candidates, err
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
		Joins("JOIN metadata_items AS snapshot_metadata ON snapshot_metadata.id = metadata_provider_snapshots.metadata_id").
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
		"next_attempt_at": nil, "last_error": "", "started_at": nil, "updated_at": time.Now().UTC(),
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

func (r *MetadataRepository) FailCatalogJob(ctx context.Context, id, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	return r.db.WithContext(ctx).Model(&model.CatalogHydrationJob{}).Where("id = ?", id).Updates(map[string]any{
		"status": model.CatalogJobStatusFailed, "next_attempt_at": nil, "last_error": message,
		"started_at": nil, "updated_at": time.Now().UTC(),
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
	q := r.db.WithContext(ctx).Where("parent_id = ? AND kind = ?", parentID, kind).
		Where(`catalog_hydrated_at IS NULL OR catalog_metadata_hydrated_at IS NULL OR catalog_artwork_hydrated_at IS NULL
OR (? = 'season' AND EXISTS (
    SELECT 1 FROM metadata_items episode
    WHERE episode.parent_id = metadata_items.id AND episode.kind = 'episode'
      AND (episode.catalog_hydrated_at IS NULL OR episode.catalog_metadata_hydrated_at IS NULL OR episode.catalog_artwork_hydrated_at IS NULL)
))`, kind)
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
