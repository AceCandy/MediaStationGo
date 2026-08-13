package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// MetadataRepository 管理共享元数据及其 provider 标识。
type MetadataRepository struct {
	db   *gorm.DB
	view *MediaViewRepository
}

func (r *MetadataRepository) FindByID(ctx context.Context, id string) (*model.MetadataItem, error) {
	var item model.MetadataItem
	if err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *MetadataRepository) FindByIdentifier(ctx context.Context, provider, entityKind, externalID string) (*model.MetadataItem, error) {
	identifier := model.MetadataIdentifier{Provider: provider, EntityKind: entityKind, ExternalID: externalID}
	if err := normalizeMetadataIdentifier(&identifier); err != nil {
		return nil, err
	}
	var item model.MetadataItem
	err := r.db.WithContext(ctx).
		Table("metadata_items AS mi").
		Joins("JOIN metadata_identifiers AS mid ON mid.metadata_id = mi.id AND mid.deleted_at IS NULL").
		Where("mi.deleted_at IS NULL AND mid.provider = ? AND mid.entity_kind = ? AND mid.external_id = ?", identifier.Provider, identifier.EntityKind, identifier.ExternalID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MetadataRepository) FindSeason(ctx context.Context, seriesID string, seasonNum int) (*model.MetadataItem, error) {
	var item model.MetadataItem
	err := r.db.WithContext(ctx).
		Where("kind = ? AND parent_id = ? AND season_num = ?", model.MetadataKindSeason, seriesID, seasonNum).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MetadataRepository) FindEpisode(ctx context.Context, seriesID string, seasonNum, episodeNum int) (*model.MetadataItem, error) {
	season, err := r.FindSeason(ctx, seriesID, seasonNum)
	if err != nil || season == nil {
		return nil, err
	}
	var item model.MetadataItem
	err = r.db.WithContext(ctx).
		Where("kind = ? AND parent_id = ? AND episode_num = ?", model.MetadataKindEpisode, season.ID, episodeNum).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MetadataRepository) Create(ctx context.Context, item *model.MetadataItem, identifiers []model.MetadataIdentifier) error {
	if err := validateMetadataItem(item); err != nil {
		return err
	}
	if err := normalizeMetadataIdentifiers(identifiers); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateMetadataParent(tx, item); err != nil {
			return err
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		for i := range identifiers {
			identifiers[i].MetadataID = item.ID
		}
		if len(identifiers) == 0 {
			return nil
		}
		return tx.Create(&identifiers).Error
	})
}

// UpsertCanonical 按 provider 标识解析共享实体；多个标识若已指向不同实体则拒绝合并。
func (r *MetadataRepository) UpsertCanonical(ctx context.Context, item *model.MetadataItem, identifiers []model.MetadataIdentifier, preferredID string) (*model.MetadataItem, error) {
	if item == nil || strings.TrimSpace(item.Title) == "" {
		return nil, errors.New("metadata title is required")
	}
	if err := validateMetadataItem(item); err != nil {
		return nil, err
	}
	if err := normalizeMetadataIdentifiers(identifiers); err != nil {
		return nil, err
	}
	var saved model.MetadataItem
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateMetadataParent(tx, item); err != nil {
			return err
		}
		metadataID := strings.TrimSpace(preferredID)
		for _, identifier := range identifiers {
			var existing model.MetadataIdentifier
			err := tx.Where("provider = ? AND entity_kind = ? AND external_id = ?",
				identifier.Provider, identifier.EntityKind, identifier.ExternalID).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if metadataID != "" && metadataID != existing.MetadataID {
				return fmt.Errorf("metadata identifiers resolve to different items: %s and %s", metadataID, existing.MetadataID)
			}
			metadataID = existing.MetadataID
		}

		if metadataID == "" {
			if err := tx.Create(item).Error; err != nil {
				return err
			}
			metadataID = item.ID
		} else {
			var existing model.MetadataItem
			if err := tx.First(&existing, "id = ?", metadataID).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.MetadataItem{}).Where("id = ?", metadataID).
				Updates(metadataItemUpdates(item)).Error; err != nil {
				return err
			}
		}

		for _, identifier := range identifiers {
			if err := tx.Where(
				"metadata_id = ? AND provider = ? AND entity_kind = ? AND external_id <> ?",
				metadataID, identifier.Provider, identifier.EntityKind, identifier.ExternalID,
			).Delete(&model.MetadataIdentifier{}).Error; err != nil {
				return err
			}
		}
		for i := range identifiers {
			identifiers[i].MetadataID = metadataID
		}
		if len(identifiers) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "provider"}, {Name: "entity_kind"}, {Name: "external_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"metadata_id": metadataID, "updated_at": time.Now(), "deleted_at": nil,
				}),
			}).Create(&identifiers).Error; err != nil {
				return err
			}
		}
		return tx.First(&saved, "id = ?", metadataID).Error
	})
	if err != nil {
		return nil, err
	}
	if r.view != nil {
		r.view.reindexMetadataBestEffort(ctx, saved.ID)
	}
	return &saved, nil
}

// UpsertCanonicalWithMerge 仅在调用方确认标识来自明确映射时归并冲突实体。
func (r *MetadataRepository) UpsertCanonicalWithMerge(ctx context.Context, item *model.MetadataItem, identifiers []model.MetadataIdentifier, preferredID string, allowMerge bool) (*model.MetadataItem, error) {
	if !allowMerge {
		return r.UpsertCanonical(ctx, item, identifiers, preferredID)
	}
	if err := normalizeMetadataIdentifiers(identifiers); err != nil {
		return nil, err
	}
	preferredID = strings.TrimSpace(preferredID)
	var saved *model.MetadataItem
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		targetID := preferredID
		resolved := map[string]struct{}{}
		if preferredID != "" {
			resolved[preferredID] = struct{}{}
		}
		for _, identifier := range identifiers {
			var existing model.MetadataIdentifier
			err := tx.Where("provider = ? AND entity_kind = ? AND external_id = ?",
				identifier.Provider, identifier.EntityKind, identifier.ExternalID).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			resolved[existing.MetadataID] = struct{}{}
			if targetID == "" || identifier.Provider == "tmdb" {
				targetID = existing.MetadataID
			}
		}
		if targetID == "" {
			var err error
			saved, err = (&MetadataRepository{db: tx}).UpsertCanonical(ctx, item, identifiers, "")
			return err
		}
		merged := false
		for sourceID := range resolved {
			if sourceID == targetID {
				continue
			}
			if err := mergeMetadataGraph(tx, sourceID, targetID); err != nil {
				return err
			}
			merged = true
		}
		upsertItem := item
		if merged {
			var target model.MetadataItem
			if err := tx.First(&target, "id = ?", targetID).Error; err != nil {
				return err
			}
			if target.Source != "local" && target.Source != "manual" {
				upsertItem = &target
			}
		}
		var err error
		saved, err = (&MetadataRepository{db: tx}).UpsertCanonical(ctx, upsertItem, identifiers, targetID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if r.view != nil && saved != nil {
		r.view.reindexMetadataBestEffort(ctx, saved.ID)
	}
	return saved, nil
}

// Merge 将 source 的全部引用迁移到 target，并物理删除无引用的 source。
func (r *MetadataRepository) Merge(ctx context.Context, sourceID, targetID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return mergeMetadataGraph(tx, strings.TrimSpace(sourceID), strings.TrimSpace(targetID))
	})
}

func (r *MetadataRepository) UpsertSeason(ctx context.Context, item *model.MetadataItem) (*model.MetadataItem, error) {
	return r.UpsertSeasonWithIdentifiers(ctx, item, nil)
}

func (r *MetadataRepository) UpsertSeasonWithIdentifiers(ctx context.Context, item *model.MetadataItem, identifiers []model.MetadataIdentifier) (*model.MetadataItem, error) {
	if item == nil || item.Kind != model.MetadataKindSeason {
		return nil, errors.New("season metadata is required")
	}
	if err := validateMetadataItem(item); err != nil {
		return nil, err
	}
	var existing model.MetadataItem
	err := r.db.WithContext(ctx).
		Where("kind = ? AND parent_id = ? AND season_num = ?", model.MetadataKindSeason, *item.ParentID, item.SeasonNum).
		First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	preferredID := ""
	if err == nil {
		preferredID = existing.ID
	}
	return r.UpsertCanonicalWithMerge(ctx, item, identifiers, preferredID, len(identifiers) > 0)
}

func (r *MetadataRepository) UpsertEpisode(ctx context.Context, item *model.MetadataItem) (*model.MetadataItem, error) {
	return r.UpsertEpisodeWithIdentifiers(ctx, item, nil)
}

func (r *MetadataRepository) UpsertEpisodeWithIdentifiers(ctx context.Context, item *model.MetadataItem, identifiers []model.MetadataIdentifier) (*model.MetadataItem, error) {
	if item == nil || item.Kind != model.MetadataKindEpisode {
		return nil, errors.New("episode metadata is required")
	}
	if err := validateMetadataItem(item); err != nil {
		return nil, err
	}
	var existing model.MetadataItem
	err := r.db.WithContext(ctx).
		Where("kind = ? AND parent_id = ? AND episode_num = ?", model.MetadataKindEpisode, *item.ParentID, item.EpisodeNum).
		First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	preferredID := ""
	if err == nil {
		preferredID = existing.ID
	}
	return r.UpsertCanonicalWithMerge(ctx, item, identifiers, preferredID, len(identifiers) > 0)
}

func (r *MetadataRepository) Update(ctx context.Context, item *model.MetadataItem) error {
	if err := validateMetadataItem(item); err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Save(item).Error; err != nil {
		return err
	}
	if r.view != nil {
		r.view.reindexMetadataBestEffort(ctx, item.ID)
	}
	return nil
}

func (r *MetadataRepository) UpsertIdentifiers(ctx context.Context, metadataID string, identifiers []model.MetadataIdentifier) error {
	if len(identifiers) == 0 {
		return nil
	}
	if err := normalizeMetadataIdentifiers(identifiers); err != nil {
		return err
	}
	for i := range identifiers {
		identifiers[i].MetadataID = metadataID
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range identifiers {
			var existing model.MetadataIdentifier
			err := tx.Unscoped().Where("provider = ? AND entity_kind = ? AND external_id = ?",
				identifiers[i].Provider, identifiers[i].EntityKind, identifiers[i].ExternalID).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if err := tx.Create(&identifiers[i]).Error; err != nil {
					return err
				}
			case err != nil:
				return err
			case existing.MetadataID != metadataID:
				return fmt.Errorf("%s %s id %s already belongs to another metadata item", identifiers[i].Provider, identifiers[i].EntityKind, identifiers[i].ExternalID)
			default:
				if err := tx.Unscoped().Model(&existing).Updates(map[string]any{"deleted_at": nil, "updated_at": time.Now()}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r *MetadataRepository) ListIdentifiers(ctx context.Context, metadataID string) ([]model.MetadataIdentifier, error) {
	var identifiers []model.MetadataIdentifier
	err := r.db.WithContext(ctx).Where("metadata_id = ?", metadataID).Order("provider, entity_kind, external_id").Find(&identifiers).Error
	return identifiers, err
}

// ReplaceIdentifier replaces one provider identity while preserving the global
// provider/kind/external-ID uniqueness contract.
func (r *MetadataRepository) ReplaceIdentifier(ctx context.Context, metadataID, provider, entityKind, externalID string) error {
	metadataID = strings.TrimSpace(metadataID)
	identifier := model.MetadataIdentifier{Provider: provider, EntityKind: entityKind, ExternalID: externalID}
	if strings.TrimSpace(externalID) == "" {
		identifier.ExternalID = "1"
	}
	if err := normalizeMetadataIdentifier(&identifier); err != nil {
		return err
	}
	provider, entityKind = identifier.Provider, identifier.EntityKind
	externalID = strings.TrimSpace(externalID)
	if externalID != "" {
		externalID = identifier.ExternalID
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("metadata_id = ? AND provider = ? AND entity_kind = ?", metadataID, provider, entityKind)
		if externalID == "" {
			return q.Delete(&model.MetadataIdentifier{}).Error
		}
		if err := q.Where("external_id <> ?", externalID).Delete(&model.MetadataIdentifier{}).Error; err != nil {
			return err
		}
		var existing model.MetadataIdentifier
		err := tx.Unscoped().Where("provider = ? AND entity_kind = ? AND external_id = ?", provider, entityKind, externalID).First(&existing).Error
		if err == nil {
			if !existing.DeletedAt.Valid && existing.MetadataID != metadataID {
				return fmt.Errorf("%s %s id %s already belongs to another metadata item", provider, entityKind, externalID)
			}
			return tx.Unscoped().Model(&existing).Updates(map[string]any{
				"metadata_id": metadataID, "deleted_at": nil, "updated_at": time.Now(),
			}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&model.MetadataIdentifier{
			MetadataID: metadataID, Provider: provider, EntityKind: entityKind, ExternalID: externalID,
		}).Error
	})
}

// InvalidateTMDbIdentifier 移除失效作品标识，并将关联电影或整部电视剧交回统一刮削队列。
func (r *MetadataRepository) InvalidateTMDbIdentifier(ctx context.Context, metadataID, entityKind, externalID string) (int64, error) {
	identifier := model.MetadataIdentifier{Provider: "tmdb", EntityKind: entityKind, ExternalID: externalID}
	if err := normalizeMetadataIdentifier(&identifier); err != nil {
		return 0, err
	}
	tmdbID, err := strconv.Atoi(identifier.ExternalID)
	if err != nil {
		return 0, err
	}
	var reset int64
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", strings.TrimSpace(metadataID)).Error; err != nil {
			return err
		}
		if item.Source != "tmdb" || item.Kind != identifier.EntityKind {
			return nil
		}
		deleted := tx.Where("metadata_id = ? AND provider = ? AND entity_kind = ? AND external_id = ?",
			item.ID, identifier.Provider, identifier.EntityKind, identifier.ExternalID).Delete(&model.MetadataIdentifier{})
		if deleted.Error != nil || deleted.RowsAffected == 0 {
			return deleted.Error
		}

		media := tx.Model(&model.Media{})
		if item.Kind == model.MetadataKindSeries {
			media = media.Where("series_hint = ?", item.ID)
		} else {
			media = media.Where("metadata_id = ?", item.ID)
		}
		if err := media.Where("lookup_tmdb_id = ?", tmdbID).Update("lookup_tmdb_id", 0).Error; err != nil {
			return err
		}
		media = tx.Model(&model.Media{})
		if item.Kind == model.MetadataKindSeries {
			media = media.Where("series_hint = ?", item.ID)
		} else {
			media = media.Where("metadata_id = ?", item.ID)
		}
		result := media.Updates(map[string]any{"scrape_status": "pending", "scrape_trigger": "event", "scrape_error": ""})
		reset = result.RowsAffected
		return result.Error
	})
	return reset, err
}

func (r *MetadataRepository) DB() *gorm.DB {
	return r.db
}

func metadataItemUpdates(item *model.MetadataItem) map[string]any {
	return map[string]any{
		"kind": item.Kind, "parent_id": item.ParentID,
		"season_num": item.SeasonNum, "episode_num": item.EpisodeNum,
		"title": item.Title, "original_name": item.OriginalName,
		"overview": item.Overview, "rating": item.Rating, "year": item.Year, "release_date": item.ReleaseDate,
		"runtime_sec": item.RuntimeSec,
		"languages":   item.Languages, "countries": item.Countries, "genres": item.Genres,
		"nsfw": item.NSFW, "source": item.Source, "updated_at": time.Now(),
	}
}

func validateMetadataItem(item *model.MetadataItem) error {
	if item == nil {
		return errors.New("metadata item is required")
	}
	switch item.Kind {
	case model.MetadataKindSeason:
		if item.ParentID == nil || strings.TrimSpace(*item.ParentID) == "" || item.SeasonNum < 0 || item.EpisodeNum != 0 {
			return errors.New("season parent, non-negative season number, and zero episode number are required")
		}
	case model.MetadataKindEpisode:
		if item.ParentID == nil || strings.TrimSpace(*item.ParentID) == "" || item.SeasonNum != 0 || item.EpisodeNum <= 0 {
			return errors.New("episode parent, zero season number, and positive episode number are required")
		}
	case model.MetadataKindMovie, model.MetadataKindSeries:
		if item.ParentID != nil || item.SeasonNum != 0 || item.EpisodeNum != 0 {
			return errors.New("movie and series metadata cannot have episode identity")
		}
	default:
		return fmt.Errorf("unsupported metadata kind %q", item.Kind)
	}
	return nil
}

func validateMetadataParent(tx *gorm.DB, item *model.MetadataItem) error {
	if item.ParentID == nil {
		return nil
	}
	var parent model.MetadataItem
	if err := tx.First(&parent, "id = ?", strings.TrimSpace(*item.ParentID)).Error; err != nil {
		return fmt.Errorf("metadata parent not found: %w", err)
	}
	want := model.MetadataKindSeries
	if item.Kind == model.MetadataKindEpisode {
		want = model.MetadataKindSeason
	}
	if parent.Kind != want {
		return fmt.Errorf("%s metadata parent must be %s", item.Kind, want)
	}
	return nil
}

func normalizeMetadataIdentifiers(identifiers []model.MetadataIdentifier) error {
	for i := range identifiers {
		if err := normalizeMetadataIdentifier(&identifiers[i]); err != nil {
			return err
		}
	}
	return nil
}

func normalizeMetadataIdentifier(identifier *model.MetadataIdentifier) error {
	if identifier == nil {
		return errors.New("metadata identifier is required")
	}
	identifier.Provider = strings.ToLower(strings.TrimSpace(identifier.Provider))
	identifier.EntityKind = strings.ToLower(strings.TrimSpace(identifier.EntityKind))
	identifier.ExternalID = strings.TrimSpace(identifier.ExternalID)
	switch identifier.Provider {
	case "tmdb", "douban", "bangumi", "thetvdb":
		if identifier.ExternalID == "" {
			return fmt.Errorf("%s external id is required", identifier.Provider)
		}
		if value, err := strconv.ParseUint(identifier.ExternalID, 10, 64); err == nil {
			if value == 0 {
				return fmt.Errorf("invalid %s external id %q", identifier.Provider, identifier.ExternalID)
			}
			identifier.ExternalID = strconv.FormatUint(value, 10)
		}
	case "local", "adult", "imdb":
		if identifier.ExternalID == "" {
			return fmt.Errorf("%s external id is required", identifier.Provider)
		}
	default:
		return fmt.Errorf("unsupported metadata provider %q", identifier.Provider)
	}
	switch identifier.EntityKind {
	case model.MetadataKindMovie, model.MetadataKindSeries, model.MetadataKindSeason, model.MetadataKindEpisode:
	default:
		return fmt.Errorf("unsupported metadata identifier kind %q", identifier.EntityKind)
	}
	return nil
}
