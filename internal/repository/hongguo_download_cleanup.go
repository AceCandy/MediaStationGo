package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RemoveUnavailableHongGuoDownloads 应用已确认的上游变化；nil detail 表示整部下架。
// 完成历史、可恢复文件和媒体库绑定优先于上游状态，不能随清理丢失。
func (r *HongGuoRepository) RemoveUnavailableHongGuoDownloads(ctx context.Context, owner model.HongGuoDownload, detail *hongguo.Work) ([]model.HongGuoDownload, error) {
	var removed []model.HongGuoDownload
	var work model.HongGuoWork
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&work, "source_id = ?", owner.SourceID).Error; err != nil {
			return err
		}
		var rows []model.HongGuoDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", owner.SourceID).Order("id").Find(&rows).Error; err != nil {
			return err
		}
		owned := false
		replace := false
		if detail != nil {
			for _, row := range rows {
				if row.Episode > 0 && row.Episode <= len(detail.VideoIDs) && detail.VideoIDs[row.Episode-1] != "" && row.VideoID != "" && row.VideoID != detail.VideoIDs[row.Episode-1] {
					replace = true
				}
			}
		}
		if replace {
			for _, row := range rows {
				if row.Status == "completed" || row.RawSize > 0 || row.SHA256 != "" {
					return errors.New("上游集数和分集对应关系已变化，已下载内容保留，请确认整部重下")
				}
			}
			var media int64
			if err := tx.Model(&model.Media{}).Where("catalog_source = ? AND lookup_catalog_id = ?", "hongguo", owner.SourceID).Count(&media).Error; err != nil {
				return err
			}
			if media > 0 {
				return errors.New("上游分集版本已变化，已有媒体库内容，请确认整部重下")
			}
			if err := tx.Model(&model.HongGuoMediaBinding{}).Where("work_id = ?", work.ID).Count(&media).Error; err != nil {
				return err
			}
			if media > 0 {
				return errors.New("上游分集版本已变化，已有媒体绑定，请确认整部重下")
			}
		}
		for _, row := range rows {
			if row.ID == owner.ID && row.LeaseToken == owner.LeaseToken && row.Status == "downloading" && row.LeaseUntil != nil && row.LeaseUntil.After(time.Now()) {
				owned = true
			}
		}
		if !owned {
			return errors.New("下载清理执行权已失效")
		}
		for _, row := range rows {
			if row.Status == "completed" || row.RawSize > 0 || row.SHA256 != "" {
				continue
			}
			if detail != nil && !replace && row.Episode > 0 && row.Episode <= len(detail.VideoIDs) && detail.VideoIDs[row.Episode-1] != "" {
				continue
			}
			if err := tx.Delete(&row).Error; err != nil {
				return err
			}
			removed = append(removed, row)
		}
		if detail != nil {
			var placement model.HongGuoDownloadWork
			if err := tx.First(&placement, "source_id = ?", owner.SourceID).Error; err != nil {
				return err
			}
			for i, videoID := range detail.VideoIDs {
				if videoID == "" {
					continue
				}
				episode := model.HongGuoEpisode{WorkID: work.ID, Number: i + 1, SourceVideoID: videoID}
				conflict := clause.OnConflict{DoNothing: true}
				if replace {
					conflict = clause.OnConflict{Columns: []clause.Column{{Name: "work_id"}, {Name: "number"}}, DoUpdates: clause.AssignmentColumns([]string{"source_video_id"})}
				}
				if err := tx.Clauses(conflict).Create(&episode).Error; err != nil {
					return err
				}
				row := model.HongGuoDownload{SourceID: owner.SourceID, Episode: i + 1, VideoID: videoID, Title: placement.Title, Root: placement.Root, RelativePath: filepath.Join(placement.Directory, "Season 01", fmt.Sprintf("S01E%03d.mp4", i+1)), Status: "queued"}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
					return err
				}
			}
			// 已下载或已绑定分集保持身份；其余过期尾集不再被手动入队补回。
			protected := tx.Model(&model.HongGuoDownload{}).Select("episode").Where("source_id = ?", owner.SourceID)
			bound := tx.Model(&model.HongGuoMediaBinding{}).Select("episode_id").Where("episode_id IS NOT NULL")
			available := make([]int, 0, len(detail.VideoIDs))
			for i, id := range detail.VideoIDs {
				if id != "" {
					available = append(available, i+1)
				}
			}
			if err := tx.Where("work_id = ? AND number <> ALL(?) AND number NOT IN (?) AND id NOT IN (?)", work.ID, &available, protected, bound).Delete(&model.HongGuoEpisode{}).Error; err != nil {
				return err
			}
			return tx.Model(&work).Updates(map[string]any{"episode_count": detail.EpisodeCount, "total_episodes": detail.TotalEpisodes, "accessible_episodes": detail.AccessibleEpisodes, "update_text": detail.UpdateText, "completed": detail.Completed}).Error
		}
		if len(removed) != len(rows) {
			return nil
		}
		var media int64
		if err := tx.Model(&model.Media{}).Where("catalog_source = ? AND lookup_catalog_id = ?", "hongguo", owner.SourceID).Count(&media).Error; err != nil || media > 0 {
			return err
		}
		if err := tx.Model(&model.HongGuoMediaBinding{}).Where("work_id = ?", work.ID).Count(&media).Error; err != nil || media > 0 {
			return err
		}
		// 显式清理作品从属记录；人物和用户观看状态不属于作品的生命周期。
		for _, target := range []any{&model.HongGuoEpisode{}, &model.HongGuoCredit{}, &model.HongGuoSnapshot{}, &model.HongGuoArtwork{}} {
			if err := tx.Where("work_id = ?", work.ID).Delete(target).Error; err != nil {
				return err
			}
		}
		for _, target := range []any{&model.HongGuoArtwork{}, &model.HongGuoDiscovery{}, &model.HongGuoRankEntry{}, &model.HongGuoSyncFailure{}, &model.HongGuoDownloadWork{}} {
			if err := tx.Where("source_id = ?", owner.SourceID).Delete(target).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&work).Error
	})
	if err != nil {
		return nil, err
	}
	r.refreshSearchWork(ctx, work.ID, work.RelatedAlbumID)
	return removed, nil
}
