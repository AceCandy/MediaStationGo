package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const hongGuoSeriesIdentity = "CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END"
const hongGuoSeasonNumber = "CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END"

// hongGuoSeriesScope 先限定可见文件，再按官方关系投影剧集；空库 ID 表示所有可见库。
func (r *MediaViewRepository) hongGuoSeriesScope(ctx context.Context, libraryID, seriesID string, filter MediaQueryFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("media m").
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
		Joins("JOIN hongguo_works w ON w.id = b.work_id").Joins(HongGuoAlbumJoin).
		Where("m.catalog_source = 'hongguo'")
	if libraryID != "" {
		q = q.Where("m.library_id = ?", libraryID)
	}
	if seriesID != "" {
		q = q.Where(hongGuoSeriesIdentity+" = ?", seriesID)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	return q
}

// hongGuoLibraryPage 仅加载当前页代表文件；封面和筛选都以整剧主体资料为准。
func (r *MediaViewRepository) hongGuoLibraryPage(ctx context.Context, libraryID, seriesID string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, []LibraryMetadataSummary, int64, error) {
	q := r.hongGuoSeriesScope(ctx, libraryID, seriesID, filter).
		Joins("JOIN hongguo_works primary_work ON primary_work.id = COALESCE(g.work_id,w.id)")
	if filter.MissingPoster {
		q = q.Where("NOT EXISTS (SELECT 1 FROM hongguo_artworks a WHERE a.work_id = primary_work.id AND a.local_key <> '')")
	}
	if filter.MissingChineseTitle {
		q = q.Where("primary_work.title !~ '[㐀-䶿一-鿿豈-﫿]'")
	}
	groups := q.Select(hongGuoSeriesIdentity + ` AS metadata_id,
 (ARRAY_AGG(m.id ORDER BY ` + hongGuoSeasonNumber + `,w.source_id,m.episode_num,m.id))[1] AS media_id,
 COUNT(DISTINCT COALESCE(b.episode_id,w.id)) AS count, COUNT(*) AS version_count,
 primary_work.title`).Group(hongGuoSeriesIdentity + ",primary_work.title")
	var total int64
	if err := r.db.WithContext(ctx).Table("(?) cards", groups).Count(&total).Error; err != nil {
		return nil, nil, 0, err
	}
	var summaries []LibraryMetadataSummary
	if err := r.db.WithContext(ctx).Table("(?) cards", groups).Order("title,metadata_id").Offset(offset).Limit(limit).Scan(&summaries).Error; err != nil {
		return nil, nil, 0, err
	}
	ids := make([]string, 0, len(summaries))
	for _, card := range summaries {
		ids = append(ids, card.MediaID)
	}
	filter.MissingPoster, filter.MissingChineseTitle = false, false
	views, err := r.FindByIDs(ctx, ids, filter)
	return views, summaries, total, err
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
	err := r.db.WithContext(ctx).Table("hongguo_works w").Joins(HongGuoAlbumJoin).
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

// HongGuoSeriesFavorite 聚合可见成员的收藏；写入仍使用各来源作品的独立身份。
func (r *MediaViewRepository) HongGuoSeriesFavorite(ctx context.Context, userID, seriesID string, filter MediaQueryFilter, favorite *bool) (bool, error) {
	q := r.hongGuoSeriesScope(ctx, "", seriesID, filter).Where("w.kind = 'series'")
	if favorite == nil {
		var value bool
		err := q.Joins("LEFT JOIN hongguo_user_states st ON st.source_id = w.source_id AND st.episode_number = 0 AND st.user_id = ?", userID).
			Select("COALESCE(BOOL_OR(st.favorite),FALSE)").Scan(&value).Error
		return value, err
	}
	var sources []string
	if err := q.Distinct("w.source_id").Pluck("w.source_id", &sources).Error; err != nil {
		return false, err
	}
	states := make([]model.HongGuoUserState, 0, len(sources))
	for _, source := range sources {
		states = append(states, model.HongGuoUserState{UserID: userID, SourceID: source, Favorite: *favorite})
	}
	if len(states) == 0 {
		return false, gorm.ErrRecordNotFound
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: clause.AssignmentColumns([]string{"favorite", "updated_at"})}).CreateInBatches(states, 500).Error
	return *favorite, err
}
