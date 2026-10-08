package repository

import (
	"context"
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
	mi.latest_media_added_at AS latest_media_added_at,
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
	searchBackend        MediaSearchBackend
	searchMu             sync.Mutex
	searchRebuild        bool
	searchDirty          map[string]struct{}
	searchFailed         atomic.Bool
	searchRebuildInvalid bool // 未能捕获已提交变更的身份时，本次重建不可激活。
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
		rows, err = r.nfoViewsByIDs(ctx, []string{id}, MediaQueryFilter{})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return &rows[0], nil
		}
		rows, err = r.hongGuoViewsByIDs(ctx, []string{id}, MediaQueryFilter{})
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			rows, err = r.huangGuoAIViewsByIDs(ctx, []string{id}, MediaQueryFilter{})
			if err != nil || len(rows) == 0 {
				return nil, err
			}
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
	hgaRows, err := r.huangGuoAIViewsByIDs(ctx, missing, filter)
	if err != nil {
		return nil, err
	}
	for _, row := range hgaRows {
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
	var localIDs, ordinaryIDs, hongGuoIDs, huangGuoAIIDs []string
	for _, id := range metadataIDs {
		if strings.HasPrefix(id, "hga-") {
			huangGuoAIIDs = append(huangGuoAIIDs, id)
		} else if strings.HasPrefix(id, "hg-") {
			hongGuoIDs = append(hongGuoIDs, id)
		} else if strings.HasPrefix(id, "nfo-") {
			localIDs = append(localIDs, strings.TrimPrefix(id, "nfo-"))
		} else {
			ordinaryIDs = append(ordinaryIDs, id)
		}
	}
	// 混合页按来源并发加载，单来源仍走原查询；结果保持红果、NFO、普通的旧拼接顺序。
	sources := [][]string{hongGuoIDs, nil, ordinaryIDs, huangGuoAIIDs}
	for _, id := range localIDs {
		sources[1] = append(sources[1], "nfo-"+id)
	}
	active := 0
	for _, ids := range sources {
		if len(ids) > 0 {
			active++
		}
	}
	if active > 1 {
		loadCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		var pending sync.WaitGroup
		var results [4][]model.MediaView
		for i, ids := range sources {
			if len(ids) > 0 {
				pending.Go(func() {
					var err error
					results[i], err = r.FindByLogicalMetadataIDs(loadCtx, ids, filter)
					if err != nil {
						cancel(err)
					}
				})
			}
		}
		pending.Wait()
		if err := context.Cause(loadCtx); err != nil {
			return nil, err
		}
		return append(append(append(results[0], results[1]...), results[2]...), results[3]...), nil
	}
	if len(huangGuoAIIDs) > 0 {
		return r.HuangGuoAIItemsViews(ctx, huangGuoAIIDs, filter)
	}
	if len(hongGuoIDs) > 0 {
		var workIDs []string
		if err := FilterHongGuoWorkIDs(r.db.WithContext(ctx).Table("hongguo_works w"), hongGuoIDs).Joins(HongGuoAlbumJoin).
			Where(hongGuoSeriesIdentity+" = ANY(?)", &hongGuoIDs).Pluck("w.id", &workIDs).Error; err != nil {
			return nil, err
		}
		var fileIDs []string
		if err := r.hongGuoSeriesScope(ctx, "", "", filter).Where("b.work_id = ANY(?)", &workIDs).
			Where(hongGuoSeriesIdentity+" = ANY(?)", &hongGuoIDs).Pluck("m.id", &fileIDs).Error; err != nil {
			return nil, err
		}
		return r.hongGuoViewsByIDs(ctx, fileIDs, filter)
	}
	if len(localIDs) > 0 {
		return scanNFOViews(r.nfoItemViewQuery(ctx, localIDs, filter))
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
	type favoriteCard struct {
		MediaID           string `gorm:"column:media_id"`
		MetadataID        string `gorm:"column:metadata_id"`
		FavoriteCreatedAt time.Time
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
	addedByMedia := make(map[string]time.Time, len(cards))
	for _, card := range cards {
		ids = append(ids, card.MediaID)
		metadataIDs = append(metadataIDs, card.MetadataID)
		metadataByMedia[card.MediaID] = card.MetadataID
		addedByMedia[card.MediaID] = card.FavoriteCreatedAt
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
			view.FavoriteAddedAt = addedByMedia[view.ID]
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
	db := r.db.WithContext(ctx)
	ordinary := db.Table("metadata_items recent").Select("recent.id, recent.latest_media_added_at AS latest").
		Where("recent.kind IN ('movie','series')")
	ordinary = FilterVisibleWorkLibraries(db, ordinary, "recent.library_ids", nil, filter).
		Select("recent.id, recent.id AS work_id, recent.kind, recent.library_ids, recent.latest_media_added_at AS latest, 'legacy' AS source, NULL::text[] AS work_ids")
	local := db.Table("nfo_items recent").
		Select("'nfo-' || recent.id AS id, recent.id AS work_id, recent.kind, NULL::jsonb AS library_ids, recent.latest_media_added_at AS latest, 'nfo' AS source, NULL::text[] AS work_ids").
		Where("recent.kind IN ('movie','series')")
	if len(filter.AllowedLibraryIDs) > 0 {
		local = local.Where("recent.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		local = local.Where("recent.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	// 合集时间按全局成员聚合一次，可见性只限制参加当前页资格检查的作品。
	albums := db.Table("hongguo_works").Where("kind='series' AND related_album_id<>'' AND season_index>0").
		Select("related_album_id AS id, MAX(latest_media_added_at) AS latest").Group("related_album_id")
	source := db.Table("hongguo_works w").Where(HongGuoReadyWorkSQL).
		Joins("LEFT JOIN albums g ON w.kind='series' AND g.id=w.related_album_id").
		Select(HongGuoWorkIdentitySQL + " AS id, " + HongGuoWorkIdentitySQL + " AS work_id, '' AS kind, NULL::jsonb AS library_ids, MAX(CASE WHEN w.kind='series' THEN g.latest ELSE w.latest_media_added_at END) AS latest, 'hongguo' AS source, ARRAY_AGG(w.id::text) AS work_ids").Group(HongGuoWorkIdentitySQL)
	source = FilterVisibleWorkLibraries(db, source, "w.library_ids", nil, filter)
	hga := FilterVisibleWorkLibraries(db, db.Table("huangguoai_works w").Where(huangGuoAIReadyWorkSQL), "w.library_ids", nil, filter).Select(huangGuoAIWorkIdentitySQL + " AS id,w.id AS work_id,w.kind,w.library_ids,w.latest_media_added_at AS latest,'huangguoai' AS source,ARRAY[w.id::text] AS work_ids")
	combined := db.Raw("WITH albums AS MATERIALIZED (?) ? UNION ALL ? UNION ALL ? UNION ALL ?", albums, ordinary, local, source, hga)
	candidates := db.Table("(?) recent_works", combined).
		Select("*, ROW_NUMBER() OVER (ORDER BY latest DESC NULLS LAST,id DESC) AS ordinal").Order("latest DESC NULLS LAST,id DESC")
	hongGuoFiles := r.hongGuoFileScope(ctx, "", filter).Select("1").Where("b.work_id=ANY(recent.work_ids)")
	ordinaryFiles := applyMediaViewFilter(r.query(ctx), filter).Select("1").
		Where(`m.metadata_id IN (SELECT recent.work_id UNION ALL
SELECT id FROM metadata_items WHERE parent_id=recent.work_id UNION ALL
SELECT leaf.id FROM metadata_items parent JOIN LATERAL (
SELECT id FROM metadata_items WHERE parent_id=parent.id OFFSET 0
) leaf ON TRUE WHERE parent.parent_id=recent.work_id)`).
		Where("CASE WHEN mi.kind IN ('episode','season') THEN series_metadata.id ELSE mi.id END = recent.work_id")
	localFiles := r.NFOCandidateFiles(ctx, filter, "recent.work_id").Select("1").Where("COALESCE(nw.id,ni.id)=recent.work_id")
	hgaFiles := r.huangGuoAIFileScope(ctx, filter).Select("1").Where("b.work_id=ANY(recent.work_ids)")
	qualification := "CASE WHEN source='legacy' THEN EXISTS (? OFFSET 0) WHEN source='nfo' THEN EXISTS (? OFFSET 0) WHEN source='huangguoai' THEN EXISTS (? OFFSET 0) ELSE EXISTS (? OFFSET 0) END"
	if !filter.MissingPoster && !filter.MissingChineseTitle {
		qualification = "CASE WHEN source='legacy' AND kind='movie' AND library_ids IS NOT NULL AND library_ids <> '[]'::jsonb AND latest IS NOT NULL THEN TRUE WHEN source='nfo' AND latest IS NOT NULL THEN TRUE ELSE " + qualification + " END"
	}
	eligible := db.Table("work_batch recent").Select("ordinal").Where(qualification, ordinaryFiles, localFiles, hgaFiles, hongGuoFiles)
	page, _, err := r.workBatchPage(ctx, candidates, eligible, 0, limit, false, true)
	if err != nil || len(page) == 0 {
		return []model.MediaView{}, err
	}
	ids := make([]string, 0, len(page))
	dates := make(map[string]*time.Time, len(page))
	ranks := make(map[string]int, len(page))
	for i, row := range page {
		ids = append(ids, row.ID)
		dates[row.ID] = row.Latest
		ranks[row.ID] = i
	}
	views, err := r.FindByLogicalMetadataIDs(ctx, ids, filter)
	fileRanks := make(map[string]int, len(views))
	for i := range views {
		id := views[i].MetadataID
		if views[i].CatalogItemID != "" {
			id = views[i].CatalogItemID
		}
		if views[i].SeriesID != "" {
			id = views[i].SeriesID
		}
		views[i].LatestMediaAddedAt = dates[id]
		fileRanks[views[i].ID] = ranks[id]
	}
	// 文件水合按来源分支返回，恢复候选页顺序后再交给各类消费者。
	sort.SliceStable(views, func(i, j int) bool { return fileRanks[views[i].ID] < fileRanks[views[j].ID] })
	return views, err
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
