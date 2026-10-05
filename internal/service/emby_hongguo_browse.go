package service

import (
	"context"
	"strings"
	"sync"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// hongGuoGlobalItems 在 SQL 中合并逻辑身份并分页，不拼接两个来源各自的分页结果。
// 独立来源或普通作品随机请求共用此入口，来源库内浏览保持独立。
func (e *EmbyService) hongGuoGlobalItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	return e.globalItemsWithCount(ctx, p, !p.SkipTotalRecordCount)
}

// globalItemsWithCount 让数组型 Latest 复用全局资格与分页，不额外计算作品总数。
func (e *EmbyService) globalItemsWithCount(ctx context.Context, p ItemsParams, count bool, latestWorks ...bool) (map[string]any, bool, error) {
	if p.ParentID != "" || containsOnlyFolderItemTypes(p.IncludeItemTypes) || (strings.TrimSpace(p.SearchTerm) == "" && !p.Recursive && len(p.IncludeItemTypes) == 0 && len(p.Filters) == 0) {
		return nil, false, nil
	}
	var hasSource bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source IN ('hongguo','nfo'))").Scan(&hasSource).Error; err != nil {
		return nil, true, err
	}
	workLatest := len(latestWorks) > 0 && latestWorks[0]
	if !hasSource && !workLatest && !(embyRandomSort(p) && containsOnlyFavoriteItemTypes(globalItemKinds(p))) {
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
	hasNFO, err := e.repo.NFO.HasMedia(ctx)
	if err != nil {
		return nil, true, err
	}
	ids, total, err := e.filteredWorkBatchPage(ctx, e.globalBatchCandidates(ctx, p, hasNFO, workLatest),
		e.globalBatchEligibility(ctx, p), p.StartIndex, p.Limit, count)
	if err != nil {
		return nil, true, err
	}
	items, err := e.globalItemPayloads(ctx, ids, p)
	if err != nil {
		return nil, true, err
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, nil
}

// hongGuoGlobalCandidates 仅投影筛选、排序所需字段；合集资料不按文件重复计算。
func (e *EmbyService) hongGuoGlobalCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	kinds := globalItemKinds(p)
	if len(kinds) == 2 && containsEmbyFilter(kinds, "movie") && containsEmbyFilter(kinds, "episode") &&
		strings.HasPrefix(globalItemsOrder(p), "latest_at ") && !containsEmbyFilter(p.Filters, "IsPlayed") && !containsEmbyFilter(p.Filters, "IsUnplayed") {
		return e.hongGuoLatestCandidates(ctx, p)
	}
	// 作品列表先压缩每个源作品的文件；季集请求才保留分集身份。
	group, episode := "b.work_id,b.episode_id", "b.episode_id"
	if containsOnlyFavoriteItemTypes(p.IncludeItemTypes) || len(p.IncludeItemTypes) == 0 && (strings.TrimSpace(p.SearchTerm) != "" || containsEmbyFilter(p.Filters, "IsFavorite")) {
		group, episode = "b.work_id", "NULL::varchar"
	}
	files := e.hongGuoVisibleFiles(ctx, p.UserID, "").
		Joins("JOIN hongguo_media_bindings b ON b.media_id=m.id").
		Joins("JOIN hongguo_works w ON w.id=b.work_id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id")
	state := "NULL::timestamp AS played_at, FALSE AS played, 0::bigint AS position_ms"
	if strings.HasPrefix(globalItemsOrder(p), "played_at ") || containsEmbyFilter(p.Filters, "IsPlayed") || containsEmbyFilter(p.Filters, "IsUnplayed") {
		files = files.Joins("LEFT JOIN (?) s ON s.source_id=w.source_id AND s.episode_number=COALESCE(ep.number,1)", repository.PlaybackStates(ctx, db, "hongguo", p.UserID, e.mediaQueryFilter(ctx, p.UserID)))
		state = "MAX(s.watched_at) AS played_at, BOOL_AND(COALESCE(s.completed,FALSE)) AS played, MAX(COALESCE(s.position_ms,0)) AS position_ms"
	}
	files = files.Select("b.work_id, " + episode + " AS episode_id, MAX(m.created_at) AS created_at, " + state).Group(group)
	return db.Table("(?) AS candidates", db.Raw(`WITH albums AS MATERIALIZED (
 SELECT related_album_id AS id, (ARRAY_AGG(title ORDER BY season_index,source_id))[1] AS title,
 MAX(latest_media_added_at) AS latest_at FROM hongguo_works
 WHERE kind='series' AND related_album_id<>'' AND season_index>0 GROUP BY related_album_id
), file_stats AS MATERIALIZED (?)
SELECT n.id, CASE WHEN g.id IS NULL THEN 'hongguo:work:'||w.source_id
 ELSE 'hongguo:group:'||g.id END AS resume_key, n.kind, n.title, MAX(m.created_at) AS created_at,
 MAX(CASE WHEN n.kind='episode' THEN m.created_at WHEN n.kind='season' OR g.id IS NULL
 THEN w.latest_media_added_at ELSE g.latest_at END) AS latest_at,
 COALESCE(MAX(m.played_at),MAX(m.created_at)) AS played_at,
 BOOL_AND(m.played) AS played, BOOL_OR(COALESCE(f.favorite,FALSE)) AS favorite,
 MAX(m.position_ms) AS position_ms,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN 0 ELSE MAX(w.rating) END AS rating,
 '' AS release_date, 0 AS year, ARRAY_AGG(DISTINCT w.id::text) AS work_ids
FROM file_stats m JOIN hongguo_works w ON w.id=m.work_id
LEFT JOIN hongguo_episodes ep ON ep.id=m.episode_id AND ep.work_id=w.id
LEFT JOIN albums g ON w.kind='series' AND w.season_index>0 AND g.id=w.related_album_id
LEFT JOIN hongguo_favorites f ON f.user_id=? AND f.item_id=`+repository.HongGuoFavoriteIdentitySQL+`
CROSS JOIN LATERAL (VALUES
 (CASE WHEN g.id IS NULL THEN 'hg-work-'||w.id ELSE 'hg-group-'||g.id END,
 CASE WHEN w.kind='movie' THEN 'movie' ELSE 'series' END, COALESCE(g.title,w.title)),
 (CASE WHEN w.kind='series' THEN 'hg-season-'||w.id END, 'season', w.title),
 (CASE WHEN w.kind='series' AND ep.id IS NOT NULL THEN 'hg-episode-'||ep.id END, 'episode', '第'||ep.number||'集')
) n(id,kind,title)
WHERE n.id IS NOT NULL GROUP BY n.id,resume_key,n.kind,n.title`,
		files, p.UserID))
}

// hongGuoLatestCandidates 直接生成电影/分集身份，避免为 Latest 再展开并聚合三层节点。
// 状态仍由当前候选批的文件资格查询检查；多版本时间保持可见文件的 MAX。
func (e *EmbyService) hongGuoLatestCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	episode := "CASE WHEN w.kind='movie' THEN NULL ELSE b.episode_id END"
	files := e.hongGuoVisibleFiles(ctx, p.UserID, "").
		Joins("JOIN hongguo_media_bindings b ON b.media_id=m.id").
		Joins("JOIN hongguo_works w ON w.id=b.work_id").
		Select("DISTINCT ON (b.work_id," + episode + ") b.work_id," + episode + " AS episode_id,m.created_at").
		Order("b.work_id," + episode + ",m.created_at DESC NULLS LAST")
	projection := `CASE WHEN w.kind='movie' THEN 'hg-work-'||w.id ELSE 'hg-episode-'||ep.id END AS id,
CASE WHEN w.kind='series' AND w.related_album_id<>'' AND w.season_index>0 THEN 'hongguo:group:'||w.related_album_id
 ELSE 'hongguo:work:'||w.source_id END AS resume_key,
CASE WHEN w.kind='movie' THEN w.title ELSE '第'||ep.number||'集' END AS title,
m.created_at,CASE WHEN w.kind='movie' THEN w.latest_media_added_at ELSE m.created_at END AS latest_at,
m.created_at AS played_at,FALSE AS played,COALESCE(f.favorite,FALSE) AS favorite,
0::bigint AS position_ms,w.rating,'' AS release_date,0 AS year,ARRAY[w.id::text] AS work_ids`
	base := db.Table("file_stats m").Joins("JOIN hongguo_works w ON w.id=m.work_id").
		Joins("LEFT JOIN hongguo_favorites f ON f.user_id=? AND f.item_id="+repository.HongGuoFavoriteIdentitySQL, p.UserID)
	// 分集 ID 已决定其作品；归属单独用 CASE 校验，避免两个关联等式被当作独立选择率。
	// kind 投影常量，避免外层类型筛选低估百万分集；电影分支不读取分集。
	episodes := base.Session(&gorm.Session{}).Joins("JOIN hongguo_episodes ep ON ep.id=m.episode_id").
		Where("CASE WHEN w.kind='series' AND ep.work_id=w.id THEN TRUE ELSE FALSE END").
		Select(projection + ",'episode' AS kind")
	movies := base.Session(&gorm.Session{}).Joins("LEFT JOIN hongguo_episodes ep ON FALSE").Where("w.kind='movie'").Select(projection + ",'movie' AS kind")
	return db.Table("(?) candidates", db.Raw("WITH file_stats AS MATERIALIZED (?) ? UNION ALL ?", files, episodes, movies))
}

func globalItemKinds(p ItemsParams) []string {
	kinds := lowerStrings(p.IncludeItemTypes)
	if len(kinds) == 0 {
		kinds = []string{model.MetadataKindMovie, model.MetadataKindEpisode}
		if containsEmbyFilter(p.Filters, "IsFavorite") || strings.TrimSpace(p.SearchTerm) != "" {
			kinds = []string{model.MetadataKindMovie, model.MetadataKindSeries}
		}
	}
	return kinds
}

func filterGlobalItems(q *gorm.DB, p ItemsParams) *gorm.DB {
	if strings.TrimSpace(p.SearchTerm) != "" {
		q = q.Where("POSITION(LOWER(?) IN LOWER(title)) > 0", strings.TrimSpace(p.SearchTerm))
	}
	q = q.Where("kind IN ?", globalItemKinds(p))
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
	case "random":
		return embyRandomOrder(p, "id") + ", id"
	case "sortname", "name":
		order, direction = "title", "ASC"
	case "datecreated":
		order, direction = "created_at", "ASC"
	case "datelastcontentadded":
		order, direction = "latest_at", "ASC"
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
	if order == "latest_at" {
		clauses[0] += " NULLS LAST"
	}
	if order == "release_date" {
		clauses = append(clauses, "year "+direction, "created_at "+direction)
	}
	return strings.Join(append(clauses, "id "+direction), ", ")
}

// globalItemPayloads 只补全最终页，所有来源沿用原有批量响应与 Fields 规则。
func (e *EmbyService) globalItemPayloads(ctx context.Context, ids []string, p ItemsParams) ([]map[string]any, error) {
	return e.globalItemPayloadsWithPreferredMedia(ctx, ids, p, nil)
}

// globalItemPayloadsWithPreferredMedia 允许续播沿用已选文件，其余列表保持默认版本顺序。
func (e *EmbyService) globalItemPayloadsWithPreferredMedia(ctx context.Context, ids []string, p ItemsParams, preferredMedia map[string]string) ([]map[string]any, error) {
	return e.loadGlobalItemPayloads(ctx, ids, p, preferredMedia, e.hongGuoBrowseLegacyPayloads)
}

// loadGlobalItemPayloads 并发读取各来源，普通 Latest 保留自己的直接绑定文件投影。
func (e *EmbyService) loadGlobalItemPayloads(ctx context.Context, ids []string, p ItemsParams, preferredMedia map[string]string, legacyPayloads func(context.Context, []string, ItemsParams) ([]map[string]any, error)) ([]map[string]any, error) {
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
	loadCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var pending sync.WaitGroup
	var sourceItems, localItems, legacyItems []map[string]any
	if len(sourceIDs) > 0 {
		pending.Go(func() {
			nodes, err := e.hongGuoPageNodes(loadCtx, p.UserID, sourceIDs)
			if err != nil {
				cancel(err)
				return
			}
			for i := range nodes {
				if mediaID := preferredMedia[nodes[i].ID]; mediaID != "" {
					nodes[i].MediaID = mediaID
				}
			}
			sourceItems, err = e.hongGuoNodePayloads(loadCtx, nodes, p.UserID, p.Fields)
			if err != nil {
				cancel(err)
			}
		})
	}
	if len(localIDs) > 0 {
		pending.Go(func() {
			var nodes []hongGuoNode
			if err := e.nfoItemNodes(loadCtx, p.UserID, localIDs...).Where("id IN ?", localIDs).Scan(&nodes).Error; err != nil {
				cancel(err)
				return
			}
			for i := range nodes {
				if mediaID := preferredMedia[nodes[i].ID]; mediaID != "" {
					nodes[i].MediaID = mediaID
				}
			}
			var err error
			localItems, err = e.nfoNodePayloads(loadCtx, nodes, p.UserID, p.Fields)
			if err != nil {
				cancel(err)
			}
		})
	}
	if len(legacyIDs) > 0 {
		pending.Go(func() {
			var err error
			legacyItems, err = legacyPayloads(loadCtx, legacyIDs, p)
			if err != nil {
				cancel(err)
			}
		})
	}
	pending.Wait()
	if err := context.Cause(loadCtx); err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, source := range [][]map[string]any{sourceItems, localItems, legacyItems} {
		for _, item := range source {
			byID[item["Id"].(string)] = item
		}
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

// hongGuoPageNodes 让收藏与全局 Latest 的作品卡片共用库内投影，季集保留原身份范围。
func (e *EmbyService) hongGuoPageNodes(ctx context.Context, userID string, ids []string) ([]hongGuoNode, error) {
	var workIDs, hierarchyIDs []string
	for _, id := range ids {
		if strings.HasPrefix(id, "hg-group-") || strings.HasPrefix(id, "hg-work-") {
			workIDs = append(workIDs, id)
		} else {
			hierarchyIDs = append(hierarchyIDs, id)
		}
	}
	var nodes []hongGuoNode
	if len(workIDs) > 0 {
		var members []struct{ ID, Kind string }
		q := repository.FilterHongGuoWorkIDs(e.repo.DB.WithContext(ctx).Table("hongguo_works w"), workIDs)
		if len(ids) != 1 || !strings.HasPrefix(ids[0], "hg-work-") {
			q = e.workLibraryScope(ctx, q, "w.library_ids", ItemsParams{UserID: userID})
		}
		if err := q.Select("w.id,w.kind").Scan(&members).Error; err != nil {
			return nil, err
		}
		if len(members) > 0 {
			memberIDs := make([]string, 0, len(members))
			seriesOnly := true
			for _, member := range members {
				memberIDs = append(memberIDs, member.ID)
				seriesOnly = seriesOnly && member.Kind == "series"
			}
			var err error
			nodes, err = e.hongGuoLibraryNodes(ctx, ItemsParams{UserID: userID}, memberIDs, seriesOnly)
			if err != nil {
				return nil, err
			}
			requested := make(map[string]bool, len(workIDs))
			for _, id := range workIDs {
				requested[id] = true
			}
			selected := nodes[:0]
			for _, node := range nodes {
				if requested[node.ID] {
					selected = append(selected, node)
				}
			}
			nodes = selected
		}
	}
	if len(hierarchyIDs) > 0 {
		var hierarchy []hongGuoNode
		if err := e.hongGuoItemNodes(ctx, userID, hierarchyIDs...).Where("id IN ?", hierarchyIDs).Scan(&hierarchy).Error; err != nil {
			return nil, err
		}
		nodes = append(nodes, hierarchy...)
	}
	return nodes, nil
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
		Where("EXISTS (? OFFSET 0)", e.hongGuoVisibleFiles(ctx, userID, libraryID).
			Joins("JOIN hongguo_media_bindings b ON b.media_id=m.id").Select("1").Where("b.work_id=w.id")).Select("n.id")
	return q.Where("id IN (?)", ids)
}
