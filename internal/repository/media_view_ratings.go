package repository

import (
	"context"

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
	var rows []struct {
		MetadataID string
		Rating     float32
	}
	if err := r.db.Session(&gorm.Session{NewDB: true, Context: ctx}).Table("metadata_identifiers AS mid").Select("mid.metadata_id, ds.rating").
		Joins("JOIN metadata_items AS mi ON mi.id = mid.metadata_id AND mi.kind = mid.entity_kind").
		Joins("JOIN douban_subjects AS ds ON ds.douban_id = mid.external_id AND ds.entity_kind = COALESCE(NULLIF(mid.douban_entity_kind, ''), mid.entity_kind)").
		Where("(SELECT COUNT(*) FROM metadata_identifiers other WHERE other.metadata_id = mid.metadata_id AND other.provider = 'douban' AND other.entity_kind = mid.entity_kind) = 1").
		Where("mid.provider = 'douban' AND mid.metadata_id = ANY(?) AND ds.rating > 0 AND ds.rating <= 10", &metadataIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		ratings[row.MetadataID] = row.Rating
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
	ratings, err := (&MetadataRepository{db: db}).DoubanRatingsByMetadataIDs(db.Statement.Context, ids)
	if err != nil {
		return err
	}
	for i := range views {
		if views[i].DoubanID != "" {
			views[i].DoubanRating = ratings[views[i].MetadataID]
		}
	}
	return nil
}

// doubanSnapshotRating 保持已有仓储解析调用兼容。
func doubanSnapshotRating(payload string) float32 { return model.DoubanRating(payload) }
