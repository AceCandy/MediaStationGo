package repository

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// NextEpisodeMediaIDs 只读取同一季中集号加一的全部可见文件，不跨季或跳过缺集。
func (r *MediaViewRepository) NextEpisodeMediaIDs(ctx context.Context, media *model.MediaView, filter MediaQueryFilter) ([]string, error) {
	if media == nil || media.MetadataKind != model.MetadataKindEpisode || media.SeasonID == "" || media.EpisodeNum <= 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Table("media AS m")
	switch media.CatalogSource {
	case model.TaskSystemHongGuo:
		episodes := r.db.WithContext(ctx).Table("hongguo_episodes").Select("id").
			Where("work_id = ? AND number = ?", strings.TrimPrefix(media.SeasonID, "hg-season-"), media.EpisodeNum+1)
		bindings := r.db.WithContext(ctx).Table("hongguo_media_bindings").Select("media_id").Where("episode_id IN (?)", episodes)
		q = q.Where("m.catalog_source = ? AND m.id IN (?)", model.TaskSystemHongGuo, bindings)
	case model.CatalogSourceNFO:
		episodes := r.db.WithContext(ctx).Table("nfo_items").Select("id").
			Where("parent_id = ? AND kind = 'episode' AND episode_num = ?", strings.TrimPrefix(media.SeasonID, "nfo-"), media.EpisodeNum+1)
		bindings := r.db.WithContext(ctx).Table("nfo_media_bindings").Select("media_id").Where("item_id IN (?)", episodes)
		q = q.Where("m.catalog_source = ? AND m.id IN (?)", model.CatalogSourceNFO, bindings)
	default:
		episodes := r.db.WithContext(ctx).Table("metadata_items").Select("id").
			Where("parent_id = ? AND kind = 'episode' AND episode_num = ?", media.SeasonID, media.EpisodeNum+1)
		q = q.Where("m.metadata_id IN (?)", episodes)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	var ids []string
	err := q.Order("m.id").Pluck("m.id", &ids).Error
	return ids, err
}
