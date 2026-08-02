package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// mergeMetadataGraph 先递归收拢层级与引用，最后删除源 metadata。
func mergeMetadataGraph(tx *gorm.DB, sourceID, targetID string) error {
	if sourceID == "" || targetID == "" {
		return errors.New("source and target metadata ids are required")
	}
	if sourceID == targetID {
		return nil
	}
	var source, target model.MetadataItem
	if err := tx.First(&source, "id = ?", sourceID).Error; err != nil {
		return err
	}
	if err := tx.First(&target, "id = ?", targetID).Error; err != nil {
		return err
	}
	if source.Kind != target.Kind {
		return errors.New("metadata merge kinds do not match")
	}
	switch source.Kind {
	case model.MetadataKindSeries:
		if err := mergeMetadataChildren(tx, source.ID, target.ID, model.MetadataKindSeason, "season_num"); err != nil {
			return err
		}
	case model.MetadataKindSeason:
		if err := mergeMetadataChildren(tx, source.ID, target.ID, model.MetadataKindEpisode, "episode_num"); err != nil {
			return err
		}
	}
	if err := tx.Model(&model.Media{}).Where("metadata_id = ?", source.ID).Update("metadata_id", target.ID).Error; err != nil {
		return err
	}
	if source.Kind == model.MetadataKindSeries {
		if err := tx.Model(&model.Media{}).Where("series_hint = ?", source.ID).Update("series_hint", target.ID).Error; err != nil {
			return err
		}
	}
	if err := mergeFavorites(tx, source.ID, target.ID); err != nil {
		return err
	}
	if err := mergePlaybackHistories(tx, source.ID, target.ID); err != nil {
		return err
	}
	if err := mergePlaylistItems(tx, source.ID, target.ID); err != nil {
		return err
	}
	if err := mergeMetadataArtwork(tx, source.ID, target.ID); err != nil {
		return err
	}
	if err := tx.Unscoped().Model(&model.MetadataIdentifier{}).Where("metadata_id = ?", source.ID).Update("metadata_id", target.ID).Error; err != nil {
		return err
	}
	return tx.Unscoped().Delete(&source).Error
}

func mergeMetadataChildren(tx *gorm.DB, sourceID, targetID, kind, identityColumn string) error {
	var children []model.MetadataItem
	if err := tx.Where("kind = ? AND parent_id = ?", kind, sourceID).Find(&children).Error; err != nil {
		return err
	}
	for i := range children {
		identity := children[i].SeasonNum
		if identityColumn == "episode_num" {
			identity = children[i].EpisodeNum
		}
		var targetChild model.MetadataItem
		err := tx.Where("kind = ? AND parent_id = ? AND "+identityColumn+" = ?", kind, targetID, identity).First(&targetChild).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&children[i]).Update("parent_id", targetID).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := mergeMetadataGraph(tx, children[i].ID, targetChild.ID); err != nil {
			return err
		}
	}
	return nil
}

func mergeFavorites(tx *gorm.DB, sourceID, targetID string) error {
	var rows []model.Favorite
	if err := tx.Unscoped().Where("metadata_id = ?", sourceID).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		var existing model.Favorite
		err := tx.Unscoped().Where("user_id = ? AND metadata_id = ?", rows[i].UserID, targetID).First(&existing).Error
		if err == nil {
			if existing.DeletedAt.Valid && !rows[i].DeletedAt.Valid {
				if err := tx.Unscoped().Model(&existing).Updates(map[string]any{"media_id": rows[i].MediaID, "deleted_at": nil}).Error; err != nil {
					return err
				}
			}
			if err := tx.Unscoped().Delete(&rows[i]).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Unscoped().Model(&rows[i]).Update("metadata_id", targetID).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergePlaybackHistories(tx *gorm.DB, sourceID, targetID string) error {
	var rows []model.PlaybackHistory
	if err := tx.Unscoped().Where("metadata_id = ?", sourceID).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		var existing model.PlaybackHistory
		err := tx.Unscoped().Where("user_id = ? AND metadata_id = ?", rows[i].UserID, targetID).First(&existing).Error
		if err == nil {
			if rows[i].WatchedAt.After(existing.WatchedAt) {
				if err := tx.Unscoped().Model(&existing).Updates(map[string]any{
					"media_id": rows[i].MediaID, "position_ms": rows[i].PositionMs, "duration_ms": rows[i].DurationMs,
					"watched_at": rows[i].WatchedAt, "completed": rows[i].Completed, "deleted_at": nil,
				}).Error; err != nil {
					return err
				}
			}
			if err := tx.Unscoped().Delete(&rows[i]).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Unscoped().Model(&rows[i]).Update("metadata_id", targetID).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergePlaylistItems(tx *gorm.DB, sourceID, targetID string) error {
	var rows []model.PlaylistItem
	if err := tx.Unscoped().Where("metadata_id = ?", sourceID).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		var existing model.PlaylistItem
		err := tx.Unscoped().Where("playlist_id = ? AND metadata_id = ?", rows[i].PlaylistID, targetID).First(&existing).Error
		if err == nil {
			if err := tx.Unscoped().Delete(&rows[i]).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Unscoped().Model(&rows[i]).Update("metadata_id", targetID).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergeMetadataArtwork(tx *gorm.DB, sourceID, targetID string) error {
	var rows []model.MetadataArtwork
	if err := tx.Unscoped().Where("metadata_id = ?", sourceID).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		var existing model.MetadataArtwork
		err := tx.Unscoped().Where("metadata_id = ? AND artwork_type = ?", targetID, rows[i].ArtworkType).First(&existing).Error
		if err == nil {
			if err := tx.Unscoped().Delete(&rows[i]).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Unscoped().Model(&rows[i]).Update("metadata_id", targetID).Error; err != nil {
			return err
		}
	}
	return nil
}
