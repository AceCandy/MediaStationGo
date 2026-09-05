package repository

import (
	"context"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const mediaViewSelect = `
m.*,
	COALESCE(CASE WHEN mi.kind IN ('episode', 'season') THEN series_metadata.id WHEN mi.kind = 'series' THEN mi.id ELSE NULL END, '') AS view_series_id,
	COALESCE(CASE WHEN mi.kind IN ('episode', 'season') THEN series_metadata.title WHEN mi.kind = 'series' THEN mi.title ELSE NULL END, '') AS view_series_title,
	COALESCE(CASE WHEN mi.kind = 'episode' THEN season_metadata.id WHEN mi.kind = 'season' THEN mi.id ELSE NULL END, '') AS view_season_id,
	COALESCE(NULLIF(mi.title, ''), m.scan_title) AS view_title,
	COALESCE(mi.original_name, '') AS view_original_name,
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
	COALESCE(still_asset.id, backdrop_asset.id, '') AS view_backdrop_asset_id,
	COALESCE(pm.duration_ms, 0) AS view_probe_duration_ms,
	COALESCE(pm.size_bytes, 0) AS view_probe_size_bytes,
	COALESCE(pm.container, '') AS view_probe_container,
	COALESCE(pm.width, 0) AS view_probe_width,
	COALESCE(pm.height, 0) AS view_probe_height,
	COALESCE(pm.video_codec, '') AS view_probe_video_codec,
	COALESCE(pm.audio_codec, '') AS view_probe_audio_codec`

// MediaViewRepository 对共享元数据完成 JOIN 后再执行权限、排序和分页。
type MediaViewRepository struct {
	db            *gorm.DB
	searchBackend MediaSearchBackend
	searchMu      sync.Mutex
	searchRebuild bool
	searchDirty   map[string]struct{}
}

func (r *MediaViewRepository) SetSearchBackend(backend MediaSearchBackend) {
	if r != nil {
		r.searchBackend = backend
	}
}

func (r *MediaViewRepository) query(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("media AS m").
		Joins("LEFT JOIN media_probe_metadata AS pm ON pm.media_id = m.id").
		Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id").
		Joins("LEFT JOIN metadata_items AS season_metadata ON season_metadata.id = mi.parent_id AND mi.kind = 'episode' AND season_metadata.kind = 'season'").
		Joins("LEFT JOIN metadata_items AS series_metadata ON series_metadata.id = CASE WHEN mi.kind = 'episode' THEN season_metadata.parent_id WHEN mi.kind = 'season' THEN mi.parent_id ELSE NULL END AND series_metadata.kind = 'series'").
		Joins(`LEFT JOIN LATERAL (
			SELECT
				MIN(CASE WHEN mid.provider = 'tmdb' THEN mid.external_id END) AS tmdb_external_id,
				MIN(CASE WHEN mid.provider = 'bangumi' THEN mid.external_id END) AS bangumi_external_id,
				MIN(CASE WHEN mid.provider = 'douban' THEN mid.external_id END) AS douban_external_id,
				MIN(CASE WHEN mid.provider = 'thetvdb' THEN mid.external_id END) AS thetvdb_external_id
			FROM metadata_identifiers AS mid
			WHERE mid.metadata_id = mi.id AND mid.entity_kind = mi.kind
		) AS identifiers ON TRUE`).
		Joins("LEFT JOIN metadata_artworks AS poster ON poster.metadata_id = mi.id AND poster.artwork_type = 'poster'").
		Joins("LEFT JOIN artwork_assets AS poster_asset ON poster_asset.id = poster.asset_id").
		Joins("LEFT JOIN metadata_artworks AS backdrop ON backdrop.metadata_id = mi.id AND backdrop.artwork_type = 'backdrop'").
		Joins("LEFT JOIN artwork_assets AS backdrop_asset ON backdrop_asset.id = backdrop.asset_id").
		Joins("LEFT JOIN metadata_artworks AS still ON still.metadata_id = mi.id AND still.artwork_type = 'still'").
		Joins("LEFT JOIN artwork_assets AS still_asset ON still_asset.id = still.asset_id")
}

func applyMediaViewFilter(q *gorm.DB, filter MediaQueryFilter) *gorm.DB {
	if !filter.IncludeNSFW {
		q = q.Where("COALESCE(mi.nsfw, FALSE) = FALSE")
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if filter.MissingPoster {
		q = q.Where("poster_asset.id IS NULL")
	}
	if filter.MissingChineseTitle {
		q = q.Where(`COALESCE(NULLIF(mi.title, ''), m.scan_title) !~ '[㐀-䶿一-鿿豈-﫿]'`)
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
	return attachMediaViewDoubanRatings(q.Session(&gorm.Session{NewDB: true}), *views)
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
	return r.FindByMetadataIDs(ctx, []string{metadataID}, filter)
}

// FindByMetadataIDs 返回多个作品当前可见的全部播放版本。
func (r *MediaViewRepository) FindByMetadataIDs(ctx context.Context, metadataIDs []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(metadataIDs) == 0 {
		return []model.MediaView{}, nil
	}
	var rows []model.MediaView
	q := applyMediaViewFilter(r.query(ctx).Where("m.metadata_id IN ?", metadataIDs), filter).
		Order("m.metadata_id, m.updated_at DESC, m.created_at DESC, m.id DESC")
	if err := scanMediaViews(q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// FindByLogicalMetadataIDs returns visible versions belonging to the requested
// works. Episode and season media are addressed by their parent series ID.
func (r *MediaViewRepository) FindByLogicalMetadataIDs(ctx context.Context, metadataIDs []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(metadataIDs) == 0 {
		return []model.MediaView{}, nil
	}
	logicalID := "CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.id, mi.id) ELSE mi.id END"
	q := applyMediaViewFilter(r.query(ctx).Where("m.metadata_id IN ? OR "+logicalID+" IN ?", metadataIDs, metadataIDs), filter).
		Order("m.created_at DESC, m.id DESC")
	var rows []model.MediaView
	if err := scanMediaViews(q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MediaViewRepository) FindByLogicalMetadataID(ctx context.Context, metadataID string, filter MediaQueryFilter) (*model.MediaView, error) {
	rows, err := r.FindByLogicalMetadataIDs(ctx, []string{metadataID}, filter)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ListFavoriteCards 为每条收藏只加载一个当前可见的媒体版本。
func (r *MediaViewRepository) ListFavoriteCards(ctx context.Context, userID string, filter MediaQueryFilter) ([]model.MediaView, error) {
	logicalID := "CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.id, mi.id) ELSE mi.id END"
	base := r.db.WithContext(ctx).
		Table("media AS m").
		Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id").
		Joins("LEFT JOIN metadata_items AS season_metadata ON season_metadata.id = mi.parent_id AND mi.kind = 'episode' AND season_metadata.kind = 'season'").
		Joins("LEFT JOIN metadata_items AS series_metadata ON series_metadata.id = CASE WHEN mi.kind = 'episode' THEN season_metadata.parent_id WHEN mi.kind = 'season' THEN mi.parent_id ELSE NULL END AND series_metadata.kind = 'series'").
		Joins("JOIN favorites AS f ON f.user_id = ? AND f.deleted_at IS NULL AND (m.metadata_id = f.metadata_id OR "+logicalID+" = f.metadata_id)", userID)
	base = applyMediaViewFilter(base, filter)
	type favoriteCard struct {
		MediaID string `gorm:"column:media_id"`
	}
	var cards []favoriteCard
	ranked := base.Select("m.id AS media_id, f.id AS favorite_id, f.created_at AS favorite_created_at, ROW_NUMBER() OVER (PARTITION BY f.id ORDER BY m.created_at DESC, m.id DESC) AS favorite_rank")
	if err := r.db.WithContext(ctx).Table("(?) AS favorite_cards", ranked).
		Where("favorite_rank = 1").Order("favorite_created_at DESC, favorite_id DESC").Scan(&cards).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.MediaID)
	}
	return r.FindByIDs(ctx, ids, filter)
}

// ListRecentLogicalWorks selects the logical work page in SQL before loading
// the versions needed to build cards.
func (r *MediaViewRepository) ListRecentLogicalWorks(ctx context.Context, limit int, filter MediaQueryFilter) ([]model.MediaView, error) {
	if limit <= 0 {
		limit = 24
	}
	logicalID := "CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.id, mi.id) ELSE mi.id END"
	base := applyMediaViewFilter(r.query(ctx), filter)
	type logicalRow struct {
		ID string `gorm:"column:logical_id"`
	}
	var ids []logicalRow
	if err := base.Select(logicalID + " AS logical_id").Group(logicalID).
		Order("MAX(m.created_at) DESC, logical_id DESC").Limit(limit).Scan(&ids).Error; err != nil {
		return nil, err
	}
	logicalIDs := make([]string, 0, len(ids))
	for _, row := range ids {
		if strings.TrimSpace(row.ID) != "" {
			logicalIDs = append(logicalIDs, row.ID)
		}
	}
	return r.FindByLogicalMetadataIDs(ctx, logicalIDs, filter)
}

func (r *MediaViewRepository) ListByLibrariesFiltered(ctx context.Context, libraryIDs []string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, int64, error) {
	if len(libraryIDs) == 0 {
		return []model.MediaView{}, 0, nil
	}
	q := applyMediaViewFilter(r.query(ctx).Where("m.library_id = ANY(?)", &libraryIDs), filter)
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
	searchFilter := MetadataSearchFilter{
		MediaQueryFilter: filter,
		Fields:           MetadataSearchFieldsWeb,
		Kinds:            []string{model.MetadataKindMovie, model.MetadataKindSeries},
	}
	ids, total, err := r.SearchMetadataIDs(ctx, query, offset, limit, searchFilter)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.FindMetadataSearchRepresentatives(ctx, ids, filter)
	return rows, total, err
}

func (r *MediaViewRepository) SearchFiltered(ctx context.Context, query string, limit int, filter MediaQueryFilter) ([]model.MediaView, error) {
	rows, _, err := r.SearchFilteredPage(ctx, query, 0, limit, filter)
	return rows, err
}
