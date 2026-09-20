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
	if !filter.IncludeNSFW {
		q = q.Where("NOT COALESCE(b.nsfw,FALSE) AND NOT COALESCE(ni.nsfw,FALSE) AND NOT COALESCE(ns.nsfw,FALSE) AND NOT COALESCE(nw.nsfw,FALSE)")
	}
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
'nfo-' || ni.id AS view_catalog_item_id,
COALESCE('nfo-' || nw.id,'') AS view_series_id,
COALESCE(nw.title,'') AS view_series_title,
COALESCE('nfo-' || ns.id,'') AS view_season_id,
b.title AS view_title,b.original_name AS view_original_name,b.overview AS view_overview,
b.rating AS view_rating,b.year AS view_year,b.release_date AS view_release_date,
COALESCE(ns.season_num,0) AS view_season_num,ni.episode_num AS view_episode_num,
b.languages AS view_languages,b.countries AS view_countries,b.genres AS view_genres,
(COALESCE(b.nsfw,FALSE) OR COALESCE(ni.nsfw,FALSE) OR COALESCE(ns.nsfw,FALSE) OR COALESCE(nw.nsfw,FALSE)) AS view_nsfw,ni.kind AS view_metadata_kind,'nfo' AS view_metadata_source,
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
	return scanNFOViews(r.nfoViewQuery(ctx, filter).Where("ni.id = ? OR ns.id = ? OR nw.id = ?", id, id, id).Order("ns.season_num, ni.episode_num, m.path"))
}

// NFONodes 将可见文件展开为本地层级节点，供 Emby 在混合来源分页前查询。
func (r *MediaViewRepository) NFONodes(ctx context.Context, userID, libraryID string, filter MediaQueryFilter) *gorm.DB {
	q := r.nfoViewQuery(ctx, filter)
	if libraryID != "" {
		q = q.Where("m.library_id = ?", libraryID)
	}
	q = q.Joins("JOIN nfo_items item ON item.id IN (ni.id,ns.id,nw.id)").
		Joins("LEFT JOIN nfo_user_states st ON st.item_id = ni.id AND st.user_id = ?", userID).
		Joins("LEFT JOIN nfo_user_states fav ON fav.item_id = item.id AND fav.user_id = ?", userID).
		Select(`'nfo-' || item.id AS id, 'nfo-' || COALESCE(nw.id,ni.id) AS resume_key,
INITCAP(item.kind) AS kind, item.title, COALESCE('nfo-' || item.parent_id,'') AS parent_id,
item.season_num AS season_number, item.episode_num AS episode_number,
(ARRAY_AGG(m.id ORDER BY CASE WHEN m.id = st.media_id THEN 0 ELSE 1 END, st.watched_at DESC NULLS LAST, m.id))[1] AS media_id, MIN(m.created_at) AS created_at, MAX(m.created_at) AS latest_at,
MAX(st.watched_at) AS played_at, BOOL_AND(COALESCE(st.completed,FALSE)) AS played,
BOOL_OR(COALESCE(fav.favorite,FALSE)) AS favorite, MAX(COALESCE(st.position_ms,0)) AS position_ms,
item.poster_asset_id AS artwork_id, item.overview, item.rating, item.release_date, item.year,
COUNT(DISTINCT ni.id) AS episode_count`).Group("item.id,COALESCE(nw.id,ni.id)")
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
	groups := q.Select(work + " AS id, MIN(m.id) AS media_id, COUNT(DISTINCT ni.id) AS count, COUNT(*) AS version_count, MAX(m.created_at) AS latest").Group(work)
	var total int64
	if err := r.db.WithContext(ctx).Table("(?) AS grouped", groups).Count(&total).Error; err != nil {
		return nil, nil, 0, err
	}
	var summaries []LibraryMetadataSummary
	err := r.db.WithContext(ctx).Table("(?) AS grouped", groups).Select("'nfo-' || id AS metadata_id, media_id, count, version_count").Order("latest DESC, id").Offset(offset).Limit(limit).Scan(&summaries).Error
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
func (r *MediaViewRepository) NFOPresentation(ctx context.Context, id string, includeNSFW bool) (*model.MediaView, error) {
	var item model.NFOItem
	q := r.db.WithContext(ctx).Where("id = ?", strings.TrimPrefix(id, "nfo-"))
	if !includeNSFW {
		q = q.Where("NOT nsfw")
	}
	result := q.Limit(1).Find(&item)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	v := &model.MediaView{Media: model.Media{PermanentBase: item.PermanentBase, LibraryID: item.LibraryID, CatalogSource: model.CatalogSourceNFO}, CatalogItemID: "nfo-" + item.ID,
		Title: item.Title, OriginalName: item.OriginalName, Overview: item.Overview, Year: item.Year, Rating: item.Rating, ReleaseDate: item.ReleaseDate,
		Genres: item.Genres, Countries: item.Countries, Languages: item.Languages, NSFW: item.NSFW, MetadataKind: item.Kind, MetadataSource: model.CatalogSourceNFO,
		SeasonNum: item.SeasonNum, EpisodeNum: item.EpisodeNum, PosterAssetID: item.PosterAssetID, BackdropAssetID: item.BackdropAssetID}
	if item.Kind == "series" {
		v.SeriesID, v.SeriesTitle = v.CatalogItemID, item.Title
	}
	v.Normalize()
	return v, nil
}

func (r *MediaViewRepository) nfoSearchQuery(ctx context.Context, filter MetadataSearchFilter) *gorm.DB {
	fileFilter := filter.MediaQueryFilter
	if filter.LibraryRestricted {
		fileFilter.AllowedLibraryIDs = filter.VisibleLibraryIDs
	}
	files := r.nfoViewQuery(ctx, fileFilter).Select("COALESCE(nw.id,ni.id)")
	q := r.db.WithContext(ctx).Table("nfo_items AS search_metadata").Where("search_metadata.id IN (?)", files).
		Where("search_metadata.kind IN ?", filter.Kinds)
	if len(filter.PersonIDs) > 0 {
		q = q.Where("FALSE")
	}
	if filter.FavoriteUserID != "" {
		q = q.Where("EXISTS (SELECT 1 FROM nfo_user_states s WHERE s.item_id = search_metadata.id AND s.user_id = ? AND s.favorite)", filter.FavoriteUserID)
	}
	if filter.ResumableUserID != "" {
		q = q.Where(`EXISTS (SELECT 1 FROM nfo_user_states s JOIN nfo_items leaf ON leaf.id = s.item_id
LEFT JOIN nfo_items season ON season.id = leaf.parent_id
WHERE s.user_id = ? AND NOT s.completed AND s.position_ms > 0 AND (leaf.id = search_metadata.id OR season.parent_id = search_metadata.id))`, filter.ResumableUserID)
	}
	return q
}
