package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// hierarchyItems 将季/分集浏览留在 SQL 分页边界，详情读取仍由 Item 负责。
func (e *EmbyService) hierarchyItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	if p.ParentID == "" {
		return nil, false, nil
	}
	parent, err := e.repo.Metadata.FindByID(ctx, p.ParentID)
	if err != nil {
		return nil, false, err
	}
	if parent == nil || (parent.Kind != model.MetadataKindSeries && parent.Kind != model.MetadataKindSeason) {
		return nil, false, nil
	}
	q := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), p.UserID)
	q = seriesScopeQuery(q).Where("media.season_num > 0 OR media.episode_num > 0")
	if parent.Kind == model.MetadataKindSeason {
		q = q.Where("scope_season.id = ?", parent.ID)
	} else {
		q = q.Where("scope_series.id = ?", parent.ID)
	}
	if parent.Kind == model.MetadataKindSeason || p.Recursive || containsItemType(p.IncludeItemTypes, "Episode") {
		views, total, err := e.metadataPage(ctx, q, p.UserID, "MIN(scope_season.season_num), MIN(emby_metadata.episode_num), MIN(media.created_at), media.metadata_id", p.StartIndex, p.Limit)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"Items": e.payloadsForViewsWithFields(ctx, views, p.UserID, p.Fields), "TotalRecordCount": int(total), "StartIndex": p.StartIndex}, true, nil
	}
	var total int64
	groups := q.Session(&gorm.Session{}).Select("scope_season.id").Group("scope_season.id")
	if err := e.repo.DB.WithContext(ctx).Table("(?) AS seasons", groups).Count(&total).Error; err != nil {
		return nil, true, err
	}
	var rows []struct {
		ID           string
		SeasonNum    int
		EpisodeCount int
		LibraryID    string
		SeriesName   string
	}
	err = q.Session(&gorm.Session{}).Select(`scope_season.id AS id, MIN(scope_season.season_num) AS season_num,
		COUNT(DISTINCT media.metadata_id) AS episode_count, MIN(media.library_id) AS library_id, MIN(scope_series.title) AS series_name`).
		Group("scope_season.id").Order("MIN(scope_season.season_num), scope_season.id").Offset(p.StartIndex).Limit(p.Limit).Scan(&rows).Error
	if err != nil {
		return nil, true, err
	}
	seasons := make([]embySeasonGroup, 0, len(rows))
	for _, row := range rows {
		seasons = append(seasons, embySeasonGroup{ID: row.ID, SeriesID: parent.ID, LibraryID: row.LibraryID, SeasonNum: row.SeasonNum,
			EpisodeCount: row.EpisodeCount, Series: embySeriesGroup{ID: parent.ID, Name: row.SeriesName}})
	}
	return map[string]any{"Items": e.seasonPayloadsWithFields(ctx, seasons, p.UserID, p.Fields), "TotalRecordCount": int(total), "StartIndex": p.StartIndex}, true, nil
}
