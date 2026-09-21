package repository

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	COALESCE(NULLIF(mi.release_date, ''), CASE WHEN mi.kind = 'episode' THEN (
		SELECT NULLIF(previous_episode.release_date, '')
		FROM metadata_items AS previous_episode
		WHERE previous_episode.kind = 'episode'
			AND previous_episode.parent_id = mi.parent_id
			AND previous_episode.episode_num < mi.episode_num
			AND NULLIF(previous_episode.release_date, '') IS NOT NULL
		ORDER BY previous_episode.episode_num DESC
		LIMIT 1
	) END, '') AS view_release_date,
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
	COALESCE(poster_asset.id, CASE WHEN mi.kind = 'season' THEN series_poster_asset.id END, '') AS view_poster_asset_id,
	COALESCE(still_asset.id, backdrop_asset.id,
		CASE WHEN mi.kind = 'episode' THEN series_backdrop_asset.id END,
		CASE WHEN mi.kind = 'episode' THEN series_poster_asset.id END, '') AS view_backdrop_asset_id,
	COALESCE(pm.duration_ms, 0) AS view_probe_duration_ms,
	COALESCE(pm.size_bytes, 0) AS view_probe_size_bytes,
	COALESCE(pm.container, '') AS view_probe_container,
	COALESCE(pm.width, 0) AS view_probe_width,
	COALESCE(pm.height, 0) AS view_probe_height,
	COALESCE(pm.video_codec, '') AS view_probe_video_codec,
	COALESCE(pm.audio_codec, '') AS view_probe_audio_codec`

// MediaViewRepository 对共享元数据完成 JOIN 后再执行权限、排序和分页。
type MediaViewRepository struct {
	db *gorm.DB
	searchIndex
}

// searchIndex 协调各资料来源的独立索引，重建期间追补已提交的变更。
type searchIndex struct {
	searchBackend MediaSearchBackend
	searchMu      sync.Mutex
	searchRebuild bool
	searchDirty   map[string]struct{}
	searchFailed  atomic.Bool
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
		Joins("LEFT JOIN artwork_assets AS still_asset ON still_asset.id = still.asset_id").
		Joins("LEFT JOIN metadata_artworks AS series_poster ON series_poster.metadata_id = series_metadata.id AND series_poster.artwork_type = 'poster'").
		Joins("LEFT JOIN artwork_assets AS series_poster_asset ON series_poster_asset.id = series_poster.asset_id").
		Joins("LEFT JOIN metadata_artworks AS series_backdrop ON series_backdrop.metadata_id = series_metadata.id AND series_backdrop.artwork_type = 'backdrop'").
		Joins("LEFT JOIN artwork_assets AS series_backdrop_asset ON series_backdrop_asset.id = series_backdrop.asset_id")
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
		var err error
		rows, err = r.nfoViewsByIDs(ctx, []string{id}, MediaQueryFilter{IncludeNSFW: true})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return &rows[0], nil
		}
		rows, err = r.hongGuoViewsByIDs(ctx, []string{id}, MediaQueryFilter{IncludeNSFW: true})
		if err != nil || len(rows) == 0 {
			return nil, err
		}
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
	missing := make([]string, 0)
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			missing = append(missing, id)
		}
	}
	sourceRows, err := r.hongGuoViewsByIDs(ctx, missing, filter)
	if err != nil {
		return nil, err
	}
	for _, row := range sourceRows {
		byID[row.ID] = row
	}
	nfoRows, err := r.nfoViewsByIDs(ctx, missing, filter)
	if err != nil {
		return nil, err
	}
	for _, row := range nfoRows {
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
	var localIDs, ordinaryIDs []string
	for _, id := range metadataIDs {
		if strings.HasPrefix(id, "nfo-") {
			localIDs = append(localIDs, strings.TrimPrefix(id, "nfo-"))
		} else {
			ordinaryIDs = append(ordinaryIDs, id)
		}
	}
	if len(localIDs) > 0 {
		rows, err := scanNFOViews(r.nfoViewQuery(ctx, filter).Where("ni.id IN ? OR ns.id IN ? OR nw.id IN ?", localIDs, localIDs, localIDs))
		if err != nil || len(ordinaryIDs) == 0 {
			return rows, err
		}
		ordinary, err := r.FindByLogicalMetadataIDs(ctx, ordinaryIDs, filter)
		return append(rows, ordinary...), err
	}
	// 先限定请求作品的文件，再关联展示字段，避免跨层级 OR 扫描全库媒体。
	candidates := r.logicalMetadataCandidates(ctx, metadataIDs)
	q := applyMediaViewFilter(r.query(ctx).
		Table("(SELECT * FROM media WHERE metadata_id IN (?) OFFSET 0) AS m", candidates), filter).
		Order("m.created_at DESC, m.id DESC")
	var rows []model.MediaView
	if err := scanMediaViews(q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// logicalMetadataCandidates 保留请求项本身，仅为整剧展开类型匹配的季和分集。
func (r *MediaViewRepository) logicalMetadataCandidates(ctx context.Context, metadataIDs []string) *gorm.DB {
	return r.db.WithContext(ctx).Raw(`WITH requested AS MATERIALIZED (
		SELECT id, kind FROM metadata_items WHERE id = ANY(?)
	)
	SELECT id FROM requested
	UNION ALL
	SELECT s.id FROM requested r
	JOIN metadata_items s ON s.parent_id = r.id AND s.kind = 'season'
	WHERE r.kind = 'series'
	UNION ALL
	SELECT e.id FROM requested r
	JOIN metadata_items s ON s.parent_id = r.id AND s.kind = 'season'
	JOIN metadata_items e ON e.parent_id = s.id AND e.kind = 'episode'
	WHERE r.kind = 'series'`, &metadataIDs)
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
	// 从收藏展开作品、季和集，避免先给整个元数据库反查媒体文件。
	base := r.db.WithContext(ctx).
		Table("favorites AS f").
		Joins("JOIN metadata_items AS favorite_metadata ON favorite_metadata.id = f.metadata_id AND favorite_metadata.kind IN ('movie', 'series')").
		Joins(`JOIN LATERAL (
SELECT favorite_metadata.id
UNION ALL
SELECT s.id FROM metadata_items s
WHERE favorite_metadata.kind = 'series' AND s.parent_id = favorite_metadata.id AND s.kind = 'season'
UNION ALL
SELECT e.id FROM metadata_items s
JOIN metadata_items e ON e.parent_id = s.id AND e.kind = 'episode'
WHERE favorite_metadata.kind = 'series' AND s.parent_id = favorite_metadata.id AND s.kind = 'season'
) AS candidate ON TRUE`).
		Joins("JOIN metadata_items AS mi ON mi.id = candidate.id").
		Joins("JOIN media AS m ON m.metadata_id = mi.id").
		Where("f.user_id = ? AND f.deleted_at IS NULL", userID)
	base = applyMediaViewFilter(base, filter)
	if !filter.IncludeNSFW {
		base = base.Where("COALESCE(favorite_metadata.nsfw, FALSE) = FALSE")
	}
	type favoriteCard struct {
		MediaID    string `gorm:"column:media_id"`
		MetadataID string `gorm:"column:metadata_id"`
	}
	var cards []favoriteCard
	ranked := base.Select("m.id AS media_id, f.metadata_id, f.id AS favorite_id, f.created_at AS favorite_created_at, ROW_NUMBER() OVER (PARTITION BY f.id ORDER BY m.created_at DESC, m.id DESC) AS favorite_rank")
	if err := r.db.WithContext(ctx).Table("(?) AS favorite_cards", ranked).
		Where("favorite_rank = 1").Order("favorite_created_at DESC, favorite_id DESC").Scan(&cards).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cards))
	metadataIDs := make([]string, 0, len(cards))
	metadataByMedia := make(map[string]string, len(cards))
	for _, card := range cards {
		ids = append(ids, card.MediaID)
		metadataIDs = append(metadataIDs, card.MetadataID)
		metadataByMedia[card.MediaID] = card.MetadataID
	}
	views, err := r.FindByIDs(ctx, ids, filter)
	if err != nil || len(views) == 0 {
		return views, err
	}
	presentations, err := r.metadataSearchPresentations(ctx, metadataIDs)
	if err != nil {
		return nil, err
	}
	out := views[:0]
	for _, view := range views {
		if presentation, ok := presentations[metadataByMedia[view.ID]]; ok {
			applyMetadataSearchPresentation(&view, presentation)
			if presentation.Kind == model.MetadataKindSeries {
				view.SeasonID, view.SeasonNum, view.EpisodeNum = "", 0, 0
			}
			out = append(out, view)
		}
	}
	return out, attachMediaViewDoubanRatings(r.db.WithContext(ctx), out)
}

// ListRecentLogicalWorks selects the logical work page in SQL before loading
// the versions needed to build cards.
func (r *MediaViewRepository) ListRecentLogicalWorks(ctx context.Context, limit int, filter MediaQueryFilter) ([]model.MediaView, error) {
	if limit <= 0 {
		limit = 24
	}
	if has, err := (&NFORepository{db: r.db}).HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		key := "CASE WHEN mi.kind IN ('episode','season') THEN COALESCE(series_metadata.id,mi.id) ELSE mi.id END"
		ordinary := applyMediaViewFilter(r.query(ctx), filter).Select(key + " AS id,MAX(m.created_at) AS latest").Group(key)
		local := r.nfoViewQuery(ctx, filter).Select("'nfo-' || COALESCE(nw.id,ni.id) AS id,MAX(m.created_at) AS latest").Group("COALESCE(nw.id,ni.id)")
		var ids []string
		if err := r.db.WithContext(ctx).Table("(?) AS works", r.db.Raw("? UNION ALL ?", ordinary, local)).Order("latest DESC,id DESC").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		return r.FindByLogicalMetadataIDs(ctx, ids, filter)
	}
	logicalID := "CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.id, mi.id) ELSE mi.id END"
	// 按文件时间逐批解析作品；跨过最后一部作品的时间边界后才能截断同时间 ID。
	// service 层已有相同游标规则，此处在仓储内保留 MediaView 的完整过滤语义。
	const batchSize = 128
	type candidate struct {
		ID        string
		CreatedAt sql.NullTime
	}
	latest := map[string]time.Time{}
	var cursor candidate
	// 高重复或低命中筛选超过 16 批时回退聚合，避免全库逐批往返；不截断结果。
	for batchNumber := 0; batchNumber < 16; batchNumber++ {
		q := r.db.WithContext(ctx).Table("media").Select("id, created_at").Where("metadata_id IS NOT NULL")
		if len(filter.AllowedLibraryIDs) > 0 {
			q = q.Where("library_id = ANY(?)", &filter.AllowedLibraryIDs)
		}
		if len(filter.HiddenLibraryIDs) > 0 {
			q = q.Where("library_id <> ALL(?)", &filter.HiddenLibraryIDs)
		}
		if cursor.ID != "" {
			q = q.Where("(created_at, id) < (?, ?)", cursor.CreatedAt.Time, cursor.ID)
		}
		var candidates []candidate
		if err := q.Order("created_at DESC, id DESC").Limit(batchSize).Scan(&candidates).Error; err != nil {
			return nil, err
		}
		// PostgreSQL 的 MAX 忽略 NULL，而 DESC 将全 NULL 作品放在最前，交回原聚合处理。
		if len(candidates) > 0 && !candidates[0].CreatedAt.Valid {
			break
		}
		mediaIDs := make([]string, 0, len(candidates))
		for _, row := range candidates {
			mediaIDs = append(mediaIDs, row.ID)
		}
		if len(mediaIDs) > 0 {
			var works []struct {
				ID        string
				CreatedAt time.Time
			}
			batch := applyMediaViewFilter(r.query(ctx).Where("m.id IN ?", mediaIDs), filter)
			if err := batch.Select(logicalID + " AS id, MAX(m.created_at) AS created_at").Group(logicalID).Scan(&works).Error; err != nil {
				return nil, err
			}
			for _, work := range works {
				if previous, ok := latest[work.ID]; !ok || work.CreatedAt.After(previous) {
					latest[work.ID] = work.CreatedAt
				}
			}
		}
		ids := make([]string, 0, len(latest))
		for id := range latest {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if latest[ids[i]].Equal(latest[ids[j]]) {
				return ids[i] > ids[j]
			}
			return latest[ids[i]].After(latest[ids[j]])
		})
		if len(ids) > limit {
			ids = ids[:limit]
		}
		if len(candidates) < batchSize || (len(ids) == limit && candidates[len(candidates)-1].CreatedAt.Time.Before(latest[ids[len(ids)-1]])) {
			return r.FindByLogicalMetadataIDs(ctx, ids, filter)
		}
		cursor = candidates[len(candidates)-1]
	}
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
	if has, err := (&NFORepository{db: r.db}).HasMedia(ctx); err != nil {
		return nil, 0, err
	} else if has {
		ordinary := q.Select("m.id, COALESCE(mi.release_date,'') AS release_date, COALESCE(mi.year,m.scan_year,0) AS year, m.updated_at,m.created_at")
		local := r.nfoViewQuery(ctx, filter).Where("m.library_id = ANY(?)", &libraryIDs).Select("m.id, COALESCE(b.release_date,'') AS release_date, b.year, m.updated_at,m.created_at")
		combined := r.db.WithContext(ctx).Table("(?) AS files", r.db.Raw("? UNION ALL ?", ordinary, local))
		var total int64
		if err := combined.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, 0, err
		}
		var ids []string
		if err := combined.Order("release_date DESC, year DESC, updated_at DESC, created_at DESC, id DESC").Offset(offset).Limit(limit).Pluck("id", &ids).Error; err != nil {
			return nil, 0, err
		}
		rows, err := r.FindByIDs(ctx, ids, filter)
		return rows, total, err
	}
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
