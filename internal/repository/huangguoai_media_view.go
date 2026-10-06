package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

const huangGuoAIWorkIdentitySQL = `CASE WHEN w.kind='series' THEN 'hga-group-' || w.source_id ELSE 'hga-work-' || w.id END`
const huangGuoAIReadyWorkSQL = `w.projection_error=''`

func (r *MediaViewRepository) huangGuoAIFileScope(ctx context.Context, filter MediaQueryFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("media m").Joins("JOIN huangguoai_media_bindings b ON b.media_id=m.id").Joins("JOIN huangguoai_works w ON w.id=b.work_id").Joins("JOIN huangguoai_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id").Joins("LEFT JOIN huangguoai_artworks a ON a.work_id=w.id AND a.local_key<>''").Joins("LEFT JOIN media_probe_metadata pm ON pm.media_id=m.id").Where("m.catalog_source='huangguoai' AND " + huangGuoAIReadyWorkSQL).Where("w.kind='series' OR ep.number=1")
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id=ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id<>ALL(?)", &filter.HiddenLibraryIDs)
	}
	if filter.MissingPoster {
		q = q.Where("a.id IS NULL")
	}
	if filter.MissingChineseTitle {
		q = q.Where("w.title !~ '[㐀-䶿一-鿿豈-﫿]'")
	}
	return q
}
func (r *MediaViewRepository) huangGuoAIViewsByIDs(ctx context.Context, ids []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var sourceIDs []string
	if e := r.db.WithContext(ctx).Model(&model.Media{}).Where("id=ANY(?) AND catalog_source='huangguoai'", &ids).Pluck("id", &sourceIDs).Error; e != nil {
		return nil, e
	}
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	rows := []model.MediaView{}
	err := r.huangGuoAIFileScope(ctx, filter).Where("m.id=ANY(?)", &sourceIDs).Select(`m.*,
 CASE WHEN w.kind='movie' THEN 'hga-work-' || w.id ELSE 'hga-episode-' || ep.id END AS view_catalog_item_id,
 CASE WHEN w.kind='series' THEN 'hga-group-' || w.source_id ELSE '' END AS view_series_id,
 CASE WHEN w.kind='series' THEN w.title ELSE '' END AS view_series_title,
 CASE WHEN w.kind='series' THEN 'hga-season-' || w.id ELSE '' END AS view_season_id,
 CASE WHEN w.kind='series' THEN '第' || ep.number || '集' ELSE w.title END AS view_title,
 w.overview AS view_overview,w.rating AS view_rating,
 CASE WHEN w.kind='series' THEN 1 ELSE 0 END AS view_season_num,
 CASE WHEN w.kind='series' THEN ep.number ELSE 0 END AS view_episode_num,
 CASE WHEN w.kind='series' THEN 'episode' ELSE 'movie' END AS view_metadata_kind,
 'huangguoai' AS view_metadata_source,COALESCE(a.id,'') AS view_poster_asset_id,
 w.latest_media_added_at,COALESCE(pm.duration_ms,0) AS view_probe_duration_ms,
 COALESCE(pm.size_bytes,0) AS view_probe_size_bytes,COALESCE(pm.container,'') AS view_probe_container,
 COALESCE(pm.width,0) AS view_probe_width,COALESCE(pm.height,0) AS view_probe_height,
 COALESCE(pm.video_codec,'') AS view_probe_video_codec,COALESCE(pm.audio_codec,'') AS view_probe_audio_codec`).Scan(&rows).Error
	for i := range rows {
		rows[i].Normalize()
		if rows[i].PosterAssetID != "" {
			rows[i].PosterURL = "/api/catalogs/huangguoai/artwork/" + rows[i].PosterAssetID
			if rows[i].MetadataKind == model.MetadataKindEpisode {
				rows[i].BackdropURL = rows[i].PosterURL
			}
		}
	}
	return rows, err
}
func (r *MediaViewRepository) HuangGuoAIItemsViews(ctx context.Context, itemIDs []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(itemIDs) == 0 {
		return []model.MediaView{}, nil
	}
	var workIDs, episodeIDs, sourceIDs []string
	for _, id := range itemIDs {
		if v, ok := strings.CutPrefix(id, "hga-work-"); ok {
			workIDs = append(workIDs, v)
		} else if v, ok := strings.CutPrefix(id, "hga-episode-"); ok {
			episodeIDs = append(episodeIDs, v)
		} else if v, ok := strings.CutPrefix(id, "hga-group-"); ok {
			sourceIDs = append(sourceIDs, v)
		} else if v, ok := strings.CutPrefix(id, "hga-season-"); ok {
			workIDs = append(workIDs, v)
		}
	}
	var ids []string
	err := r.huangGuoAIFileScope(ctx, filter).Where("w.id=ANY(?) OR ep.id=ANY(?) OR w.source_id=ANY(?)", &workIDs, &episodeIDs, &sourceIDs).Where(`CASE WHEN w.kind='movie' THEN 'hga-work-' || w.id ELSE 'hga-episode-' || ep.id END = ANY(?) OR ('hga-group-' || w.source_id = ANY(?) AND w.kind='series') OR ('hga-season-' || w.id = ANY(?) AND w.kind='series')`, &itemIDs, &itemIDs, &itemIDs).Order("ep.number,m.id").Pluck("m.id", &ids).Error
	if err != nil {
		return nil, err
	}
	return r.huangGuoAIViewsByIDs(ctx, ids, filter)
}
func (r *MediaViewRepository) HuangGuoAIMediaPage(ctx context.Context, id string, page, size int, filter MediaQueryFilter) ([]model.MediaView, int64, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	q := r.huangGuoAIFileScope(ctx, filter).Where("w.source_id=?", id)
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	if err := q.Order("ep.number,m.id").Offset((page-1)*size).Limit(size).Pluck("m.id", &ids).Error; err != nil {
		return nil, 0, err
	}
	rows, err := r.huangGuoAIViewsByIDs(ctx, ids, filter)
	if err == nil && rows == nil {
		rows = []model.MediaView{}
	}
	return rows, count, err
}
