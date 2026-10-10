package repository

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// DoubanRatingsByMetadataIDs 批量读取实体自身已关联豆瓣的快照评分，不继承父级或替代通用评分。
func (r *MetadataRepository) DoubanRatingsByMetadataIDs(ctx context.Context, metadataIDs []string) (map[string]float32, error) {
	ratings := map[string]float32{}
	metadataIDs = uniqueNonEmptyStrings(metadataIDs)
	if len(metadataIDs) == 0 {
		return ratings, nil
	}
	db := r.db.WithContext(ctx)
	linked := db.Table("metadata_identifiers AS identifier").Select("identifier.metadata_id").
		Joins("JOIN metadata_items AS metadata ON metadata.id = identifier.metadata_id AND metadata.kind = identifier.entity_kind").
		Where("identifier.provider = ? AND identifier.external_id <> '' AND identifier.metadata_id = ANY(?)", "douban", &metadataIDs)
	var snapshots []model.MetadataProviderSnapshot
	if err := db.Select("metadata_id", "payload").
		Where("provider = ? AND metadata_id IN (?)", "douban", linked).
		Find(&snapshots).Error; err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		if rating := doubanSnapshotRating(snapshot.Payload); rating > 0 {
			ratings[snapshot.MetadataID] = rating
		}
	}
	return ratings, nil
}

// attachMediaViewDoubanRatings 只为当前结果中已关联豆瓣的实体批量加载评分，不改变分页或通用评分。
func attachMediaViewDoubanRatings(db *gorm.DB, views []model.MediaView) error {
	ids := make([]string, 0, len(views))
	for i := range views {
		views[i].DoubanRating = 0
		if views[i].DoubanID != "" {
			ids = append(ids, views[i].MetadataID)
		}
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	var snapshots []model.MetadataProviderSnapshot
	if err := db.Select("metadata_id", "payload").
		Where("provider = ? AND metadata_id = ANY(?)", "douban", &ids).
		Find(&snapshots).Error; err != nil {
		return err
	}
	ratings := make(map[string]float32, len(snapshots))
	for _, snapshot := range snapshots {
		ratings[snapshot.MetadataID] = doubanSnapshotRating(snapshot.Payload)
	}
	for i := range views {
		if views[i].DoubanID != "" {
			views[i].DoubanRating = ratings[views[i].MetadataID]
		}
	}
	return nil
}

// doubanSnapshotRating 兼容移动端评分及历史 subject/data 包装；缺失或无效评分保持未知。
func doubanSnapshotRating(payload string) float32 {
	var subject map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &subject) != nil {
		return 0
	}
	for _, key := range []string{"subject", "data"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(subject[key], &nested) == nil && nested != nil {
			subject = nested
			break
		}
	}
	for _, key := range []string{"rate", "rating"} {
		raw := subject[key]
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil && nested != nil {
			raw = nested["value"]
			if len(raw) == 0 {
				raw = nested["score"]
			}
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			value, err := number.Float64()
			if err == nil && value > 0 && value <= 10 {
				return float32(value)
			}
		}
	}
	return 0
}
