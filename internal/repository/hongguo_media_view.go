package repository

import (
	"context"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// HongGuoLibraryCard 在媒体库中按人工聚合去重，源资料库列表仍保留每个作品。
type HongGuoLibraryCard struct {
	ID        string `json:"id"`
	SourceID  string `json:"source_id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	ArtworkID string `json:"artwork_id"`
}

func (r *MediaViewRepository) HongGuoLibraryCards(ctx context.Context, libraryID string, page int) ([]HongGuoLibraryCard, int64, error) {
	if page < 1 || page > 1000000 {
		return nil, 0, errors.New("分页参数无效")
	}
	q := r.db.WithContext(ctx).Table("media m").Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").Joins("JOIN hongguo_works w ON w.id = b.work_id").Joins("LEFT JOIN hongguo_group_members gm ON gm.work_id = w.id").Joins("LEFT JOIN hongguo_groups g ON g.id = gm.group_id").Joins("LEFT JOIN hongguo_artworks a ON a.work_id = w.id AND a.local_key <> ''").Where("m.library_id = ? AND m.catalog_source = 'hongguo'", libraryID).
		Select("COALESCE(g.id,w.id) AS id, COALESCE(g.title,w.title) AS title, w.kind, (ARRAY_AGG(w.source_id ORDER BY gm.season_number NULLS LAST,w.id))[1] AS source_id, COALESCE((ARRAY_AGG(a.id ORDER BY gm.season_number NULLS LAST,w.id) FILTER (WHERE a.id IS NOT NULL))[1],'') AS artwork_id").Group("COALESCE(g.id,w.id), COALESCE(g.title,w.title), w.kind")
	outer := r.db.WithContext(ctx).Table("(?) AS cards", q)
	var total int64
	if err := outer.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []HongGuoLibraryCard{}
	err := outer.Order("title, id").Offset((page - 1) * 50).Limit(50).Scan(&rows).Error
	return rows, total, err
}

// HongGuoMediaPage 按源作品分页返回已绑定文件，先应用库权限再分页。
func (r *MediaViewRepository) HongGuoMediaPage(ctx context.Context, sourceID string, page, size int, filter MediaQueryFilter) ([]model.MediaView, int64, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	q := r.db.WithContext(ctx).Table("media AS m").Joins("JOIN hongguo_media_bindings AS b ON b.media_id = m.id").Joins("JOIN hongguo_works AS w ON w.id = b.work_id").Where("m.catalog_source = ? AND w.source_id = ?", model.TaskSystemHongGuo, sourceID)
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	if err := q.Order("m.episode_num, m.id").Offset((page-1)*size).Limit(size).Pluck("m.id", &ids).Error; err != nil {
		return nil, 0, err
	}
	rows, err := r.FindByIDs(ctx, ids, filter)
	return rows, total, err
}

// HongGuoItemViews 仅解析电影或分集的逻辑身份，不把整剧作为可播放文件。
func (r *MediaViewRepository) HongGuoItemViews(ctx context.Context, id string, filter MediaQueryFilter) ([]model.MediaView, error) {
	return r.HongGuoItemsViews(ctx, []string{id}, filter)
}

// HongGuoItemsViews 批量加载当前页逻辑条目的文件版本。
func (r *MediaViewRepository) HongGuoItemsViews(ctx context.Context, itemIDs []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(itemIDs) == 0 {
		return []model.MediaView{}, nil
	}
	q := r.db.WithContext(ctx).Table("hongguo_media_bindings AS b").Joins("JOIN hongguo_works AS w ON w.id = b.work_id")
	q = q.Where("CASE WHEN w.kind = 'movie' AND b.episode_id IS NULL THEN 'hg-work-' || w.id WHEN w.kind = 'series' AND b.episode_id IS NOT NULL THEN 'hg-episode-' || b.episode_id ELSE '' END = ANY(?)", &itemIDs)
	var ids []string
	if err := q.Order("b.media_id").Pluck("b.media_id", &ids).Error; err != nil {
		return nil, err
	}
	return r.FindByIDs(ctx, ids, filter)
}

func (r *MediaViewRepository) hongGuoViewsByIDs(ctx context.Context, ids []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// 先检查文件归属，旧体系已命中查询无需访问红果表。
	var sourceIDs []string
	if err := r.db.WithContext(ctx).Model(&model.Media{}).Where("id = ANY(?) AND catalog_source = ?", &ids, model.TaskSystemHongGuo).Pluck("id", &sourceIDs).Error; err != nil {
		return nil, err
	}
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Table("media AS m").
		Joins("JOIN hongguo_media_bindings AS b ON b.media_id = m.id").
		Joins("JOIN hongguo_works AS w ON w.id = b.work_id").
		Joins("LEFT JOIN hongguo_episodes AS ep ON ep.id = b.episode_id AND ep.work_id = w.id").
		Joins("LEFT JOIN hongguo_group_members AS gm ON gm.work_id = w.id").
		Joins("LEFT JOIN hongguo_groups AS g ON g.id = gm.group_id").
		Joins("LEFT JOIN hongguo_artworks AS a ON a.work_id = w.id AND a.local_key <> ''").
		Joins("LEFT JOIN media_probe_metadata AS pm ON pm.media_id = m.id").
		Where("m.id = ANY(?)", &sourceIDs)
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if filter.MissingPoster {
		q = q.Where("a.id IS NULL")
	}
	if filter.MissingChineseTitle {
		q = q.Where("w.title !~ '[㐀-䶿一-鿿豈-﫿]'")
	}
	var rows []model.MediaView
	err := q.Select(`m.*,
CASE WHEN ep.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-episode-' || ep.id END AS view_catalog_item_id,
CASE WHEN ep.id IS NULL THEN '' WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END AS view_series_id,
CASE WHEN ep.id IS NULL THEN '' ELSE COALESCE(g.title,w.title) END AS view_series_title,
CASE WHEN ep.id IS NULL THEN '' ELSE 'hg-season-' || w.id END AS view_season_id,
CASE WHEN ep.id IS NULL THEN w.title ELSE '第' || ep.number || '集' END AS view_title,
CASE WHEN ep.id IS NULL THEN w.overview ELSE '' END AS view_overview,
CASE WHEN ep.id IS NULL THEN w.rating ELSE 0 END AS view_rating,
CASE WHEN ep.id IS NULL THEN 0 ELSE COALESCE(gm.season_number,1) END AS view_season_num,
COALESCE(ep.number,0) AS view_episode_num,
CASE WHEN ep.id IS NULL THEN 'movie' ELSE 'episode' END AS view_metadata_kind,
'hongguo' AS view_metadata_source,
CASE WHEN ep.id IS NULL THEN COALESCE(a.id,'') ELSE '' END AS view_poster_asset_id,
COALESCE(pm.duration_ms,0) AS view_probe_duration_ms,
COALESCE(pm.size_bytes,0) AS view_probe_size_bytes,
COALESCE(pm.container,'') AS view_probe_container,
COALESCE(pm.width,0) AS view_probe_width,
COALESCE(pm.height,0) AS view_probe_height,
COALESCE(pm.video_codec,'') AS view_probe_video_codec,
COALESCE(pm.audio_codec,'') AS view_probe_audio_codec`).Scan(&rows).Error
	for i := range rows {
		rows[i].Normalize()
		if rows[i].PosterAssetID != "" {
			rows[i].PosterURL = "/api/catalogs/hongguo/artwork/" + rows[i].PosterAssetID
		}
	}
	return rows, err
}
