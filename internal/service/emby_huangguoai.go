package service

import (
	"context"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
	"strings"
)

// huangGuoAIFiles 共用文件权限和独立绑定，电影只接受真实第一集。
func (e *EmbyService) huangGuoAIFiles(ctx context.Context, userID, libraryID string, ids ...string) *gorm.DB {
	q := e.repo.DB.WithContext(ctx).Table("media m").
		Joins("JOIN huangguoai_media_bindings b ON b.media_id=m.id").
		Joins("JOIN huangguoai_works w ON w.id=b.work_id AND w.projection_error=''").
		Joins("JOIN huangguoai_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id").
		Where("m.catalog_source='huangguoai' AND (w.kind='series' OR ep.number=1)")
	v := e.mediaVisibility(ctx, userID)
	if !v.IncludeNSFW || (v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0) {
		q = q.Where("FALSE")
	}
	if len(v.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id=ANY(?)", &v.AllowedLibraryIDs)
	}
	if len(v.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id<>ALL(?)", &v.HiddenLibraryIDs)
	}
	if libraryID != "" {
		q = q.Where("m.library_id=ANY(?)", &[]string{libraryID})
	}
	if len(ids) > 0 {
		q = q.Where(`(w.kind='movie' AND 'hga-work-'||w.id=ANY(?)) OR
 (w.kind='series' AND ('hga-group-'||w.source_id=ANY(?) OR 'hga-season-'||w.id=ANY(?) OR 'hga-episode-'||ep.id=ANY(?)))`, &ids, &ids, &ids, &ids)
	}
	return q
}

// huangGuoAINodes 在限定作品文件后产生默认季、真实分集与作品节点。
func (e *EmbyService) huangGuoAINodes(ctx context.Context, userID, libraryID string, ids ...string) *gorm.DB {
	files := e.huangGuoAIFiles(ctx, userID, libraryID, ids...).Select("m.*,w.id AS work_id,w.source_id,w.kind,w.title AS work_title,w.overview,w.tags,w.rating,w.latest_media_added_at,ep.id AS episode_id,ep.number")

	if len(ids) > 0 {
		var workIDs, sourceIDs, episodeIDs []string
		for _, id := range ids {
			switch {
			case strings.HasPrefix(id, "hga-group-"):
				sourceIDs = append(sourceIDs, strings.TrimPrefix(id, "hga-group-"))
			case strings.HasPrefix(id, "hga-episode-"):
				episodeIDs = append(episodeIDs, strings.TrimPrefix(id, "hga-episode-"))
			case strings.HasPrefix(id, "hga-work-"):
				workIDs = append(workIDs, strings.TrimPrefix(id, "hga-work-"))
			case strings.HasPrefix(id, "hga-season-"):
				workIDs = append(workIDs, strings.TrimPrefix(id, "hga-season-"))
			}
		}
		episodes := e.repo.DB.WithContext(ctx).Table("huangguoai_episodes").Select("work_id").Where("id=ANY(?)", &episodeIDs)
		// 逐页作品内定位文件，避免优化器把页内容查询转成整库文件哈希扫描。
		files = e.repo.DB.WithContext(ctx).Table("huangguoai_works scoped").
			Where("scoped.id=ANY(?) OR scoped.source_id=ANY(?) OR scoped.id IN (?)", &workIDs, &sourceIDs, episodes).
			Joins("JOIN LATERAL (? OFFSET 0) v ON TRUE", files.Where("w.id=scoped.id")).Select("v.*")
	}
	states := repository.PlaybackStates(ctx, e.repo.DB, "huangguoai", userID, e.mediaQueryFilter(ctx, userID))
	return e.repo.DB.WithContext(ctx).Table("(?) nodes", e.repo.DB.Raw(`SELECT n.id,n.kind,n.title,n.parent_id,n.season_number,n.episode_number,
 MIN(v.id) AS media_id,MIN(v.created_at) AS created_at,MAX(v.latest_media_added_at) AS latest_at,
 MAX(s.watched_at) AS played_at,BOOL_OR(COALESCE(f.favorite,FALSE)) AS favorite,MAX(f.updated_at) AS favorite_at,
 BOOL_AND(COALESCE(s.completed,FALSE)) AS played,MAX(COALESCE(s.position_ms,0)) AS position_ms,
 MIN(v.source_id) AS source_id,MIN(v.overview) AS overview,MIN(v.tags::text) AS tags,MAX(v.rating) AS rating,
 COUNT(DISTINCT v.episode_id) AS episode_count,
 COUNT(DISTINCT v.episode_id) FILTER (WHERE NOT COALESCE(s.completed,FALSE)) AS unplayed_item_count,
 COALESCE(MIN(a.id),'') AS artwork_id
 FROM (?) v LEFT JOIN (?) s ON s.source_id=v.source_id AND s.episode_number=v.number
 LEFT JOIN huangguoai_favorites f ON f.source_id=v.source_id AND f.user_id=?
 LEFT JOIN huangguoai_artworks a ON a.work_id=v.work_id AND a.local_key<>''
 CROSS JOIN LATERAL (VALUES
 (CASE WHEN v.kind='movie' THEN 'hga-work-'||v.work_id ELSE 'hga-group-'||v.source_id END,
 CASE WHEN v.kind='movie' THEN 'Movie' ELSE 'Series' END,v.work_title,''::text,0,0),
 (CASE WHEN v.kind='series' THEN 'hga-season-'||v.work_id END,'Season',v.work_title,'hga-group-'||v.source_id,1,0),
 (CASE WHEN v.kind='series' THEN 'hga-episode-'||v.episode_id END,'Episode','第'||v.number||'集','hga-season-'||v.work_id,1,v.number)
 ) n(id,kind,title,parent_id,season_number,episode_number)
 WHERE n.id IS NOT NULL GROUP BY n.id,n.kind,n.title,n.parent_id,n.season_number,n.episode_number`, files, states, userID))
}
func (e *EmbyService) huangGuoAIPayloads(ctx context.Context, nodes []hongGuoNode, userID string, fields []string) ([]map[string]any, error) {
	ids := []string{}
	for _, n := range nodes {
		if n.Kind == "Movie" || n.Kind == "Episode" {
			ids = append(ids, n.ID)
		}
	}
	views, err := e.repo.MediaView.HuangGuoAIItemsViews(ctx, ids, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	relations := &embyItemRelations{fields: newEmbyListFields(fields), versionsByMetadataID: map[string][]model.MediaView{}}
	for _, v := range views {
		relations.versionsByMetadataID[v.CatalogItemID] = append(relations.versionsByMetadataID[v.CatalogItemID], v)
	}
	out := []map[string]any{}
	for _, n := range nodes {
		if n.Kind == "Movie" || n.Kind == "Episode" {
			versions := orderMediaVersionSiblings(relations.versionsByMetadataID[n.ID], n.MediaID)
			if len(versions) == 0 {
				continue
			}
			relations.versionsByMetadataID[n.ID] = versions
			out = append(out, e.itemPayloadWithRelations(ctx, &versions[0], userID, n.Kind == "Movie" && n.Favorite, n.PositionMs, n.Played, false, relations))
			continue
		}
		item, err := e.hongGuoNodePayload(ctx, n, userID)
		if err != nil {
			return nil, err
		}
		if n.Kind == "Season" {
			item["SeriesName"] = n.Title
			item["Name"] = seasonName(n.SeasonNumber)
		}
		item["ProviderIds"] = map[string]string{"HuangGuoAI": n.SourceID}
		if !relations.fields.providerIDs {
			delete(item, "ProviderIds")
		}
		out = append(out, item)
	}
	return out, nil
}
func (e *EmbyService) huangGuoAIHierarchyItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	libraryID := ""
	if !strings.HasPrefix(p.ParentID, "hga-") {
		if p.ParentID == "" {
			return nil, false, nil
		}
		lib, err := FindLibraryBasic(ctx, e.repo, e.cache, p.ParentID)
		if err != nil {
			return nil, true, err
		}
		if lib == nil || lib.Type != model.LibraryTypeHuangGuoAI {
			return nil, false, nil
		}
		libraryID = lib.ID
	}
	if libraryID != "" && !containsEmbyFilter(p.Filters, "IsResumable") &&
		(containsOnlyFavoriteItemTypes(p.IncludeItemTypes) || len(p.IncludeItemTypes) == 0 && !p.Recursive || containsEmbyFilter(p.Filters, "IsFavorite")) {
		p = workDateSortParams(p)
	}
	if libraryID != "" && hongGuoLibraryPageSupported(p) {
		result, err := e.huangGuoAILibraryItems(ctx, p)
		return result, true, err
	}
	q := e.huangGuoAINodes(ctx, p.UserID, libraryID)
	if libraryID == "" {
		q = e.huangGuoAINodes(ctx, p.UserID, "", p.ParentID)
		if p.Recursive || containsItemType(p.IncludeItemTypes, "Episode") {
			q = q.Where("kind='Episode'")
		} else {
			q = q.Where("parent_id=?", p.ParentID)
		}
	} else if !p.Recursive {
		q = q.Where("parent_id=''")
	}
	if len(p.IncludeItemTypes) > 0 && !containsOnlyFolderItemTypes(p.IncludeItemTypes) {
		q = q.Where("LOWER(kind) IN ?", lowerStrings(p.IncludeItemTypes))
	}
	if p.SearchTerm != "" {
		q = q.Where("POSITION(LOWER(?) IN LOWER(title))>0", strings.TrimSpace(p.SearchTerm))
	}
	if len(p.PersonIDs) > 0 {
		q = q.Where("FALSE")
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		q = q.Where("favorite AND kind IN ('Movie','Series')")
	}
	if containsEmbyFilter(p.Filters, "IsPlayed") {
		q = q.Where("played")
	}
	if containsEmbyFilter(p.Filters, "IsUnplayed") {
		q = q.Where("NOT played")
	}
	if containsEmbyFilter(p.Filters, "IsResumable") {
		q = q.Where("position_ms>0 AND NOT played AND kind IN ('Movie','Episode')")
	}
	var total int64
	if !p.SkipTotalRecordCount {
		if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, true, err
		}
	}
	order := "title"
	switch primarySupportedEmbySort(p.SortBy, false) {
	case "favoriteadded":
		order = "favorite_at"
	case "datecreated":
		order = "created_at"
	case "datelastcontentadded":
		order = "latest_at"
	case "dateplayed":
		order = "played_at"
	case "communityrating":
		order = "rating"
	case "random":
		order = embyRandomOrder(p, "id")
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	var nodes []hongGuoNode
	if primarySupportedEmbySort(p.SortBy, false) != "favoriteadded" {
		q = q.Order("season_number,episode_number")
	} else {
		order += " NULLS LAST, id DESC"
	}
	if err := q.Order(order).Offset(p.StartIndex).Limit(p.Limit).Scan(&nodes).Error; err != nil {
		return nil, true, err
	}
	items, err := e.huangGuoAIPayloads(ctx, nodes, p.UserID, p.Fields)
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, err
}

// huangGuoAIGlobalCandidates 作品查询不展开整站分集；季集查询沿用文件层级。
func (e *EmbyService) huangGuoAIGlobalCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	if !containsOnlyFavoriteItemTypes(globalItemKinds(p)) {
		return e.huangGuoAINodes(ctx, p.UserID, "").Select(`id,LOWER(kind) AS kind,title,created_at,latest_at,played_at,favorite,rating,'' AS release_date,0 AS year,NULL::text AS origin_id,NULL::text[] AS work_ids,'huangguoai' AS source`)
	}
	q := e.workLibraryScope(ctx, db.Table("huangguoai_works w"), "w.library_ids", p).Where("w.projection_error=''")
	if !e.mediaVisibility(ctx, p.UserID).IncludeNSFW || len(p.PersonIDs) > 0 {
		q = q.Where("FALSE")
	}
	created, playedAt := "w.created_at", "NULL::timestamp"
	if globalWorkDateAggregate(p) != "" {
		files := e.huangGuoAIFiles(ctx, p.UserID, "").Where("b.work_id=w.id").Select("MIN(m.created_at) AS created_at")
		q = q.Joins("LEFT JOIN LATERAL (?) dates ON TRUE", files)
		created = "dates.created_at"
	}
	if strings.HasPrefix(globalItemsOrder(p), "played_at ") {
		q = q.Joins("LEFT JOIN LATERAL (SELECT MAX(watched_at) AS played_at FROM huangguoai_user_states WHERE source_id=w.source_id AND user_id=?) dates_played ON TRUE", p.UserID)
		playedAt = "dates_played.played_at"
	}
	return q.Select(`CASE WHEN w.kind='series' THEN 'hga-group-'||w.source_id ELSE 'hga-work-'||w.id END AS id,w.kind,w.title,`+created+` AS created_at,w.latest_media_added_at AS latest_at,`+playedAt+` AS played_at,
 EXISTS (SELECT 1 FROM huangguoai_favorites f WHERE f.source_id=w.source_id AND f.user_id=? AND f.favorite) AS favorite,
 w.rating,'' AS release_date,0 AS year,NULL::text AS origin_id,ARRAY[w.id::text] AS work_ids,'huangguoai' AS source`, p.UserID)
}

// huangGuoAILibraryItems 先按作品分页，再加载当前页文件与状态。
func (e *EmbyService) huangGuoAILibraryItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	p = workDateSortParams(p)
	base := p
	base.Filters = nil
	if len(base.IncludeItemTypes) == 0 {
		base.IncludeItemTypes = []string{"Movie", "Series"}
	}
	db := e.repo.DB.WithContext(ctx)
	q := filterGlobalItems(db.Table("(?) works", e.huangGuoAIGlobalCandidates(ctx, base)), base)
	order := globalItemsOrder(p)
	candidates := q.Select("id,kind,work_ids,ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal").Order(order)
	files := e.huangGuoAIFiles(ctx, p.UserID, p.ParentID).Where("b.work_id=ANY(item.work_ids)")
	eligible := db.Table("work_batch item").Select("ordinal").Where("EXISTS (? OFFSET 0)", e.workBatchFileEligibility(ctx, p, files, "huangguoai", "source_id=w.source_id AND episode_number=ep.number"))
	ids, total, err := e.filteredWorkBatchPage(ctx, candidates, eligible, p.StartIndex, p.Limit, !p.SkipTotalRecordCount)
	if err != nil {
		return nil, err
	}
	var nodes []hongGuoNode
	if len(ids) > 0 {
		err = e.huangGuoAINodes(ctx, p.UserID, p.ParentID, ids...).Where("id=ANY(?)", &ids).Scan(&nodes).Error
		if err != nil {
			return nil, err
		}
	}
	byID := map[string]hongGuoNode{}
	for _, node := range nodes {
		byID[node.ID] = node
	}
	nodes = nil
	for _, id := range ids {
		if node, ok := byID[id]; ok {
			nodes = append(nodes, node)
		}
	}
	items, err := e.huangGuoAIPayloads(ctx, nodes, p.UserID, p.Fields)
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, err
}
