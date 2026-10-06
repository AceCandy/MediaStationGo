package repository

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *MediaRepository) upsertHuangGuoAIMedia(ctx context.Context, m *model.Media) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var library model.Library
		if e := tx.Where("id = ? AND type = ?", m.LibraryID, model.LibraryTypeHuangGuoAI).Take(&library).Error; e != nil {
			return e
		}
		if m.MetadataID != "" {
			return errors.New("黄果 AI 媒体不能绑定旧资料")
		}
		if _, e := (&MediaRepository{db: tx}).upsert(ctx, m); e != nil {
			return e
		}
		var current model.Media
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", m.ID).Take(&current).Error; e != nil {
			return e
		}
		return bindHuangGuoAIMedia(tx, m)
	})
}

// bindHuangGuoAIMedia requires the original source ID and S01 coordinates, including movies' first file.
func bindHuangGuoAIMedia(tx *gorm.DB, m *model.Media) error {
	// 停用只阻止新绑定，保留既有文件与用户记录的身份。
	var enabled string
	if err := tx.Model(&model.Setting{}).Select("value").Where("key=?", "huangguoai.enabled").Scan(&enabled).Error; err != nil {
		return err
	}
	if enabled == "false" {
		return nil
	}
	pending := func(reason string) error {
		if e := tx.Where("media_id = ?", m.ID).Delete(&model.HuangGuoAIMediaBinding{}).Error; e != nil {
			return e
		}
		m.ScrapeStatus = "source_pending"
		m.ScrapeError = reason
		return tx.Model(m).Updates(map[string]any{"scrape_status": m.ScrapeStatus, "scrape_error": reason}).Error
	}
	var work model.HuangGuoAIWork
	err := tx.Where("source_id = ?", m.LookupCatalogID).Take(&work).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return pending("等待黄果 AI 作品资料或修正源 ID")
	}
	if err != nil {
		return err
	}
	if work.ProjectionError != "" {
		return pending("来源分类冲突，暂停绑定")
	}
	if m.SeasonNum != 1 || m.EpisodeNum < 1 {
		return pending("黄果 AI 文件必须使用 S01Exxx 坐标")
	}
	if work.Kind == model.MetadataKindMovie && m.EpisodeNum != 1 {
		return pending("黄果 AI 电影仅匹配首集文件")
	}
	var episode model.HuangGuoAIEpisode
	err = tx.Where("work_id = ? AND number = ?", work.ID, m.EpisodeNum).Take(&episode).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return pending("黄果 AI 资料没有对应真实集号")
	}
	if err != nil {
		return err
	}
	binding := model.HuangGuoAIMediaBinding{MediaID: m.ID, WorkID: work.ID, EpisodeID: episode.ID}
	if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "media_id"}}, DoUpdates: clause.AssignmentColumns([]string{"work_id", "episode_id"})}).Create(&binding).Error; err != nil {
		return err
	}
	m.ScrapeStatus = "matched"
	m.ScrapeError = ""
	return tx.Model(m).Updates(map[string]any{"scrape_status": "matched", "scrape_error": ""}).Error
}

func (r *HuangGuoAIRepository) RebindWork(ctx context.Context, id string) error {
	after := ""
	for {
		var rows []model.Media
		if err := r.db.WithContext(ctx).Where("catalog_source = ? AND lookup_catalog_id = ? AND id > ?", model.TaskSystemHuangGuoAI, id, after).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var current model.Media
				e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND catalog_source = ? AND lookup_catalog_id = ?", row.ID, model.TaskSystemHuangGuoAI, id).Take(&current).Error
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return nil
				}
				if e != nil {
					return e
				}
				return bindHuangGuoAIMedia(tx, &current)
			})
			if err != nil {
				return err
			}
			after = row.ID
		}
	}
}
