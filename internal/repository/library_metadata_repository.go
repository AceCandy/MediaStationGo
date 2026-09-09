package repository

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// LibraryMetadataSummary 保存作品分页和卡片所需统计，不携带全部版本或分集。
type LibraryMetadataSummary struct {
	MetadataID   string
	MediaID      string
	Count        int
	VersionCount int
}

// libraryMetadataScope 通过真实文件关联限定作品归属，支持整剧、季和分集直接关联。
func (r *MediaViewRepository) libraryMetadataScope(ctx context.Context, libraryID, kind, metadataID string, filter MediaQueryFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("media AS m")
	filtered := filter.MissingPoster || filter.MissingChineseTitle
	if filtered && kind == model.MetadataKindSeries {
		q = r.libraryFilteredSeriesScope(ctx, libraryID, metadataID, filter)
	} else {
		q = q.Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id")
		if filtered && kind == model.MetadataKindMovie {
			q = q.Joins("JOIN metadata_items AS work ON work.id = mi.id")
		} else {
			q = q.Joins("LEFT JOIN metadata_items AS season ON season.id = mi.parent_id AND mi.kind = 'episode' AND season.kind = 'season'").
				Joins("JOIN metadata_items AS work ON work.id = CASE WHEN mi.kind = 'episode' THEN season.parent_id WHEN mi.kind = 'season' THEN mi.parent_id ELSE mi.id END")
		}
	}
	q = q.Where("m.library_id = ? AND work.kind = ?", libraryID, kind)
	if metadataID != "" {
		q = q.Where("work.id = ?", metadataID)
	}
	if !filter.IncludeNSFW {
		q = q.Where("COALESCE(mi.nsfw, FALSE) = FALSE AND COALESCE(work.nsfw, FALSE) = FALSE")
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if filtered && kind == model.MetadataKindSeries {
		return q
	}
	return applyLibraryMetadataFilters(q, filter)
}

func applyLibraryMetadataFilters(q *gorm.DB, filter MediaQueryFilter) *gorm.DB {
	if filter.MissingPoster {
		q = q.Where(`NOT EXISTS (SELECT 1 FROM metadata_artworks AS art JOIN artwork_assets AS asset ON asset.id = art.asset_id WHERE art.metadata_id = work.id AND art.artwork_type = 'poster')`)
	}
	if filter.MissingChineseTitle {
		q = q.Where(`COALESCE(NULLIF(work.title, ''), work.original_name, '') !~ '[㐀-䶿一-鿿豈-﫿]'`)
	}
	return q
}

// libraryFilteredSeriesScope 先筛整剧，再沿父子索引展开关联文件，避免逐文件检查整剧海报。
func (r *MediaViewRepository) libraryFilteredSeriesScope(ctx context.Context, libraryID, metadataID string, filter MediaQueryFilter) *gorm.DB {
	candidates := r.db.WithContext(ctx).Table("metadata_items AS work").Where("work.kind = 'series'")
	if metadataID != "" {
		candidates = candidates.Where("work.id = ?", metadataID)
	}
	candidates = applyLibraryMetadataFilters(candidates, filter)
	episodes := `SELECT episode.* FROM metadata_items AS parent
JOIN metadata_items AS episode ON episode.parent_id = parent.id AND episode.kind = 'episode'
WHERE parent.parent_id = work.id AND parent.kind = 'season'`
	var args []any
	if filter.MissingChineseTitle && !filter.MissingPoster {
		// IS TRUE 保留一次构建的库内文件哈希集合，避免每部剧重复聚合或逐集探测文件索引。
		episodes += ` AND (episode.id IN (SELECT metadata_id FROM media WHERE library_id = ?)) IS TRUE`
		args = append(args, libraryID)
	}
	return r.db.WithContext(ctx).Table("(WITH candidates AS MATERIALIZED (?) SELECT * FROM candidates) AS work", candidates).
		Joins(`CROSS JOIN LATERAL (SELECT work.* UNION ALL
SELECT child.* FROM metadata_items AS child WHERE child.parent_id = work.id AND child.kind = 'season'
UNION ALL `+episodes+`) AS mi`, args...).
		Joins("JOIN media AS m ON m.metadata_id = mi.id").
		Joins("LEFT JOIN metadata_items AS season ON season.id = mi.parent_id AND mi.kind = 'episode' AND season.kind = 'season'")
}

// ListLibraryMetadataPage 在数据库按作品统计、排序和分页，只读取当前页的一个操作文件。
func (r *MediaViewRepository) ListLibraryMetadataPage(ctx context.Context, libraryID, kind, metadataID string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, []LibraryMetadataSummary, int64, error) {
	query := func() *gorm.DB { return r.libraryMetadataScope(ctx, libraryID, kind, metadataID, filter) }
	var total int64
	var summaries []LibraryMetadataSummary
	var err error
	if kind == model.MetadataKindSeries {
		summaries, total, err = r.librarySeriesPage(ctx, query(), libraryID, metadataID, offset, limit, filter.MissingPoster || filter.MissingChineseTitle)
	} else {
		if err := r.db.WithContext(ctx).Table("(?) AS works", query().Select("work.id").Group("work.id")).Count(&total).Error; err != nil {
			return nil, nil, 0, err
		}
		if total == 0 {
			return nil, nil, 0, nil
		}
		// 分段先选择最低 PartIndex，电影版本再沿用本地、清晰度、大小的优先顺序。
		q := query()
		priority := "CASE WHEN COALESCE(m.strm_url, '') ~* '^https?://' THEN 1 ELSE 0 END, COALESCE(probe.width, 0)::bigint * COALESCE(probe.height, 0) DESC, COALESCE(probe.size_bytes, 0) DESC, m.created_at DESC, m.id DESC"
		order := "(ARRAY_AGG(m.created_at ORDER BY " + priority + "))[1] DESC, work.id DESC"
		q = q.Joins("LEFT JOIN media_probe_metadata AS probe ON probe.media_id = m.id")
		partScope := query().Select("MIN(m.part_index)").Where("m.part_group_key = outer_media.part_group_key AND m.part_index > 0")
		// 相关子查询需要独立的外层别名，避免子查询条件引用自身。
		q = q.Joins("JOIN media AS outer_media ON outer_media.id = m.id").Where("COALESCE(m.part_group_key, '') = '' OR m.part_index <= 0 OR m.part_index = (?)", partScope)
		err = q.Select("work.id AS metadata_id, (ARRAY_AGG(m.id ORDER BY " + priority + "))[1] AS media_id, COUNT(DISTINCT mi.id) AS count, COUNT(*) AS version_count").
			Group("work.id").Order(order).Offset(offset).Limit(limit).Scan(&summaries).Error
	}
	if err != nil {
		return nil, nil, 0, err
	}
	ids := make([]string, 0, len(summaries))
	for _, row := range summaries {
		ids = append(ids, row.MediaID)
	}
	// 卡片筛选针对作品而非代表分集，不可再次用分集海报/标题筛选。
	filter.MissingPoster, filter.MissingChineseTitle = false, false
	views, err := r.FindByIDs(ctx, ids, filter)
	return views, summaries, total, err
}

// librarySeriesPage 共用一次文件范围计算总数与分页，只为选中作品排序代表文件。
func (r *MediaViewRepository) librarySeriesPage(ctx context.Context, q *gorm.DB, libraryID, metadataID string, offset, limit int, filtered bool) ([]LibraryMetadataSummary, int64, error) {
	// 普通浏览保留文件边界；缺失信息筛选已从候选整剧出发，不可覆盖其输入表。
	if metadataID == "" && !filtered {
		q = q.Table("(SELECT * FROM media WHERE library_id = ? OFFSET 0) AS m", libraryID)
	}
	q = q.Select(`work.id AS metadata_id, m.id AS media_id, mi.id AS item_id,
COALESCE(season.season_num, mi.season_num, 0) AS season_num, COALESCE(mi.episode_num, 0) AS episode_num, m.created_at,
CASE WHEN COALESCE(mi.release_date, '') <> '' THEN mi.release_date
WHEN COALESCE(mi.year, 0) > 0 THEN LPAD(mi.year::text, 4, '0') || '-12-31'
ELSE to_char(GREATEST(m.updated_at, m.created_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US') END AS sort_date`)
	page := r.db.Table("works").Order("sort_date DESC, metadata_id DESC").Offset(offset).Limit(limit)
	var rows []struct {
		LibraryMetadataSummary
		Total int64
	}
	err := r.db.WithContext(ctx).Raw(`WITH scoped AS MATERIALIZED (?), works AS MATERIALIZED (
SELECT metadata_id, MAX(sort_date) AS sort_date FROM scoped GROUP BY metadata_id
), page AS MATERIALIZED (?), cards AS (
SELECT page.metadata_id, page.sort_date,
(ARRAY_AGG(scoped.media_id ORDER BY scoped.season_num, scoped.episode_num, scoped.created_at, scoped.media_id))[1] AS media_id,
COUNT(DISTINCT scoped.item_id) AS count, COUNT(*) AS version_count
FROM page JOIN scoped USING (metadata_id) GROUP BY page.metadata_id, page.sort_date
)
SELECT cards.*, totals.total FROM (SELECT COUNT(*) AS total FROM works) totals
LEFT JOIN cards ON TRUE ORDER BY cards.sort_date DESC, cards.metadata_id DESC`, q, page).Scan(&rows).Error
	var total int64
	summaries := make([]LibraryMetadataSummary, 0, len(rows))
	for _, row := range rows {
		total = row.Total
		// 空页仍由 LEFT JOIN 返回总数，但不能产生空卡片。
		if row.MetadataID != "" {
			summaries = append(summaries, row.LibraryMetadataSummary)
		}
	}
	return summaries, total, err
}

// LibrarySeriesMetadataIDs 只用于解析旧哈希深链，不读取全库文件展示数据。
func (r *MediaViewRepository) LibrarySeriesMetadataIDs(ctx context.Context, libraryID string, filter MediaQueryFilter) ([]string, error) {
	var ids []string
	err := r.libraryMetadataScope(ctx, libraryID, model.MetadataKindSeries, "", filter).Distinct("work.id").Pluck("work.id", &ids).Error
	return ids, err
}

// ListLibrarySeriesViews 仅加载指定作品在当前库内的可见关联文件。
func (r *MediaViewRepository) ListLibrarySeriesViews(ctx context.Context, libraryID, metadataID string, filter MediaQueryFilter) ([]model.MediaView, error) {
	ids := r.libraryMetadataScope(ctx, libraryID, model.MetadataKindSeries, metadataID, filter).Select("m.id")
	var rows []model.MediaView
	// 先限定文件输入，再关联展示数据；OFFSET 0 阻止规划器将剧集过滤推迟到全库投影之后。
	err := scanMediaViews(r.query(ctx).Table("(SELECT * FROM media WHERE id IN (?) OFFSET 0) AS m", ids).
		Order("view_season_num, view_episode_num, m.created_at, m.id"), &rows)
	return rows, err
}
