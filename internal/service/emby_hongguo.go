package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// ResumeItems 为直接调用者提供首页续播；HTTP 分页入口复用相同候选查询。
func (e *EmbyService) ResumeItems(ctx context.Context, userID string, limit int) (map[string]any, error) {
	return e.continuationItems(ctx, ItemsParams{UserID: userID, Limit: limit}, repository.ContinuationResume)
}

// hongGuoNode 是文件可见性过滤后的逻辑目录，不写入旧资料表。
type hongGuoNode struct {
	ID                string
	Kind              string
	Title             string
	ParentID          string
	MediaID           string
	SeasonNumber      int
	EpisodeNumber     int
	CreatedAt         time.Time
	LatestAt          time.Time
	PlayedAt          time.Time
	Favorite          bool
	Played            bool
	ArtworkID         string
	PositionMs        int64
	SourceID          string
	Overview          string
	Tags              string
	Rating            float32
	EpisodeCount      int
	UnplayedItemCount int
}

func (e *EmbyService) hongGuoNodes(ctx context.Context, userID, libraryID string) *gorm.DB {
	return e.hongGuoFileNodes(ctx, userID, e.hongGuoVisibleFiles(ctx, userID, libraryID))
}

// hongGuoItemNodes 在层级聚合前限定目标身份的文件，供详情和季集浏览共用。
// 外层仍按节点身份筛选；不能把已入合集的旧 work 身份重新暴露为独立作品。
func (e *EmbyService) hongGuoItemNodes(ctx context.Context, userID string, ids ...string) *gorm.DB {
	return e.hongGuoFileNodes(ctx, userID, e.hongGuoItemFiles(ctx, userID, ids...))
}

// hongGuoItemFiles 共用原生身份和文件权限范围，写状态无需展开展示节点。
func (e *EmbyService) hongGuoItemFiles(ctx context.Context, userID string, ids ...string) *gorm.DB {
	bindings := e.repo.DB.WithContext(ctx).Table("hongguo_media_bindings scoped_binding").Select("scoped_binding.media_id")
	var groups, works, episodes []string
	for _, id := range ids {
		switch {
		case strings.HasPrefix(id, "hg-group-"):
			groups = append(groups, strings.TrimPrefix(id, "hg-group-"))
		case strings.HasPrefix(id, "hg-work-"):
			works = append(works, strings.TrimPrefix(id, "hg-work-"))
		case strings.HasPrefix(id, "hg-season-"):
			works = append(works, strings.TrimPrefix(id, "hg-season-"))
		case strings.HasPrefix(id, "hg-episode-"):
			episodes = append(episodes, strings.TrimPrefix(id, "hg-episode-"))
		}
	}
	if len(ids) == 1 && len(works) == 1 {
		bindings = bindings.Where("scoped_binding.work_id = ?", works[0])
	} else if len(ids) == 1 && len(episodes) == 1 {
		bindings = bindings.Where("scoped_binding.episode_id = ?", episodes[0])
	} else {
		workIDs := e.repo.DB.Table("hongguo_works").Select("id").Where(`id = ANY(?) OR
 (related_album_id = ANY(?) AND related_album_id <> '' AND kind = 'series' AND season_index > 0)`, &works, &groups)
		workIDs = e.workLibraryScope(ctx, workIDs, "hongguo_works.library_ids", ItemsParams{UserID: userID})
		bindings = bindings.Where("scoped_binding.work_id IN (?)", workIDs)
		if len(episodes) > 0 {
			// 分开走作品/分集绑定索引；UNION 去重父子重叠文件，避免 OR 全扫。
			leaves := e.repo.DB.Table("hongguo_media_bindings").Select("media_id").Where("episode_id = ANY(?)", &episodes)
			bindings = e.repo.DB.Raw("? UNION ?", bindings, leaves)
		}
	}
	files := e.hongGuoVisibleFiles(ctx, userID, "").Where("m.id IN (?)", bindings)
	return e.repo.DB.Raw("? OFFSET 0", files)
}

func (e *EmbyService) hongGuoVisibleFiles(ctx context.Context, userID, libraryID string) *gorm.DB {
	v := e.mediaVisibility(ctx, userID)
	files := e.repo.DB.WithContext(ctx).Table("media AS m").Select("m.*").Where("m.catalog_source = ?", model.TaskSystemHongGuo)
	if libraryID != "" {
		files = files.Where("m.library_id = ?", libraryID)
	}
	if v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0 {
		files = files.Where("FALSE")
	}
	if len(v.AllowedLibraryIDs) > 0 {
		files = files.Where("m.library_id = ANY(?)", &v.AllowedLibraryIDs)
	}
	if len(v.HiddenLibraryIDs) > 0 {
		files = files.Where("m.library_id <> ALL(?)", &v.HiddenLibraryIDs)
	}
	return files
}

// hongGuoFileNodes 允许作品分页先限定文件范围，再复用原有层级和状态投影。
func (e *EmbyService) hongGuoFileNodes(ctx context.Context, userID string, files *gorm.DB) *gorm.DB {
	// 同一文件贡献整剧、季、集节点；先过滤文件，再按逻辑身份聚合多版本。
	return e.repo.DB.WithContext(ctx).Table(`(?) AS nodes`, e.repo.DB.Raw(`
SELECT n.id, n.resume_key, n.kind, n.title, n.parent_id, n.season_number, n.episode_number,
 MIN(m.id) AS media_id, MIN(m.created_at) AS created_at, MAX(m.created_at) AS file_latest_at,
 MAX(CASE WHEN n.kind = 'Episode' THEN m.created_at WHEN n.kind = 'Season' THEN w.latest_media_added_at
 ELSE `+repository.HongGuoLatestMediaAddedSQL+` END) AS latest_at,
 MAX(s.watched_at) AS played_at,
 BOOL_OR(COALESCE(f.favorite,FALSE)) AS favorite,
 BOOL_AND(COALESCE(s.completed,FALSE)) AS played, MAX(COALESCE(s.position_ms,0)) AS position_ms,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN '' ELSE MIN(w.source_id) END AS source_id,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN '' ELSE MIN(w.overview) END AS overview,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN '[]' ELSE MIN(w.tags) END AS tags,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN 0 ELSE MAX(w.rating) END AS rating,
	 COUNT(DISTINCT ep.id) AS episode_count,
	 COUNT(DISTINCT ep.id) FILTER (WHERE NOT COALESCE(s.completed,FALSE)) AS unplayed_item_count,
 CASE WHEN n.kind = 'Episode' THEN '' ELSE COALESCE((ARRAY_AGG(a.id ORDER BY NULLIF(w.season_index,0) NULLS LAST,w.id) FILTER (WHERE a.id IS NOT NULL))[1],'') END AS artwork_id
FROM (?) AS m
JOIN hongguo_media_bindings b ON b.media_id = m.id
JOIN hongguo_works w ON w.id = b.work_id
LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id
`+repository.HongGuoAlbumJoin+`
LEFT JOIN hongguo_artworks a ON a.work_id = w.id AND a.local_key <> ''
LEFT JOIN (?) s ON s.source_id = w.source_id AND s.episode_number = COALESCE(ep.number,1)
LEFT JOIN hongguo_favorites f ON f.user_id = ? AND f.item_id = `+repository.HongGuoFavoriteIdentitySQL+`
CROSS JOIN LATERAL (VALUES
	(CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END,
	 CASE WHEN w.kind = 'movie' THEN 'Movie' ELSE 'Series' END,
	 COALESCE(g.title,w.title), ''::text, 0, 0, CASE WHEN g.id IS NOT NULL THEN 'hongguo:group:' || g.id ELSE 'hongguo:work:' || w.source_id END),
	(CASE WHEN w.kind = 'series' THEN 'hg-season-' || w.id END, 'Season', w.title,
	 CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END, CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END, 0, CASE WHEN g.id IS NOT NULL THEN 'hongguo:group:' || g.id ELSE 'hongguo:work:' || w.source_id END),
	(CASE WHEN w.kind = 'series' AND ep.id IS NOT NULL THEN 'hg-episode-' || ep.id END, 'Episode', '第' || ep.number || '集', 'hg-season-' || w.id, CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END, ep.number,
	 CASE WHEN g.id IS NOT NULL THEN 'hongguo:group:' || g.id ELSE 'hongguo:work:' || w.source_id END)
) AS n(id,kind,title,parent_id,season_number,episode_number,resume_key)
WHERE n.id IS NOT NULL
GROUP BY n.id,n.resume_key,n.kind,n.title,n.parent_id,n.season_number,n.episode_number`, files, repository.PlaybackStates(ctx, e.repo.DB, "hongguo", userID, e.mediaQueryFilter(ctx, userID)), userID))
}

// LatestItems 使用可见文件的入库时间合并最新项，各来源只读取所需的前 N 个候选。
func (e *EmbyService) LatestItems(ctx context.Context, userID, parentID string, limit int, isPlayed bool, fields ...string) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if parentID == "" {
		filter := "IsUnplayed"
		if isPlayed {
			filter = "IsPlayed"
		}
		p := ItemsParams{UserID: userID, Recursive: true, IncludeItemTypes: []string{"Movie", "Series"}, Limit: limit,
			Fields: fields, Filters: []string{filter}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
		result, _, err := e.globalItemsWithCount(ctx, p, false, true)
		if err != nil {
			return nil, err
		}
		items, _ := result["Items"].([]map[string]any)
		return items, nil
	}
	filterHGA := "IsUnplayed"
	if isPlayed {
		filterHGA = "IsPlayed"
	}
	if result, ok, err := e.huangGuoAIHierarchyItems(ctx, ItemsParams{UserID: userID, ParentID: parentID, Limit: limit, Fields: fields, IncludeItemTypes: []string{"Movie", "Series"}, Filters: []string{filterHGA}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}); ok {
		if err != nil {
			return nil, err
		}
		items, _ := result["Items"].([]map[string]any)
		return items, nil
	}
	if parentID != "" && !strings.HasPrefix(parentID, "hg-") && !strings.HasPrefix(parentID, "nfo-") {
		library, err := FindLibraryBasic(ctx, e.repo, e.cache, parentID)
		if err != nil {
			return nil, err
		}
		if library != nil && library.Type == model.LibraryTypeHongGuo {
			filter := "IsUnplayed"
			if isPlayed {
				filter = "IsPlayed"
			}
			items, _, err := e.hongGuoLibraryItems(ctx, ItemsParams{UserID: userID, ParentID: parentID, Limit: limit, Fields: fields, Filters: []string{filter}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}, false)
			return items, err
		}
		if library != nil && libraryUsesNFOOnly(library) {
			filter := "IsUnplayed"
			if isPlayed {
				filter = "IsPlayed"
			}
			items, _, err := e.nfoLibraryItems(ctx, ItemsParams{UserID: userID, ParentID: parentID, Limit: limit, Fields: fields, Filters: []string{filter}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}, false)
			return items, err
		}
	}
	if has, err := e.repo.NFO.HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		filter := "IsUnplayed"
		if isPlayed {
			filter = "IsPlayed"
		}
		params := ItemsParams{UserID: userID, ParentID: parentID, Limit: limit, Fields: fields, Filters: []string{filter}, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
		if result, handled, err := e.hongGuoHierarchyItems(ctx, params); handled {
			if err != nil {
				return nil, err
			}
			items, _ := result["Items"].([]map[string]any)
			return items, nil
		}
	}
	if parentID != "" {
		library, err := FindLibraryBasic(ctx, e.repo, e.cache, parentID)
		if err != nil {
			return nil, err
		}
		if library == nil || library.Type != model.LibraryTypeHongGuo {
			return e.legacyLatestItems(ctx, userID, parentID, limit, isPlayed, fields...)
		}
	}
	var hasSource bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source = 'hongguo')").Scan(&hasSource).Error; err != nil {
		return nil, err
	}
	if !hasSource {
		return e.legacyLatestItems(ctx, userID, parentID, limit, isPlayed, fields...)
	}
	p := ItemsParams{UserID: userID, ParentID: parentID, Fields: fields}
	if isPlayed {
		p.Filters = []string{"IsPlayed"}
	} else {
		p.Filters = []string{"IsUnplayed"}
	}
	q := e.hongGuoGlobalWorkCandidates(ctx, p).Where("played = ?", isPlayed)
	var nodes []hongGuoNode
	if err := q.Select("id,latest_at").Order("latest_at DESC, id").Limit(limit).Scan(&nodes).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return e.globalItemPayloads(ctx, ids, p)
}

func (e *EmbyService) hongGuoHierarchyItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	libraryID := ""
	local := strings.HasPrefix(p.ParentID, "nfo-")
	if !local && !strings.HasPrefix(p.ParentID, "hg-") {
		if p.ParentID == "" {
			return nil, false, nil
		}
		lib, err := FindLibraryBasic(ctx, e.repo, e.cache, p.ParentID)
		if err != nil {
			return nil, true, err
		}
		if lib == nil || (lib.Type != model.LibraryTypeHongGuo && !libraryUsesNFOOnly(lib)) {
			return nil, false, nil
		}
		libraryID = lib.ID
		local = libraryUsesNFOOnly(lib)
	}
	if libraryID != "" && !local && hongGuoLibraryPageSupported(p) {
		items, total, err := e.hongGuoLibraryItems(ctx, p, !p.SkipTotalRecordCount)
		return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, err
	}
	if libraryID != "" && local && hongGuoLibraryPageSupported(p) {
		items, total, err := e.nfoLibraryItems(ctx, p, !p.SkipTotalRecordCount)
		return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, err
	}
	nodesQuery := e.hongGuoNodes
	if local {
		nodesQuery = e.nfoNodes
	}
	q := nodesQuery(ctx, p.UserID, libraryID)
	if libraryID == "" {
		if local {
			q = e.nfoItemNodes(ctx, p.UserID, p.ParentID)
		} else {
			q = e.hongGuoItemNodes(ctx, p.UserID, p.ParentID)
		}
	}
	if libraryID != "" {
		if !p.Recursive {
			q = q.Where("parent_id = ''")
		}
	} else if p.Recursive || containsItemType(p.IncludeItemTypes, "Episode") {
		if local {
			seasons := q.Session(&gorm.Session{}).Select("id").Where("parent_id = ? AND kind = 'Season'", p.ParentID)
			q = q.Where("(parent_id = ? OR parent_id IN (?)) AND kind = 'Episode'", p.ParentID, seasons)
		} else if strings.HasPrefix(p.ParentID, "hg-season-") {
			q = q.Where("parent_id = ?", p.ParentID)
		} else {
			seasons := q.Session(&gorm.Session{}).Select("id").Where("parent_id = ? AND kind = 'Season'", p.ParentID)
			q = q.Where("parent_id IN (?) AND kind = 'Episode'", seasons)
		}
	} else {
		q = q.Where("parent_id = ?", p.ParentID)
	}
	if len(p.IncludeItemTypes) > 0 && !containsOnlyFolderItemTypes(p.IncludeItemTypes) {
		q = q.Where("LOWER(kind) IN ?", lowerStrings(p.IncludeItemTypes))
	}
	if p.SearchTerm != "" {
		q = q.Where("POSITION(LOWER(?) IN LOWER(title)) > 0", strings.TrimSpace(p.SearchTerm))
	}
	if local && len(p.PersonIDs) > 0 {
		q = q.Where("FALSE")
	} else if !local {
		q = e.hongGuoPersonFilter(ctx, q, p.UserID, libraryID, p.PersonIDs)
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
	resumeFilter := containsEmbyFilter(p.Filters, "IsResumable")
	if resumeFilter {
		q = q.Where("position_ms > 0 AND kind IN ('Movie','Episode')")
		q = e.repo.DB.WithContext(ctx).Table("(?) AS grouped_resume", q.Select("DISTINCT ON (resume_key) *").Order("resume_key, played_at DESC, id DESC"))
	}
	var total int64
	if !p.SkipTotalRecordCount {
		if err := q.Count(&total).Error; err != nil {
			return nil, true, err
		}
	}
	order := "title"
	if embyRandomSort(p) {
		order = embyRandomOrder(p, "id")
	} else if strings.Contains(strings.ToLower(p.SortBy), "datecreated") {
		order = "created_at"
	} else if strings.Contains(strings.ToLower(p.SortBy), "datelastcontentadded") {
		order = "latest_at"
	} else if resumeFilter && strings.Contains(strings.ToLower(p.SortBy), "dateplayed") {
		order = "played_at"
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	var nodes []hongGuoNode
	if !resumeFilter && !embyRandomSort(p) {
		q = q.Order("season_number, episode_number")
	}
	if err := q.Order(order).Order("id").Limit(p.Limit).Offset(p.StartIndex).Scan(&nodes).Error; err != nil {
		return nil, true, err
	}
	payloads := e.hongGuoNodePayloads
	if local {
		payloads = e.nfoNodePayloads
	}
	items, err := payloads(ctx, nodes, p.UserID, p.Fields)
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, err
}

// hongGuoNodePayloads 只加载当前页的文件版本，列表、搜索和最新添加共用此边界。
func (e *EmbyService) hongGuoNodePayloads(ctx context.Context, nodes []hongGuoNode, userID string, fields []string) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(nodes))
	itemIDs := []string{}
	sourceIDs := []string{}
	for _, node := range nodes {
		if node.SourceID != "" {
			sourceIDs = append(sourceIDs, node.SourceID)
		}
		if node.Kind == "Movie" || node.Kind == "Episode" {
			itemIDs = append(itemIDs, node.ID)
		}
	}
	views, err := e.repo.MediaView.HongGuoItemsViews(ctx, itemIDs, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	relations := &embyItemRelations{fields: newEmbyListFields(fields), versionsByMetadataID: map[string][]model.MediaView{}}
	if relations.fields.people {
		relations.peopleByMetadataID, err = e.hongGuoPeople(ctx, sourceIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, view := range views {
		relations.versionsByMetadataID[view.CatalogItemID] = append(relations.versionsByMetadataID[view.CatalogItemID], view)
	}
	for _, node := range nodes {
		if node.Kind == "Movie" || node.Kind == "Episode" {
			versions := relations.versionsByMetadataID[node.ID]
			if len(versions) == 0 {
				continue
			}
			versions = orderMediaVersionSiblings(versions, node.MediaID)
			relations.versionsByMetadataID[node.ID] = versions
			items = append(items, e.itemPayloadWithRelations(ctx, &versions[0], userID, node.Kind == "Movie" && node.Favorite, node.PositionMs, node.Played, false, relations))
			continue
		}
		item, err := e.hongGuoNodePayload(ctx, node, userID)
		if err != nil {
			return nil, err
		}
		if item != nil {
			if !relations.fields.providerIDs {
				delete(item, "ProviderIds")
			}
			if relations.fields.people {
				item["People"] = []model.EmbyPerson{}
				if people := relations.peopleByMetadataID[node.SourceID]; people != nil {
					item["People"] = people
				}
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func (e *EmbyService) hongGuoNodePayload(ctx context.Context, node hongGuoNode, userID string) (map[string]any, error) {
	if node.Kind == "Movie" || node.Kind == "Episode" {
		return e.Item(ctx, node.MediaID, userID)
	}
	images := map[string]string{}
	if node.ArtworkID != "" {
		images["Primary"] = node.ArtworkID
	}
	genres := []string{}
	_ = json.Unmarshal([]byte(node.Tags), &genres)
	providers := map[string]string{}
	if node.SourceID != "" {
		providers["HongGuoDB"] = node.SourceID
	}
	item := map[string]any{
		"Id": node.ID, "Name": node.Title, "Type": node.Kind, "ServerId": embyServerID,
		"IsFolder": true, "ParentId": node.ParentID, "IndexNumber": node.SeasonNumber,
		"DateCreated": formatEmbyDateTime(node.CreatedAt), "ImageTags": images,
		"Overview": node.Overview, "Genres": genres, "CommunityRating": node.Rating,
		"ProviderIds": providers, "RecursiveItemCount": node.EpisodeCount,
		"UserData": map[string]any{"IsFavorite": node.Kind == "Series" && node.Favorite, "Played": node.Played, "PlaybackPositionTicks": 0, "UnplayedItemCount": node.UnplayedItemCount},
	}
	if node.Kind == "Season" {
		item["SeriesId"] = node.ParentID
		item["SeriesName"] = seasonName(node.SeasonNumber)
	}
	return item, nil
}

func lowerStrings(values []string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = strings.ToLower(value)
	}
	return result
}

// hongGuoContainerMutation 仅更改当前可见且有文件的成员，状态仍使用源作品及源集号。
func (e *EmbyService) hongGuoContainerMutation(ctx context.Context, userID, id string, played bool) (bool, error) {
	if !strings.HasPrefix(id, "hg-") || strings.HasPrefix(id, "hg-episode-") {
		return false, nil
	}
	query := e.repo.DB.WithContext(ctx).Table("(?) AS m", e.hongGuoItemFiles(ctx, userID, id)).
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
		Joins("JOIN hongguo_works w ON w.id = b.work_id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").
		Joins("LEFT JOIN media_probe_metadata pm ON pm.media_id = m.id")
	switch {
	case strings.HasPrefix(id, "hg-season-"):
		query = query.Where("w.kind = 'series'")
	case strings.HasPrefix(id, "hg-work-"):
		// 合集成员不再接受旧的独立作品身份。
		query = query.Where("w.kind = 'movie' OR COALESCE(w.related_album_id,'') = '' OR COALESCE(w.season_index,0) <= 0")
	case strings.HasPrefix(id, "hg-group-"):
	default:
		return true, errors.New("media not found")
	}
	// 与节点 MIN(media_id) 保持相同的多版本代表；只读取写状态所需字段。
	query = query.Select(`DISTINCT ON (w.id,ep.id) m.id,m.catalog_source,m.lookup_catalog_id,
w.kind AS view_metadata_kind,COALESCE(ep.number,0) AS view_episode_num,
COALESCE(pm.duration_ms,0) AS view_probe_duration_ms`).Order("w.id,ep.id,m.id")
	handled := true
	err := e.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var views []model.MediaView
		if err := tx.Table("(?) AS targets", query).Scan(&views).Error; err != nil {
			return err
		}
		if len(views) == 0 {
			return errors.New("media not found")
		}
		if views[0].MetadataKind == model.MetadataKindMovie {
			handled = false
			return nil
		}
		episodes := views[:0]
		for _, view := range views {
			if view.EpisodeNum > 0 {
				episodes = append(episodes, view)
			}
		}
		return repository.New(tx).HongGuo.MarkPlayedBatch(ctx, userID, episodes, played)
	})
	return handled || err != nil, err
}
