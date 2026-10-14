package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// TMDbSourceIdentity 是按需获取来源资料时校验的本地绑定。
type TMDbSourceIdentity struct {
	MetadataID   string
	Title        string
	SeriesTitle  string
	EntityKind   string
	TMDbID       int `gorm:"column:tmdb_id"`
	SeriesTMDbID int `gorm:"column:series_tmdb_id"`
	SeasonNum    int
	EpisodeNum   int
}

func (r *MetadataRepository) SaveProviderData(ctx context.Context, metadataID, provider string, payload json.RawMessage, fetchedAt time.Time) error {
	return r.saveProviderData(ctx, metadataID, provider, payload, fetchedAt, false)
}

func (r *MetadataRepository) SaveDegradedProviderData(ctx context.Context, metadataID, provider string, payload json.RawMessage, fetchedAt time.Time) error {
	return r.saveProviderData(ctx, metadataID, provider, payload, fetchedAt, true)
}

func (r *MetadataRepository) saveProviderData(ctx context.Context, metadataID, provider string, payload json.RawMessage, fetchedAt time.Time, degraded bool) error {
	if strings.TrimSpace(metadataID) == "" || strings.TrimSpace(provider) == "" || !json.Valid(payload) {
		return errors.New("valid provider data is required")
	}
	if strings.EqualFold(strings.TrimSpace(provider), "douban") {
		return r.saveDoubanSubject(ctx, metadataID, payload, fetchedAt, degraded)
	}
	if strings.EqualFold(strings.TrimSpace(provider), "tmdb") {
		return r.saveTMDbSourceData(ctx, metadataID, payload, fetchedAt, degraded)
	}
	return errors.New("unsupported catalog provider")
}

// ReplaceIdentifierWithData 在同一事务中更新 provider 标识和资料中心正文。
func (r *MetadataRepository) ReplaceIdentifierWithData(ctx context.Context, metadataID, provider, entityKind, externalID string, payload json.RawMessage, fetchedAt time.Time) error {
	if !json.Valid(payload) {
		return errors.New("valid provider data is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := replaceMetadataIdentifier(tx, metadataID, provider, entityKind, externalID); err != nil {
			return err
		}
		txRepo := &MetadataRepository{db: tx}
		return txRepo.SaveProviderData(ctx, metadataID, provider, payload, fetchedAt)
	})
}

// InsertMissingTMDbData 校验当前身份后只补空缺，保护请求期间的改绑和其他路径已保存的来源资料。
func (r *MetadataRepository) InsertMissingTMDbData(ctx context.Context, candidate TMDbSourceIdentity, payload json.RawMessage, fetchedAt time.Time) error {
	if !json.Valid(payload) || candidate.TMDbID <= 0 {
		return errors.New("valid TMDB source data is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&item, "id = ? AND kind = ?", candidate.MetadataID, candidate.EntityKind).Error; err != nil {
			return err
		}
		var identifier model.MetadataIdentifier
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"metadata_id = ? AND provider = ? AND entity_kind = ? AND external_id = ?",
			candidate.MetadataID, "tmdb", candidate.EntityKind, strconv.Itoa(candidate.TMDbID),
		).First(&identifier).Error; err != nil {
			return err
		}
		// 与所有中心写入共用事务锁，避免请求期间已补入的来源被旧结果覆盖。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('tmdb_catalog_write'))").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table(tmdbSourceDataSQL+" AS snapshots").Where("metadata_id = ? AND provider = 'tmdb'", candidate.MetadataID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		return (&MetadataRepository{db: tx}).saveTMDbSourceData(ctx, candidate.MetadataID, payload, fetchedAt, false)
	})
}

func (r *MetadataRepository) FindProviderData(ctx context.Context, metadataID, provider string) (*model.ProviderData, error) {
	if strings.EqualFold(strings.TrimSpace(provider), "douban") {
		subject, err := (&DoubanRepository{db: r.db}).FindForMetadata(ctx, metadataID)
		if err != nil || subject == nil || subject.Payload == nil || subject.FetchedAt == nil {
			return nil, err
		}
		return &model.ProviderData{MetadataID: metadataID, Provider: "douban", Payload: *subject.Payload,
			Degraded: subject.Quality == model.DoubanQualityDegraded, FetchedAt: *subject.FetchedAt}, nil
	}
	var snapshot model.ProviderData
	err := r.db.WithContext(ctx).Table(tmdbSourceDataSQL+" AS source_data").Where("metadata_id = ? AND provider = ?", metadataID, strings.ToLower(strings.TrimSpace(provider))).Take(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &snapshot, err
}

func (r *MetadataRepository) ListProviderDataAfter(ctx context.Context, provider string, kinds []string, afterID string, limit int) ([]model.ProviderData, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || len(kinds) == 0 {
		return []model.ProviderData{}, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var snapshots []model.ProviderData
	q := r.db.WithContext(ctx).Model(&model.ProviderData{}).Table(tmdbSourceDataSQL+" AS source_data").
		Select("source_data.*").
		Joins("JOIN metadata_presentations AS snapshot_metadata ON snapshot_metadata.id = source_data.metadata_id").
		Where("source_data.provider = ? AND snapshot_metadata.source = ? AND snapshot_metadata.kind IN ?", provider, provider, kinds)
	if strings.TrimSpace(afterID) != "" {
		q = q.Where("source_data.id > ?", afterID)
	}
	err := q.Preload("Metadata", func(db *gorm.DB) *gorm.DB { return db.Table("metadata_presentations") }).Order("source_data.id ASC").Limit(limit).Find(&snapshots).Error
	return snapshots, err
}

var ErrMetadataAssociationChanged = errors.New("资料关联已变化，请刷新后重试")

// RebindTMDb 拒绝网络请求期间发生的父子改绑，不重建本地媒体身份。
func (r *MetadataRepository) RebindTMDb(ctx context.Context, observed model.MetadataItem, externalID string, payload json.RawMessage) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMetadataHierarchy(tx, observed.ID); err != nil {
			return err
		}
		var current model.MetadataItem
		if err := tx.First(&current, "id=?", observed.ID).Error; err != nil {
			return err
		}
		if !current.UpdatedAt.Equal(observed.UpdatedAt) {
			return ErrMetadataAssociationChanged
		}
		if err := replaceMetadataIdentifier(tx, observed.ID, "tmdb", observed.Kind, externalID); err != nil {
			return err
		}
		if externalID == "" {
			return nil
		}
		return (&MetadataRepository{db: tx}).SaveProviderData(ctx, observed.ID, "tmdb", payload, time.Now().UTC())
	})
}
