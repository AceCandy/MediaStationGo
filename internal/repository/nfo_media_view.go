package repository

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// nfoViewQuery 只通过独立绑定读取本地资料，并在分页前应用文件权限。
func (r *MediaViewRepository) nfoViewQuery(ctx context.Context, filter MediaQueryFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("media AS m").
		Joins("JOIN nfo_media_bindings AS b ON b.media_id = m.id").
		Joins("JOIN nfo_items AS ni ON ni.id = b.item_id").
		Joins("LEFT JOIN nfo_items AS ns ON ns.id = ni.parent_id AND ni.kind = 'episode'").
		Joins("LEFT JOIN nfo_items AS nw ON nw.id = ns.parent_id").
		Joins("LEFT JOIN media_probe_metadata AS pm ON pm.media_id = m.id").
		Where("m.catalog_source = ?", model.CatalogSourceNFO)
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if filter.MissingPoster {
		q = q.Where("COALESCE(NULLIF(b.poster_asset_id,''),nw.poster_asset_id,'') = ''")
	}
	if filter.MissingChineseTitle {
		q = q.Where("COALESCE(nw.title,b.title) !~ '[㐀-䶿一-鿿豈-﫿]'")
	}
	return q
}

const nfoViewSelect = `m.*,
ni.latest_media_added_at AS latest_media_added_at,
ni.created_at AS view_catalog_created_at,
'nfo-' || ni.id AS view_catalog_item_id,
COALESCE('nfo-' || nw.id,'') AS view_series_id,
COALESCE(nw.title,'') AS view_series_title,
COALESCE('nfo-' || ns.id,'') AS view_season_id,
b.title AS view_title,b.original_name AS view_original_name,b.overview AS view_overview,
b.rating AS view_rating,b.year AS view_year,b.release_date AS view_release_date,
COALESCE(ns.season_num,0) AS view_season_num,ni.episode_num AS view_episode_num,
b.languages AS view_languages,b.countries AS view_countries,b.genres AS view_genres,
ni.kind AS view_metadata_kind,'nfo' AS view_metadata_source,
COALESCE(NULLIF(b.poster_asset_id,''),nw.poster_asset_id,'') AS view_poster_asset_id,
COALESCE(NULLIF(b.backdrop_asset_id,''),nw.backdrop_asset_id,'') AS view_backdrop_asset_id,
COALESCE(pm.duration_ms,0) AS view_probe_duration_ms,COALESCE(pm.size_bytes,0) AS view_probe_size_bytes,
COALESCE(pm.container,'') AS view_probe_container,COALESCE(pm.width,0) AS view_probe_width,
COALESCE(pm.height,0) AS view_probe_height,COALESCE(pm.video_codec,'') AS view_probe_video_codec,
COALESCE(pm.audio_codec,'') AS view_probe_audio_codec`

func scanNFOViews(q *gorm.DB) ([]model.MediaView, error) {
	var rows []model.MediaView
	err := q.Select(nfoViewSelect).Scan(&rows).Error
	for i := range rows {
		rows[i].Normalize()
		rows[i].LookupCatalogID = strings.TrimPrefix(rows[i].CatalogItemID, "nfo-")
	}
	return rows, err
}

func (r *MediaViewRepository) nfoViewsByIDs(ctx context.Context, ids []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var sourceIDs []string
	if err := r.db.WithContext(ctx).Model(&model.Media{}).Where("id = ANY(?) AND catalog_source = ?", &ids, model.CatalogSourceNFO).Pluck("id", &sourceIDs).Error; err != nil {
		return nil, err
	}
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	return scanNFOViews(r.nfoViewQuery(ctx, filter).Where("m.id = ANY(?)", &sourceIDs))
}

func (r *MediaViewRepository) NFOItemViews(ctx context.Context, id string, filter MediaQueryFilter) ([]model.MediaView, error) {
	id = strings.TrimPrefix(id, "nfo-")
	return scanNFOViews(r.nfoItemViewQuery(ctx, []string{id}, filter).Order("ns.season_num, ni.episode_num, m.path"))
}

// nfoItemViewQuery 先一次性解析目标条目，再按绑定索引读取文件，避免层级连接重排成全表扫描。
func (r *MediaViewRepository) nfoItemViewQuery(ctx context.Context, ids []string, filter MediaQueryFilter) *gorm.DB {
	return r.nfoViewQuery(ctx, filter).Where("b.item_id = ANY(ARRAY(?))", r.nfoWorkFileItems(ctx, ids, filter))
}

// NFONodes 将可见文件展开为本地层级节点，供 Emby 在混合来源分页前查询。
func (r *MediaViewRepository) NFONodes(ctx context.Context, userID, libraryID string, filter MediaQueryFilter) *gorm.DB {
	q := r.nfoViewQuery(ctx, filter)
	return r.nfoNodesFromFiles(ctx, userID, libraryID, filter, q, false)
}

// NFOWorkNodes 仅展开当前页作品的文件，避免详情读取重新遍历全库绑定。
func (r *MediaViewRepository) NFOWorkNodes(ctx context.Context, userID, libraryID string, filter MediaQueryFilter, ids []string, containersOnly bool) *gorm.DB {
	q := r.nfoItemViewQuery(ctx, ids, filter)
	return r.nfoNodesFromFiles(ctx, userID, libraryID, filter, q, containersOnly)
}

// nfoWorkFileItems 定位条目自身、季下分集与剧下分集；混合父子输入必须去重。
func (r *MediaViewRepository) nfoWorkFileItems(ctx context.Context, ids []string, filter MediaQueryFilter) *gorm.DB {
	db := r.db.WithContext(ctx)
	items := db.Table("nfo_items").Select("id")
	if len(filter.AllowedLibraryIDs) > 0 {
		items = items.Where("library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		items = items.Where("library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	direct := items.Session(&gorm.Session{}).Where("id = ANY(?)", &ids)
	children := items.Session(&gorm.Session{}).Where("parent_id = ANY(?) AND kind='episode'", &ids)
	// 归属条件跟随每个索引定位分支；外层再 JOIN 全部条目可能重排成全目录扫描。
	episodes := items.Session(&gorm.Session{}).Where("parent_id=season.id AND kind='episode'")
	descendants := db.Table("nfo_items season").Select("ep.id").
		Joins("JOIN LATERAL (? OFFSET 0) ep ON TRUE", episodes).Where("season.parent_id = ANY(?)", &ids)
	return db.Raw("? UNION ? UNION ?", direct, children, descendants)
}

func (r *MediaViewRepository) nfoNodesFromFiles(ctx context.Context, userID, libraryID string, filter MediaQueryFilter, q *gorm.DB, containersOnly bool) *gorm.DB {
	if libraryID != "" {
		q = q.Where("m.library_id = ?", libraryID)
	}
	states := PlaybackStates(ctx, r.db, "nfo", userID, filter)
	stateJoin := "LEFT JOIN (?) st ON st.item_id = ni.id"
	completed := "COALESCE(st.completed,FALSE)"
	playbackFields := `(ARRAY_AGG(m.id ORDER BY CASE WHEN m.id = st.media_id THEN 0 ELSE 1 END, st.watched_at DESC NULLS LAST, m.id))[1] AS media_id,
MAX(st.watched_at) AS played_at, MAX(COALESCE(st.position_ms,0)) AS position_ms, MAX(m.created_at) AS file_latest_at,`
	if containersOnly {
		states = CompletedPlaybackStates(ctx, r.db, "nfo", userID, filter).Where("item_id = ni.id")
		// 保留页内逐身份索引探测，不将 UNION 放大成全用户状态扫描。
		stateJoin = "LEFT JOIN LATERAL (? OFFSET 0) st ON TRUE"
		completed, playbackFields = "st.item_id IS NOT NULL", ""
		q = q.Where("item.kind IN ('series','season')")
	}
	q = q.Joins("JOIN nfo_items item ON item.id IN (ni.id,ns.id,nw.id)").
		Joins(stateJoin, states).
		Joins("LEFT JOIN nfo_user_states fav ON fav.item_id = item.id AND fav.user_id = ?", userID).
		Select(`'nfo-' || item.id AS id, 'nfo-' || COALESCE(nw.id,ni.id) AS resume_key,
INITCAP(item.kind) AS kind, item.title, COALESCE('nfo-' || item.parent_id,'') AS parent_id,
item.season_num AS season_number, item.episode_num AS episode_number, ` + playbackFields + `
item.created_at, item.latest_media_added_at AS latest_at, BOOL_AND(` + completed + `) AS played,
BOOL_OR(COALESCE(fav.favorite,FALSE)) AS favorite, MAX(fav.favorite_added_at) AS favorite_at,
item.poster_asset_id AS artwork_id, item.overview, item.rating, item.release_date, item.year,
COUNT(DISTINCT ni.id) AS episode_count,
COUNT(DISTINCT ni.id) FILTER (WHERE ni.kind = 'episode' AND NOT (` + completed + `)) AS unplayed_item_count`).Group("item.id,COALESCE(nw.id,ni.id)")
	return r.db.WithContext(ctx).Table("(?) AS nodes", q)
}

func (r *MediaViewRepository) nfoLibraryPage(ctx context.Context, libraryID, kind, itemID string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, []LibraryMetadataSummary, int64, error) {
	q := r.nfoViewQuery(ctx, filter).Where("m.library_id = ?", libraryID)
	work := "ni.id"
	if kind == model.MetadataKindSeries {
		work = "nw.id"
		q = q.Where("ni.kind = 'episode'")
	} else {
		q = q.Where("ni.kind = 'movie'")
	}
	if itemID != "" {
		q = q.Where(work+" = ?", strings.TrimPrefix(itemID, "nfo-"))
	}
	baseFilter := filter
	baseFilter.MissingPoster, baseFilter.MissingChineseTitle = false, false
	groups := r.db.WithContext(ctx).Table("(?) recent", r.NFOWorkCandidates(ctx, "", libraryID, baseFilter, false)).
		Select("recent.id, recent.latest_at AS latest").Where("recent.kind=?", kind)
	if itemID != "" {
		groups = groups.Where("recent.id=?", strings.TrimPrefix(itemID, "nfo-"))
	}
	page := r.db.Table("works").Order("latest DESC NULLS LAST, id").Offset(offset).Limit(limit)
	var rows []struct {
		ID    string
		Total int64
	}
	var total int64
	var workIDs []string
	var err error
	if filter.MissingPoster || filter.MissingChineseTitle {
		fileKind := model.MetadataKindMovie
		if kind == model.MetadataKindSeries {
			fileKind = model.MetadataKindEpisode
		}
		candidates := r.db.Table("(?) candidates", groups).
			Select("*, ROW_NUMBER() OVER (ORDER BY latest DESC NULLS LAST,id) AS ordinal").Order("latest DESC NULLS LAST,id")
		eligible := r.db.Table("work_batch recent").Select("recent.ordinal").
			Where("EXISTS (? OFFSET 0)", r.NFOCandidateFiles(ctx, filter, "recent.id").Select("1").
				Where("m.library_id=? AND ni.kind=?", libraryID, fileKind).
				Where(work+"=recent.id"))
		workIDs, total, err = r.WorkBatchPage(ctx, candidates, eligible, offset, limit, true)
	} else {
		err = r.db.WithContext(ctx).Raw(`WITH works AS MATERIALIZED (?), page AS (?)
SELECT COALESCE(page.id,'') AS id, totals.total FROM (SELECT COUNT(*) AS total FROM works) totals
LEFT JOIN page ON TRUE ORDER BY page.latest DESC NULLS LAST, page.id`, groups, page).Scan(&rows).Error
		for _, row := range rows {
			total = row.Total
			if row.ID != "" {
				workIDs = append(workIDs, row.ID)
			}
		}
	}
	if err != nil {
		return nil, nil, 0, err
	}
	if len(workIDs) == 0 {
		return []model.MediaView{}, []LibraryMetadataSummary{}, total, nil
	}
	var summaries []LibraryMetadataSummary
	err = q.Joins("JOIN (?) page_leaf ON page_leaf.id = b.item_id", r.nfoWorkFileItems(ctx, workIDs, filter)).
		Select("'nfo-' || " + work + " AS metadata_id, MIN(m.id) AS media_id, COUNT(DISTINCT ni.id) AS count, COUNT(*) AS version_count").
		Group(work).Order("MAX(COALESCE(nw.latest_media_added_at,ni.latest_media_added_at)) DESC NULLS LAST, " + work).Scan(&summaries).Error
	if err != nil {
		return nil, nil, 0, err
	}
	ids := make([]string, 0, len(summaries))
	for _, row := range summaries {
		ids = append(ids, row.MediaID)
	}
	filter.MissingPoster, filter.MissingChineseTitle = false, false
	views, err := r.nfoViewsByIDs(ctx, ids, filter)
	return views, summaries, total, err
}

// NFOPresentation 读取已由调用方按文件可见性确认的本地层级资料。
func (r *MediaViewRepository) NFOPresentation(ctx context.Context, id string) (*model.MediaView, error) {
	var item model.NFOItem
	q := r.db.WithContext(ctx).Where("id = ?", strings.TrimPrefix(id, "nfo-"))
	result := q.Limit(1).Find(&item)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return nfoPresentation(item), nil
}

func nfoPresentation(item model.NFOItem) *model.MediaView {
	v := &model.MediaView{Media: model.Media{PermanentBase: item.PermanentBase, LibraryID: item.LibraryID, CatalogSource: model.CatalogSourceNFO}, CatalogItemID: "nfo-" + item.ID, CatalogCreatedAt: item.CreatedAt,
		Title: item.Title, OriginalName: item.OriginalName, Overview: item.Overview, Year: item.Year, Rating: item.Rating, ReleaseDate: item.ReleaseDate,
		Genres: item.Genres, Countries: item.Countries, Languages: item.Languages, MetadataKind: item.Kind, MetadataSource: model.CatalogSourceNFO,
		SeasonNum: item.SeasonNum, EpisodeNum: item.EpisodeNum, PosterAssetID: item.PosterAssetID, BackdropAssetID: item.BackdropAssetID}
	if item.Kind == "series" {
		v.SeriesID, v.SeriesTitle = v.CatalogItemID, item.Title
	}
	v.Normalize()
	return v
}

// NFOCandidateFiles 从指定节点展开绑定；itemID 只接受调用方固定的 SQL 别名表达式。
func (r *MediaViewRepository) NFOCandidateFiles(ctx context.Context, filter MediaQueryFilter, itemID string) *gorm.DB {
	return r.nfoViewQuery(ctx, filter).Where(`b.item_id IN (
SELECT ` + itemID + `
UNION ALL SELECT leaf.id FROM nfo_items leaf WHERE leaf.parent_id = ` + itemID + ` AND leaf.kind = 'episode'
UNION ALL SELECT leaf.id FROM nfo_items season JOIN LATERAL (
SELECT id FROM nfo_items WHERE parent_id=season.id AND kind='episode' OFFSET 0
) leaf ON TRUE WHERE season.parent_id = ` + itemID + `
)`)
}

func (r *MediaViewRepository) nfoSearchRepresentatives(ctx context.Context, ids []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	itemIDs := make([]string, len(ids))
	for i, id := range ids {
		itemIDs[i] = strings.TrimPrefix(id, "nfo-")
	}
	files := r.NFOCandidateFiles(ctx, filter, "requested.id").
		Where("ni.id = requested.id OR ns.id = requested.id OR nw.id = requested.id").
		Select("m.id AS representative_id").Order("ns.season_num, ni.episode_num, m.path").Limit(1)
	var rows []struct {
		model.NFOItem
		RepresentativeID string
	}
	q := r.db.WithContext(ctx).Table("nfo_items AS requested").
		Select("requested.*, representative.representative_id").
		Joins("JOIN LATERAL (?) AS representative ON TRUE", files).Where("requested.id = ANY(?)", &itemIDs)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]model.MediaView, 0, len(rows))
	for _, row := range rows {
		view := nfoPresentation(row.NFOItem)
		view.ID = row.RepresentativeID
		view.LookupCatalogID = row.NFOItem.ID
		views = append(views, *view)
	}
	return views, nil
}

func (r *MediaViewRepository) nfoSearchQuery(ctx context.Context, filter MetadataSearchFilter, groups []metadataSearchTermGroup) *gorm.DB {
	fileFilter := filter.MediaQueryFilter
	if filter.LibraryRestricted {
		fileFilter.AllowedLibraryIDs = filter.VisibleLibraryIDs
	}
	files := r.NFOCandidateFiles(ctx, fileFilter, "search_metadata.id").
		Where("COALESCE(nw.id,ni.id) = search_metadata.id").Select("1").Limit(1)
	q := r.db.WithContext(ctx).Table("nfo_items AS search_metadata")
	if len(groups) > 0 {
		matches := applyMetadataSearchLIKEFilter(r.db.WithContext(ctx).Table("nfo_items AS search_metadata"), groups, filter.Fields).
			Where("search_metadata.kind IN ?", filter.Kinds).Select("search_metadata.*")
		// 先匹配作品再检查文件权限，避免全库关联和逐行子计划的过高成本估算。
		q = r.db.WithContext(ctx).Table("(WITH matching AS MATERIALIZED (?) SELECT * FROM matching) AS search_metadata", matches)
	}
	q = q.Where("search_metadata.kind IN ?", filter.Kinds)
	if len(fileFilter.AllowedLibraryIDs) > 0 {
		q = q.Where("search_metadata.library_id = ANY(?)", &fileFilter.AllowedLibraryIDs)
	}
	if len(fileFilter.HiddenLibraryIDs) > 0 {
		q = q.Where("search_metadata.library_id <> ALL(?)", &fileFilter.HiddenLibraryIDs)
	}
	if fileFilter.MissingPoster || fileFilter.MissingChineseTitle {
		q = q.Where("EXISTS (? OFFSET 0)", files)
	} else {
		// 正常入库事务保证条目和绑定同库；未初始化的汇总才回查文件。
		q = q.Where("search_metadata.latest_media_added_at IS NOT NULL OR EXISTS (? OFFSET 0)", files)
	}
	if len(filter.PersonIDs) > 0 {
		q = q.Where("FALSE")
	}
	if filter.FavoriteUserID != "" {
		q = q.Where("EXISTS (SELECT 1 FROM nfo_user_states s WHERE s.item_id = search_metadata.id AND s.user_id = ? AND s.favorite)", filter.FavoriteUserID)
	}
	if filter.ResumableUserID != "" {
		q = q.Where(`EXISTS (SELECT 1 FROM (?) s JOIN nfo_items leaf ON leaf.id = s.item_id
LEFT JOIN nfo_items season ON season.id = leaf.parent_id
WHERE s.position_ms > 0 AND (leaf.id = search_metadata.id OR season.parent_id = search_metadata.id))`, PlaybackStates(ctx, r.db, "nfo", filter.ResumableUserID, fileFilter))
	}
	return q
}
