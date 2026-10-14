package repository

import (
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// saveTMDbInventorySubject 新坐标保存摘要，已有条目只更新目录与活跃状态，不推进正文时间。
func saveTMDbInventorySubject(tx *gorm.DB, kind string, parent *string, number *int, o tmdbObject, quality int, at time.Time) (*model.TMDbSubject, error) {
	var row model.TMDbSubject
	err := tx.Where("kind=? AND tmdb_id=?", kind, tmdbInt(o, "id")).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return saveTMDbSubject(tx, kind, parent, number, o, quality, at)
	}
	if err != nil {
		return nil, err
	}
	if parent != nil && (row.ParentID == nil || *row.ParentID != *parent || row.Number == nil || number == nil || *row.Number != *number) {
		return nil, errors.New("TMDb parent conflict")
	}
	if row.FetchedAt != nil && row.FetchedAt.After(at) {
		return &row, nil
	}
	updates := map[string]any{}
	if status := tmdbString(o, "status"); status != "" && status != row.Status {
		row.Status = status
		updates["status"] = status
	}
	fields := tmdbObject{}
	for _, key := range []string{"number_of_seasons", "number_of_episodes", "episode_count", "last_air_date", "last_episode_to_air", "next_episode_to_air", "in_production"} {
		if value, ok := o[key]; ok {
			fields[key] = value
		}
	}
	merged := mergeTMDbFields(row.Fields, tmdbJSON(fields), false)
	if merged != mergeTMDbFields(row.Fields, "{}", false) {
		row.Fields = merged
		updates["fields"] = merged
	}
	if len(updates) > 0 {
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return &row, nil
}
