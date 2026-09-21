package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// hongGuoGlobalItems 在 SQL 中合并逻辑身份并分页，不拼接两个来源各自的分页结果。
// 仅在存在独立来源文件时接管全局浏览，来源库内浏览保持独立。
func (e *EmbyService) hongGuoGlobalItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	if p.ParentID != "" || containsOnlyFolderItemTypes(p.IncludeItemTypes) || (strings.TrimSpace(p.SearchTerm) == "" && !p.Recursive && len(p.IncludeItemTypes) == 0 && len(p.Filters) == 0) {
		return nil, false, nil
	}
	var hasSource bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source IN ('hongguo','nfo'))").Scan(&hasSource).Error; err != nil {
		return nil, true, err
	}
	if !hasSource {
		return nil, false, nil
	}
	v := e.mediaVisibility(ctx, p.UserID)
	if v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0 {
		return emptyItemsEnvelope(p.StartIndex), true, nil
	}
	if containsEmbyFilter(p.Filters, "IsResumable") {
		result, err := e.globalResumeItems(ctx, p)
		return result, true, err
	}
	files := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), p.UserID).
		Select("media.metadata_id, media.created_at")
	// 每个文件最多展开自身、季、整剧；没有文件的资料不成为候选。
	legacy := e.repo.DB.WithContext(ctx).Table("(?) AS f", files).
		Joins("JOIN metadata_items leaf ON leaf.id = f.metadata_id").
		Joins("LEFT JOIN metadata_items parent ON parent.id = leaf.parent_id").
		Joins("LEFT JOIN metadata_items grandparent ON grandparent.id = parent.parent_id").
		Joins("JOIN metadata_items item ON item.id IN (leaf.id, parent.id, grandparent.id)").
		Joins("LEFT JOIN playback_histories h ON h.metadata_id = leaf.id AND h.user_id = ? AND h.deleted_at IS NULL", p.UserID).
		Joins("LEFT JOIN favorites fav ON fav.metadata_id = item.id AND fav.user_id = ? AND fav.deleted_at IS NULL", p.UserID).
		Select(`item.id, CASE WHEN item.kind = 'episode' THEN 'legacy:' || COALESCE(grandparent.id,item.id) ELSE 'legacy:' || item.id END AS resume_key,
 item.kind, item.title, MAX(f.created_at) AS created_at,
 COALESCE(MAX(h.watched_at),MAX(f.created_at)) AS played_at,
 BOOL_AND(COALESCE(h.completed,FALSE)) AS played,
 BOOL_OR(fav.id IS NOT NULL) AS favorite, MAX(COALESCE(h.position_ms,0)) AS position_ms,
 item.rating, COALESCE(item.release_date,'') AS release_date, item.year`).
		Group("item.id, grandparent.id")
	if !v.IncludeNSFW {
		legacy = legacy.Where("NOT COALESCE(item.nsfw,FALSE)")
	}
	if len(p.PersonIDs) > 0 {
		legacy = legacy.Where(`EXISTS (SELECT 1 FROM metadata_credits c
 WHERE c.person_id IN ? AND c.metadata_id = CASE WHEN item.kind = 'episode' THEN item.parent_id ELSE item.id END)`, p.PersonIDs)
	}
	source := e.hongGuoPersonFilter(ctx, e.hongGuoNodes(ctx, p.UserID, ""), p.UserID, "", p.PersonIDs).
		Select("id, resume_key, LOWER(kind) AS kind, title, latest_at AS created_at, COALESCE(played_at,latest_at) AS played_at, played, favorite, position_ms, rating, '' AS release_date, 0 AS year")
	combined := e.repo.DB.Raw("? UNION ALL ?", legacy, source)
	if hasNFO, err := e.repo.NFO.HasMedia(ctx); err != nil {
		return nil, true, err
	} else if hasNFO {
		local := e.nfoNodes(ctx, p.UserID, "").Select("id, resume_key, LOWER(kind) AS kind, title, latest_at AS created_at, COALESCE(played_at,latest_at) AS played_at, played, favorite, position_ms, rating, release_date, year")
		if len(p.PersonIDs) > 0 {
			local = local.Where("FALSE")
		}
		combined = e.repo.DB.Raw("? UNION ALL ?", combined, local)
	}
	q := filterGlobalItems(e.repo.DB.WithContext(ctx).Table("(?) AS combined", combined), p)
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, true, err
	}
	var ids []string
	if err := q.Session(&gorm.Session{}).Order(globalItemsOrder(p)).Offset(p.StartIndex).Limit(p.Limit).Pluck("id", &ids).Error; err != nil {
		return nil, true, err
	}
	items, err := e.globalItemPayloads(ctx, ids, p)
	if err != nil {
		return nil, true, err
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, nil
}

func filterGlobalItems(q *gorm.DB, p ItemsParams) *gorm.DB {
	if strings.TrimSpace(p.SearchTerm) != "" {
		q = q.Where("POSITION(LOWER(?) IN LOWER(title)) > 0", strings.TrimSpace(p.SearchTerm))
	}
	kinds := lowerStrings(p.IncludeItemTypes)
	if len(kinds) == 0 {
		kinds = []string{model.MetadataKindMovie, model.MetadataKindEpisode}
		if containsEmbyFilter(p.Filters, "IsFavorite") || strings.TrimSpace(p.SearchTerm) != "" {
			kinds = []string{model.MetadataKindMovie, model.MetadataKindSeries}
		}
	}
	q = q.Where("kind IN ?", kinds)
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		q = q.Where("favorite AND kind IN ('movie','series')")
	}
	if containsEmbyFilter(p.Filters, "IsPlayed") {
		q = q.Where("played")
	}
	if containsEmbyFilter(p.Filters, "IsUnplayed") {
		q = q.Where("NOT played")
	}
	return q
}

func globalItemsOrder(p ItemsParams) string {
	order, direction := "release_date", "DESC"
	switch primarySupportedEmbySort(p.SortBy, containsEmbyFilter(p.Filters, "IsResumable")) {
	case "sortname", "name":
		order, direction = "title", "ASC"
	case "datecreated":
		order, direction = "created_at", "ASC"
	case "dateplayed":
		order, direction = "played_at", "ASC"
	case "communityrating":
		order, direction = "rating", "ASC"
	}
	if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
		direction = "DESC"
	} else if strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending") {
		direction = "ASC"
	}
	clauses := []string{order + " " + direction}
	if order == "release_date" {
		clauses = append(clauses, "year "+direction, "created_at "+direction)
	}
	return strings.Join(append(clauses, "id "+direction), ", ")
}

// globalItemPayloads 只补全最终页，所有来源沿用原有批量响应与 Fields 规则。
func (e *EmbyService) globalItemPayloads(ctx context.Context, ids []string, p ItemsParams) ([]map[string]any, error) {
	sourceIDs := []string{}
	localIDs := []string{}
	legacyIDs := []string{}
	for _, id := range ids {
		if strings.HasPrefix(id, "hg-") {
			sourceIDs = append(sourceIDs, id)
		} else if strings.HasPrefix(id, "nfo-") {
			localIDs = append(localIDs, id)
		} else {
			legacyIDs = append(legacyIDs, id)
		}
	}
	var nodes []hongGuoNode
	if len(sourceIDs) > 0 {
		if err := e.hongGuoNodes(ctx, p.UserID, "").Where("id IN ?", sourceIDs).Scan(&nodes).Error; err != nil {
			return nil, err
		}
	}
	sourceItems, err := e.hongGuoNodePayloads(ctx, nodes, p.UserID, p.Fields)
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	if len(localIDs) > 0 {
		var localNodes []hongGuoNode
		if err := e.nfoNodes(ctx, p.UserID, "").Where("id IN ?", localIDs).Scan(&localNodes).Error; err != nil {
			return nil, err
		}
		localItems, err := e.nfoNodePayloads(ctx, localNodes, p.UserID, p.Fields)
		if err != nil {
			return nil, err
		}
		for _, item := range localItems {
			byID[item["Id"].(string)] = item
		}
	}
	for _, item := range sourceItems {
		byID[item["Id"].(string)] = item
	}
	legacyItems, err := e.hongGuoBrowseLegacyPayloads(ctx, legacyIDs, p)
	if err != nil {
		return nil, err
	}
	for _, item := range legacyItems {
		byID[item["Id"].(string)] = item
	}
	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		item := byID[id]
		if item != nil {
			items = append(items, item)
		}
	}
	return items, nil
}

// 混合页中的旧资料仍使用原有批量投影与 Fields 规则，不逐项调用完整详情。
func (e *EmbyService) hongGuoBrowseLegacyPayloads(ctx context.Context, ids []string, p ItemsParams) ([]map[string]any, error) {
	items := []map[string]any{}
	if len(ids) == 0 {
		return items, nil
	}
	var metadata []model.MetadataItem
	if err := e.repo.DB.WithContext(ctx).Select("id, kind").Where("id IN ?", ids).Find(&metadata).Error; err != nil {
		return nil, err
	}
	leafIDs, seriesIDs, seasonIDs := []string{}, []string{}, []string{}
	for _, item := range metadata {
		switch item.Kind {
		case model.MetadataKindSeries:
			seriesIDs = append(seriesIDs, item.ID)
		case model.MetadataKindSeason:
			seasonIDs = append(seasonIDs, item.ID)
		default:
			leafIDs = append(leafIDs, item.ID)
		}
	}
	views, err := e.repo.MediaView.FindByMetadataIDs(ctx, leafIDs, e.mediaQueryFilter(ctx, p.UserID))
	if err != nil {
		return nil, err
	}
	items = append(items, e.payloadsForViewsWithFields(ctx, views, p.UserID, p.Fields)...)
	scope := func() *gorm.DB {
		return seriesScopeQuery(e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), p.UserID))
	}
	groups, err := e.seriesSummaries(ctx, scope(), seriesIDs)
	if err != nil {
		return nil, err
	}
	items = append(items, e.seriesPayloadsWithFields(ctx, groups, p.UserID, p.Fields)...)
	if len(seasonIDs) > 0 {
		var rows []struct {
			ID, SeriesID, SeriesName, LibraryID string
			SeasonNum, EpisodeCount             int
		}
		if err := scope().Where("scope_season.id IN ?", seasonIDs).
			Select(`scope_season.id, scope_series.id AS series_id, scope_series.title AS series_name,
				scope_season.season_num, MIN(media.library_id) AS library_id, COUNT(DISTINCT media.metadata_id) AS episode_count`).
			Group("scope_season.id, scope_series.id").Scan(&rows).Error; err != nil {
			return nil, err
		}
		seasons := make([]embySeasonGroup, 0, len(rows))
		for _, row := range rows {
			seasons = append(seasons, embySeasonGroup{ID: row.ID, SeriesID: row.SeriesID, LibraryID: row.LibraryID,
				SeasonNum: row.SeasonNum, EpisodeCount: row.EpisodeCount, Series: embySeriesGroup{ID: row.SeriesID, Name: row.SeriesName}})
		}
		items = append(items, e.seasonPayloadsWithFields(ctx, seasons, p.UserID, p.Fields)...)
	}
	return items, nil
}

// hongGuoPersonFilter 按源作品演职员筛选；官方合集包含成员季，不按人名猜测归属。
func (e *EmbyService) hongGuoPersonFilter(ctx context.Context, q *gorm.DB, userID, libraryID string, personIDs []string) *gorm.DB {
	if len(personIDs) == 0 {
		return q
	}
	ids := e.repo.DB.WithContext(ctx).Table("hongguo_credits c").
		Joins("JOIN hongguo_works w ON w.id = c.work_id").
		Joins(repository.HongGuoAlbumJoin).
		Joins("LEFT JOIN hongguo_episodes ep ON ep.work_id = c.work_id").
		Joins("CROSS JOIN LATERAL (VALUES ('hg-work-' || c.work_id), ('hg-season-' || c.work_id), ('hg-group-' || g.id), ('hg-episode-' || ep.id)) n(id)").
		Where("'hg-person-' || c.person_id IN ?", personIDs).
		Where("w.source_id IN (?)", e.hongGuoNodes(ctx, userID, libraryID).Select("source_id").Where("source_id <> ''")).Select("n.id")
	return q.Where("id IN (?)", ids)
}
