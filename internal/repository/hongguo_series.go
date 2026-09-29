package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

const hongGuoSeriesIdentity = "CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END"
const hongGuoSeasonNumber = "CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END"

// hongGuoFileScope 统一作品存在性判断与当前页文件统计的可见范围。
func (r *MediaViewRepository) hongGuoFileScope(ctx context.Context, libraryID string, filter MediaQueryFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("media m").
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
		Where("m.catalog_source = 'hongguo'")
	if libraryID != "" {
		q = q.Where("m.library_id = ?", libraryID)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	return q
}

// hongGuoSeriesScope 先限定可见文件，再按官方关系投影剧集；空库 ID 表示所有可见库。
func (r *MediaViewRepository) hongGuoSeriesScope(ctx context.Context, libraryID, seriesID string, filter MediaQueryFilter) *gorm.DB {
	q := r.hongGuoFileScope(ctx, libraryID, filter).
		Joins("JOIN hongguo_works w ON w.id = b.work_id").Joins(HongGuoAlbumJoin)
	if seriesID != "" {
		// 先用原生身份限定绑定；仅按展示 CASE 筛选会遍历整个库的文件。
		if albumID, ok := strings.CutPrefix(seriesID, "hg-group-"); ok {
			works := r.db.Table("hongguo_works").Select("id").Where("related_album_id = ? AND related_album_id <> '' AND kind = 'series' AND season_index > 0", albumID)
			q = q.Where("b.work_id IN (?)", works)
		} else if workID, ok := strings.CutPrefix(seriesID, "hg-work-"); ok {
			q = q.Where("b.work_id = ?", workID)
		} else {
			q = q.Where("FALSE")
		}
		q = q.Where(hongGuoSeriesIdentity+" = ?", seriesID)
	}
	return q
}

// hongGuoLibraryPage 仅加载当前页代表文件；封面和筛选都以整剧主体资料为准。
func (r *MediaViewRepository) hongGuoLibraryPage(ctx context.Context, libraryID, seriesID string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, []LibraryMetadataSummary, int64, error) {
	visible := r.hongGuoFileScope(ctx, libraryID, filter).Select("1").Where("b.work_id = w.id")
	albums := r.db.WithContext(ctx).Table("hongguo_works").
		Where("kind = 'series' AND related_album_id <> '' AND season_index > 0").
		Select("DISTINCT ON (related_album_id) related_album_id AS id, id AS work_id, title").
		Order("related_album_id, season_index, source_id")
	q := r.db.WithContext(ctx).Table("hongguo_works w").Where(HongGuoReadyWorkSQL).
		Joins("LEFT JOIN albums g ON w.kind = 'series' AND g.id = w.related_album_id").
		Where("CASE WHEN w.library_ids IS NULL THEN EXISTS (? OFFSET 0) ELSE TRUE END", visible)
	q = FilterVisibleWorkLibraries(r.db.WithContext(ctx), q, "w.library_ids", []string{libraryID}, filter)
	if seriesID != "" {
		q = FilterHongGuoWorkIDs(q, []string{seriesID}).Where(HongGuoWorkIdentitySQL+" = ?", seriesID)
		albumID, _ := strings.CutPrefix(seriesID, "hg-group-")
		albums = albums.Where("related_album_id = ?", albumID)
	}
	if filter.MissingPoster {
		q = q.Where("NOT EXISTS (SELECT 1 FROM hongguo_artworks a WHERE a.work_id = COALESCE(g.work_id,w.id) AND a.local_key <> '')")
	}
	if filter.MissingChineseTitle {
		q = q.Where("COALESCE(g.title,w.title) !~ '[㐀-䶿一-鿿豈-﫿]'")
	}
	q = q.Select("w.id AS work_id, " + HongGuoWorkIdentitySQL + " AS metadata_id, COALESCE(g.title,w.title) AS title")
	page := r.db.Table("works").Order("title, metadata_id").Offset(offset).Limit(limit)
	var rows []struct {
		WorkID string
		Total  int64
	}
	// 存在性查询保留逐作品索引查找边界，总数和分页共用一次作品范围计算。
	err := r.db.WithContext(ctx).Raw(`WITH albums AS MATERIALIZED (?), scoped AS MATERIALIZED (?), works AS MATERIALIZED (
SELECT metadata_id, title FROM scoped GROUP BY metadata_id, title
), page AS MATERIALIZED (?), page_works AS (
SELECT scoped.work_id, page.metadata_id, page.title FROM page JOIN scoped USING (metadata_id)
)
SELECT page_works.work_id, totals.total FROM (SELECT COUNT(*) AS total FROM works) totals
LEFT JOIN page_works ON TRUE ORDER BY page_works.title, page_works.metadata_id`, albums, q, page).Scan(&rows).Error
	if err != nil {
		return nil, nil, 0, err
	}
	var total int64
	workIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		total = row.Total
		if row.WorkID != "" {
			workIDs = append(workIDs, row.WorkID)
		}
	}
	if len(workIDs) == 0 {
		return nil, nil, total, nil
	}
	// 显式传入当页作品 ID，避免分页连接的基数高估触发昂贵的 JIT 优化。
	var summaries []LibraryMetadataSummary
	err = r.hongGuoSeriesScope(ctx, libraryID, "", filter).
		Joins("JOIN hongguo_works primary_work ON primary_work.id = COALESCE(g.work_id,w.id)").
		Where("b.work_id = ANY(?)", &workIDs).
		Select(hongGuoSeriesIdentity + ` AS metadata_id,
(ARRAY_AGG(m.id ORDER BY ` + hongGuoSeasonNumber + `,w.source_id,m.episode_num,m.id))[1] AS media_id,
COUNT(DISTINCT COALESCE(b.episode_id,w.id)) AS count, COUNT(*) AS version_count`).
		Group(hongGuoSeriesIdentity + ",primary_work.title").Order("primary_work.title,metadata_id").Scan(&summaries).Error
	if err != nil {
		return nil, nil, 0, err
	}
	ids := make([]string, 0, len(summaries))
	for _, row := range summaries {
		ids = append(ids, row.MediaID)
	}
	filter.MissingPoster, filter.MissingChineseTitle = false, false
	views, err := r.FindByIDs(ctx, ids, filter)
	return views, summaries, total, err
}

// hongGuoSearchRepresentatives 将当页作品资料与可见文件合并，合集只占一个搜索位置。
func (r *MediaViewRepository) hongGuoSearchRepresentatives(ctx context.Context, ids []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	if len(ids) == 0 {
		return []model.MediaView{}, nil
	}
	// 先将当页合集解析为来源作品，文件查询才能使用 work_id 索引限定范围。
	var workIDs []string
	if err := FilterHongGuoWorkIDs(r.db.WithContext(ctx).Table("hongguo_works w"), ids).Joins(HongGuoAlbumJoin).
		Where(hongGuoSeriesIdentity+" IN ?", ids).Pluck("w.id", &workIDs).Error; err != nil {
		return nil, err
	}
	if len(workIDs) == 0 {
		return []model.MediaView{}, nil
	}
	var representatives []LibraryMetadataSummary
	err := r.hongGuoSeriesScope(ctx, "", "", filter).
		Where("b.work_id = ANY(?)", &workIDs).
		Where(hongGuoSeriesIdentity+" IN ?", ids).
		Select("DISTINCT ON (" + hongGuoSeriesIdentity + ") " + hongGuoSeriesIdentity + " AS metadata_id, m.id AS media_id").
		Order(hongGuoSeriesIdentity + "," + hongGuoSeasonNumber + ",w.source_id,m.episode_num,m.id").Scan(&representatives).Error
	if err != nil {
		return nil, err
	}
	mediaIDs := make([]string, 0, len(representatives))
	byMediaID := make(map[string]string, len(representatives))
	for _, row := range representatives {
		mediaIDs = append(mediaIDs, row.MediaID)
		byMediaID[row.MediaID] = row.MetadataID
	}
	views, err := r.FindByIDs(ctx, mediaIDs, filter)
	if err != nil {
		return nil, err
	}
	presentations, err := r.hongGuoPresentations(ctx, ids, false)
	if err != nil {
		return nil, err
	}
	result := make([]model.MediaView, 0, len(views))
	for _, view := range views {
		if presentation, ok := presentations[byMediaID[view.ID]]; ok {
			presentation.Media = view.Media
			result = append(result, presentation)
		}
	}
	return result, nil
}

// HongGuoSeriesForSource 兼容旧来源作品链接，只解析当前库内已匹配且可见的作品。
func (r *MediaViewRepository) HongGuoSeriesForSource(ctx context.Context, libraryID, sourceID string, filter MediaQueryFilter) (string, error) {
	var id string
	err := r.hongGuoSeriesScope(ctx, libraryID, "", filter).Where("w.source_id = ?", sourceID).
		Select(hongGuoSeriesIdentity).Limit(1).Scan(&id).Error
	return id, err
}

// hongGuoPresentations 批量读取整剧主体或指定季；第一季缺席时取最早已收录季。
func (r *MediaViewRepository) hongGuoPresentations(ctx context.Context, ids []string, season bool) (map[string]model.MediaView, error) {
	out := make(map[string]model.MediaView)
	if len(ids) == 0 {
		return out, nil
	}
	identity := hongGuoSeriesIdentity
	if season {
		identity = "'hg-season-' || w.id"
	}
	var rows []struct {
		model.HongGuoWork
		Identity  string
		ArtworkID string
	}
	q := r.db.WithContext(ctx).Table("hongguo_works w")
	if !season {
		q = FilterHongGuoWorkIDs(q, ids)
	}
	err := q.Joins(HongGuoAlbumJoin).
		Joins("LEFT JOIN hongguo_artworks a ON a.work_id = w.id AND a.local_key <> ''").
		Where(identity+" IN ?", ids).
		Select("DISTINCT ON (" + identity + ") w.*, " + identity + " AS identity, COALESCE(a.id,'') AS artwork_id").
		Order(identity + ",w.season_index,w.source_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var tags []string
		if err := json.Unmarshal([]byte(row.Tags), &tags); err != nil {
			return nil, err
		}
		view := model.MediaView{Media: model.Media{PermanentBase: row.PermanentBase, CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: row.SourceID},
			CatalogItemID: row.Identity, Title: row.Title, Overview: row.Overview, Rating: row.Rating,
			Genres: strings.Join(tags, ","), MetadataKind: row.Kind, MetadataSource: model.TaskSystemHongGuo}
		view.ID = row.Identity
		if row.ArtworkID != "" {
			view.PosterURL = "/api/catalogs/hongguo/artwork/" + row.ArtworkID
		}
		if season {
			view.MetadataKind = model.MetadataKindSeason
			view.SeasonNum = 1
			if row.RelatedAlbumID != "" && row.SeasonIndex > 0 {
				view.SeasonNum = row.SeasonIndex
			}
		} else if row.Kind == model.MetadataKindSeries {
			view.SeriesID, view.SeriesTitle = row.Identity, row.Title
		}
		out[row.Identity] = view
	}
	return out, nil
}

// HongGuoSeriesFavorite 仅校验目标合集有可见文件，收藏本身直接使用合集身份。
func (r *MediaViewRepository) HongGuoSeriesFavorite(ctx context.Context, userID, seriesID string, filter MediaQueryFilter, favorite *bool) (bool, error) {
	if userID == "" {
		return false, gorm.ErrRecordNotFound
	}
	files := r.hongGuoFileScope(ctx, "", filter).Select("1").Where("b.work_id = w.id")
	q := FilterHongGuoWorkIDs(r.db.WithContext(ctx).Table("hongguo_works w"), []string{seriesID}).Where("w.kind = 'series'")
	if strings.HasPrefix(seriesID, "hg-work-") {
		// 兼容尚未补齐合集的详情；已归组作品不能用旧 work ID 绕过展示身份。
		q = q.Where("w.related_album_id = '' OR w.season_index <= 0")
	}
	var itemID string
	err := q.
		Where("EXISTS (? OFFSET 0)", files).Select(HongGuoFavoriteIdentitySQL).Limit(1).Scan(&itemID).Error
	if err != nil {
		return false, err
	}
	if itemID == "" {
		return false, gorm.ErrRecordNotFound
	}
	if favorite != nil && strings.HasPrefix(seriesID, "hg-work-") {
		// 补齐关系可能与旧详情的收藏并发，复用源入口的行锁和身份重读。
		err := (&HongGuoRepository{db: r.db}).SetFavorite(ctx, userID, itemID, *favorite)
		return *favorite, err
	}
	return hongGuoFavorite(r.db.WithContext(ctx), userID, itemID, favorite)
}
