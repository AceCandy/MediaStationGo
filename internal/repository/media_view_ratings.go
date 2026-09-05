package repository

import (
	"encoding/json"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

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
