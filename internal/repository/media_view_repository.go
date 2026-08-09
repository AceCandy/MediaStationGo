package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const mediaViewSelect = `
m.*,
	COALESCE(CASE WHEN mi.kind IN ('episode', 'season') THEN series_metadata.id WHEN mi.kind = 'series' THEN mi.id ELSE NULL END, '') AS view_series_id,
	COALESCE(CASE WHEN mi.kind = 'episode' THEN season_metadata.id WHEN mi.kind = 'season' THEN mi.id ELSE NULL END, '') AS view_season_id,
	COALESCE(NULLIF(mi.title, ''), m.scan_title) AS view_title,
	COALESCE(mi.original_name, '') AS view_original_name,
	COALESCE(mi.episode_title, '') AS view_episode_title,
	COALESCE(mi.overview, '') AS view_overview,
	COALESCE(mi.rating, 0) AS view_rating,
	COALESCE(mi.year, m.scan_year, 0) AS view_year,
	COALESCE(mi.release_date, '') AS view_release_date,
	COALESCE(season_metadata.season_num, m.season_num, 0) AS view_season_num,
	COALESCE(NULLIF(mi.episode_num, 0), m.episode_num, 0) AS view_episode_num,
	COALESCE(identifiers.tmdb_external_id, '') AS view_tmdb_external_id,
	COALESCE(identifiers.bangumi_external_id, '') AS view_bangumi_external_id,
	COALESCE(identifiers.douban_external_id, '') AS view_douban_id,
	COALESCE(identifiers.thetvdb_external_id, '') AS view_thetvdb_id,
	COALESCE(mi.languages, '') AS view_languages,
	COALESCE(mi.countries, '') AS view_countries,
	COALESCE(mi.genres, '') AS view_genres,
	COALESCE(mi.nsfw, FALSE) AS view_nsfw,
	COALESCE(mi.kind, '') AS view_metadata_kind,
	COALESCE(mi.source, '') AS view_metadata_source,
	COALESCE(poster_asset.id, '') AS view_poster_asset_id,
	COALESCE(still_asset.id, backdrop_asset.id, '') AS view_backdrop_asset_id`

// MediaViewRepository 对共享元数据完成 JOIN 后再执行权限、排序和分页。
type MediaViewRepository struct {
	db            *gorm.DB
	searchBackend MediaSearchBackend
}

func (r *MediaViewRepository) SetSearchBackend(backend MediaSearchBackend) {
	if r != nil {
		r.searchBackend = backend
	}
}

func (r *MediaViewRepository) query(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("media AS m").
		Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id AND mi.deleted_at IS NULL").
		Joins("LEFT JOIN metadata_items AS season_metadata ON season_metadata.id = mi.parent_id AND mi.kind = 'episode' AND season_metadata.kind = 'season' AND season_metadata.deleted_at IS NULL").
		Joins("LEFT JOIN metadata_items AS series_metadata ON series_metadata.id = CASE WHEN mi.kind = 'episode' THEN season_metadata.parent_id WHEN mi.kind = 'season' THEN mi.parent_id ELSE NULL END AND series_metadata.kind = 'series' AND series_metadata.deleted_at IS NULL").
		Joins(`LEFT JOIN (
			SELECT metadata_id, entity_kind,
				MIN(CASE WHEN provider = 'tmdb' THEN external_id END) AS tmdb_external_id,
				MIN(CASE WHEN provider = 'bangumi' THEN external_id END) AS bangumi_external_id,
				MIN(CASE WHEN provider = 'douban' THEN external_id END) AS douban_external_id,
				MIN(CASE WHEN provider = 'thetvdb' THEN external_id END) AS thetvdb_external_id
			FROM metadata_identifiers
			WHERE deleted_at IS NULL
			GROUP BY metadata_id, entity_kind
		) AS identifiers ON identifiers.metadata_id = mi.id AND identifiers.entity_kind = mi.kind`).
		Joins("LEFT JOIN metadata_artworks AS poster ON poster.metadata_id = mi.id AND poster.artwork_type = 'poster' AND poster.deleted_at IS NULL").
		Joins("LEFT JOIN artwork_assets AS poster_asset ON poster_asset.id = poster.asset_id AND poster_asset.deleted_at IS NULL").
		Joins("LEFT JOIN metadata_artworks AS backdrop ON backdrop.metadata_id = mi.id AND backdrop.artwork_type = 'backdrop' AND backdrop.deleted_at IS NULL").
		Joins("LEFT JOIN artwork_assets AS backdrop_asset ON backdrop_asset.id = backdrop.asset_id AND backdrop_asset.deleted_at IS NULL").
		Joins("LEFT JOIN metadata_artworks AS still ON still.metadata_id = mi.id AND still.artwork_type = 'still' AND still.deleted_at IS NULL").
		Joins("LEFT JOIN artwork_assets AS still_asset ON still_asset.id = still.asset_id AND still_asset.deleted_at IS NULL").
		Where("m.deleted_at IS NULL")
}

func applyMediaViewFilter(q *gorm.DB, filter MediaQueryFilter) *gorm.DB {
	if !filter.IncludeNSFW {
		q = q.Where("COALESCE(mi.nsfw, FALSE) = FALSE")
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id NOT IN ?", filter.HiddenLibraryIDs)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id IN ?", filter.AllowedLibraryIDs)
	}
	return q
}

func scanMediaViews(q *gorm.DB, views *[]model.MediaView) error {
	if err := q.Select(mediaViewSelect).Scan(views).Error; err != nil {
		return err
	}
	for i := range *views {
		(*views)[i].Normalize()
	}
	return nil
}

func (r *MediaViewRepository) FindByID(ctx context.Context, id string) (*model.MediaView, error) {
	var rows []model.MediaView
	if err := scanMediaViews(r.query(ctx).Where("m.id = ?", id).Limit(1), &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *MediaViewRepository) FindByIDs(ctx context.Context, ids []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(ids) == 0 {
		return []model.MediaView{}, nil
	}
	var rows []model.MediaView
	q := applyMediaViewFilter(r.query(ctx).Where("m.id IN ?", ids), filter)
	if err := scanMediaViews(q, &rows); err != nil {
		return nil, err
	}
	byID := make(map[string]model.MediaView, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	out := make([]model.MediaView, 0, len(rows))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// FindByMetadataID 返回一个作品当前可见的全部播放版本。
func (r *MediaViewRepository) FindByMetadataID(ctx context.Context, metadataID string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if strings.TrimSpace(metadataID) == "" {
		return []model.MediaView{}, nil
	}
	var rows []model.MediaView
	q := applyMediaViewFilter(r.query(ctx).Where("m.metadata_id = ?", metadataID), filter).
		Order("m.updated_at DESC, m.created_at DESC, m.id DESC")
	if err := scanMediaViews(q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MediaViewRepository) ListByLibrariesFiltered(ctx context.Context, libraryIDs []string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, int64, error) {
	if len(libraryIDs) == 0 {
		return []model.MediaView{}, 0, nil
	}
	q := applyMediaViewFilter(r.query(ctx).Where("m.library_id IN ?", libraryIDs), filter)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.MediaView
	q = q.Order("COALESCE(mi.release_date, '') DESC, COALESCE(mi.year, m.scan_year, 0) DESC, m.updated_at DESC, m.created_at DESC, m.id DESC").
		Offset(offset).Limit(limit)
	if err := scanMediaViews(q, &rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *MediaViewRepository) SearchFilteredPage(ctx context.Context, query string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	if query != "" && r.searchBackend != nil {
		if rows, total, ok := r.searchFilteredBackend(ctx, query, offset, limit, filter); ok {
			return rows, total, nil
		}
	}
	q := applyMediaViewFilter(r.query(ctx), filter)
	q = applyMediaViewLIKEFilter(q, query)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if query != "" {
		prefix := escapeLike(query) + "%"
		q = q.Order(gorm.Expr("CASE WHEN mi.title = ? THEN 0 WHEN mi.original_name = ? THEN 1 WHEN mi.title LIKE ? ESCAPE '\\' THEN 2 WHEN mi.original_name LIKE ? ESCAPE '\\' THEN 3 ELSE 4 END, m.created_at DESC", query, query, prefix, prefix))
	} else {
		q = q.Order("m.created_at DESC")
	}
	var rows []model.MediaView
	if err := scanMediaViews(q.Offset(offset).Limit(limit), &rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *MediaViewRepository) searchFilteredBackend(ctx context.Context, query string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, int64, bool) {
	ids, total, err := r.searchBackend.SearchMediaIDs(ctx, query, offset, limit, filter)
	if err != nil {
		return nil, 0, false
	}
	rows, err := r.FindByIDs(ctx, ids, filter)
	if err != nil || (len(rows) == 0 && total > 0) {
		return nil, 0, false
	}
	return rows, total, true
}

func applyMediaViewLIKEFilter(q *gorm.DB, query string) *gorm.DB {
	for _, term := range mediaSearchTerms(query) {
		like := "%" + escapeLike(term) + "%"
		q = q.Where("(mi.title LIKE ? ESCAPE '\\' OR mi.original_name LIKE ? ESCAPE '\\' OR mi.overview LIKE ? ESCAPE '\\' OR mi.genres LIKE ? ESCAPE '\\' OR m.scan_title LIKE ? ESCAPE '\\' OR m.path LIKE ? ESCAPE '\\')",
			like, like, like, like, like, like)
	}
	return q
}

func (r *MediaViewRepository) SearchFiltered(ctx context.Context, query string, limit int, filter MediaQueryFilter) ([]model.MediaView, error) {
	rows, _, err := r.SearchFilteredPage(ctx, query, 0, limit, filter)
	return rows, err
}

func (r *MediaViewRepository) reindexMetadataBestEffort(ctx context.Context, metadataID string) {
	if r == nil || r.db == nil || strings.TrimSpace(metadataID) == "" {
		return
	}
	var ids []string
	err := r.db.WithContext(ctx).
		Table("media AS m").
		Select("m.id").
		Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id AND mi.deleted_at IS NULL").
		Joins("LEFT JOIN metadata_items AS season_metadata ON season_metadata.id = mi.parent_id AND mi.kind = 'episode' AND season_metadata.deleted_at IS NULL").
		Where("m.deleted_at IS NULL AND (m.metadata_id = ? OR mi.parent_id = ? OR season_metadata.parent_id = ?)", metadataID, metadataID, metadataID).
		Find(&ids).Error
	if err == nil {
		r.indexMediaIDsBestEffort(ctx, ids)
	}
}

// ReindexMediaIDs refreshes external-search documents after a media row gains
// or changes its shared metadata link.
func (r *MediaViewRepository) ReindexMediaIDs(ctx context.Context, ids ...string) {
	r.indexMediaIDsBestEffort(ctx, ids)
}
