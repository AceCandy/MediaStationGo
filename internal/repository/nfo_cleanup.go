package repository

import (
	"slices"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// lockNFOLibrary 在文件及条目行锁之前串行化同库入库与清理，避免最后版本删除和新版本入库冲突。
func lockNFOLibrary(tx *gorm.DB, libraryID string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "nfo-library:"+libraryID).Error
}

// pruneEmptyNFOItems 仅清理候选及祖先的空叶；用户状态、播放事件及共享图片保持独立。
func pruneEmptyNFOItems(tx *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var targets []string
	if err := tx.Raw(`WITH RECURSIVE ancestors AS (
 SELECT id,parent_id FROM nfo_items WHERE id = ANY(?)
 UNION SELECT p.id,p.parent_id FROM nfo_items p JOIN ancestors a ON p.id=a.parent_id
) SELECT id FROM ancestors`, &ids).Scan(&targets).Error; err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	for {
		result := tx.Where("id = ANY(?)", &targets).
			Where("NOT EXISTS (SELECT 1 FROM nfo_media_bindings b WHERE b.item_id=nfo_items.id)").
			Where("NOT EXISTS (SELECT 1 FROM nfo_items child WHERE child.parent_id=nfo_items.id)").
			Delete(&model.NFOItem{})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
	}
}

// DeleteMedia 原子删除筛选出的媒体及无文件 NFO 条目；库/根清理可同时处理该库既有孤儿。
// query 只携带内部媒体筛选条件；事务入口保留调用方的上下文和现有事务。
func DeleteMedia(query *gorm.DB, libraryIDs ...string) (int64, error) {
	var removed int64
	err := query.Session(&gorm.Session{NewDB: true}).Transaction(func(tx *gorm.DB) error {
		scope := func() *gorm.DB { return tx.Model(&model.Media{}).Where(query) }
		var libraries []string
		if err := scope().Where("catalog_source = ?", model.CatalogSourceNFO).
			Distinct("library_id").Order("library_id").Pluck("library_id", &libraries).Error; err != nil {
			return err
		}
		libraries = append(libraries, libraryIDs...)
		if len(libraries) == 0 {
			result := scope().Delete(&model.Media{})
			removed = result.RowsAffected
			return result.Error
		}
		// 多库批量删除按固定顺序取锁，与单库入库保持相同锁序。
		slices.Sort(libraries)
		libraries = slices.Compact(libraries)
		for _, id := range libraries {
			if err := lockNFOLibrary(tx, id); err != nil {
				return err
			}
		}
		var ids []string
		if err := tx.Model(&model.NFOMediaBinding{}).Where("media_id IN (?)", scope().Select("id")).
			Distinct("item_id").Pluck("item_id", &ids).Error; err != nil {
			return err
		}
		if len(libraryIDs) > 0 {
			var orphanIDs []string
			if err := tx.Model(&model.NFOItem{}).Where("library_id = ANY(?)", &libraryIDs).
				Where("NOT EXISTS (SELECT 1 FROM nfo_media_bindings b WHERE b.item_id=nfo_items.id)").
				Pluck("id", &orphanIDs).Error; err != nil {
				return err
			}
			ids = append(ids, orphanIDs...)
		}
		result := scope().Delete(&model.Media{})
		if result.Error != nil {
			return result.Error
		}
		removed = result.RowsAffected
		return pruneEmptyNFOItems(tx, ids)
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
