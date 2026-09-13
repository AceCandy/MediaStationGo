package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoPendingMedia 是管理员排查未绑定文件的只读投影，不包含 STRM 目标 URL。
type HongGuoPendingMedia struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Reason   string `json:"reason"`
}

func (r *HongGuoRepository) PendingMedia(ctx context.Context, page int) ([]HongGuoPendingMedia, int64, error) {
	if page < 1 || page > 1000000 {
		return nil, 0, errors.New("分页参数无效")
	}
	q := r.db.WithContext(ctx).Table("media m").Where("m.catalog_source = 'hongguo' AND NOT EXISTS (SELECT 1 FROM hongguo_media_bindings b WHERE b.media_id = m.id)")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []HongGuoPendingMedia{}
	err := q.Select("m.id,m.lookup_catalog_id AS source_id,m.scan_title AS title,m.path,m.scrape_error AS reason").Order("m.id").Offset((page - 1) * 50).Limit(50).Scan(&rows).Error
	return rows, total, err
}

func (r *MediaRepository) upsertHongGuoMedia(ctx context.Context, m *model.Media) error {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Path)), "cloud://") {
		return errCloudMediaPathUnsupported
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var library model.Library
		if err := tx.First(&library, "id = ? AND type = ?", m.LibraryID, model.LibraryTypeHongGuo).Error; err != nil {
			return err
		}
		var existing model.Media
		err := tx.Where("path = ?", m.Path).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing.ID != "" && (existing.MetadataID != "" || existing.CatalogSource != model.TaskSystemHongGuo) {
			return errors.New("不能把现有体系文件自动改绑到红果")
		}
		if m.MetadataID != "" {
			return errors.New("红果媒体不能携带旧资料绑定")
		}
		if _, err := (&MediaRepository{db: tx}).upsert(ctx, m); err != nil {
			return err
		}
		return bindHongGuoMedia(tx, m)
	})
}

// bindHongGuoMedia 仅按作品 ID 与源季集坐标绑定，未知资料保留待匹配文件。
func bindHongGuoMedia(tx *gorm.DB, m *model.Media) error {
	if enabled, err := New(tx).Setting.Get(tx.Statement.Context, "hongguo.enabled"); err != nil {
		return err
	} else if enabled == "false" {
		return errors.New("HongGuoDB 已停用，文件绑定保持不变")
	}
	pending := func(reason string) error {
		if err := tx.Where("media_id = ?", m.ID).Delete(&model.HongGuoMediaBinding{}).Error; err != nil {
			return err
		}
		m.ScrapeStatus, m.ScrapeError = "source_pending", reason
		return tx.Model(m).Updates(map[string]any{"scrape_status": m.ScrapeStatus, "scrape_error": reason}).Error
	}
	var work model.HongGuoWork
	err := tx.Where("source_id = ?", m.LookupCatalogID).First(&work).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return pending("等待对应红果资料或修正来源 ID")
	}
	if err != nil {
		return err
	}
	binding := model.HongGuoMediaBinding{MediaID: m.ID, WorkID: work.ID}
	if work.Kind == model.MetadataKindSeries {
		if m.SeasonNum != 1 || m.EpisodeNum < 1 {
			return pending("红果源作品使用 S01Exxx，聚合季号不改变源文件坐标")
		}
		var episode model.HongGuoEpisode
		if err := tx.Where("work_id = ? AND number = ?", work.ID, m.EpisodeNum).First(&episode).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return pending("红果资料尚无对应集号")
		} else if err != nil {
			return err
		}
		binding.EpisodeID = &episode.ID
	} else if m.EpisodeNum > 1 {
		return pending("全一集电影不能匹配到其他集号")
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "media_id"}}, DoUpdates: clause.AssignmentColumns([]string{"work_id", "episode_id"})}).Create(&binding).Error; err != nil {
		return err
	}
	m.ScrapeStatus, m.ScrapeError = "matched", ""
	return tx.Model(m).Updates(map[string]any{"scrape_status": "matched", "scrape_error": ""}).Error
}

// RebindWork 在资料刷新后重验文件，包含待匹配文件及分类改变的已有绑定。
func (r *HongGuoRepository) RebindWork(ctx context.Context, sourceID string) error {
	after := ""
	for {
		var rows []model.Media
		if err := r.db.WithContext(ctx).Where("catalog_source = ? AND lookup_catalog_id = ? AND id > ?", model.TaskSystemHongGuo, sourceID, after).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var current model.Media
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND catalog_source = ? AND lookup_catalog_id = ?", row.ID, model.TaskSystemHongGuo, sourceID).First(&current).Error; errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				} else if err != nil {
					return err
				}
				return bindHongGuoMedia(tx, &current)
			}); err != nil {
				return err
			}
			after = row.ID
		}
	}
}
