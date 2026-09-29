package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// hongGuoLibraryPageSupported 只接管作品层请求；季集和特殊筛选保留层级查询。
func hongGuoLibraryPageSupported(p ItemsParams) bool {
	if p.SearchTerm != "" || len(p.PersonIDs) > 0 || containsEmbyFilter(p.Filters, "IsFavorite") || containsEmbyFilter(p.Filters, "IsResumable") {
		return false
	}
	if p.Recursive && len(p.IncludeItemTypes) == 0 {
		return false
	}
	for _, kind := range p.IncludeItemTypes {
		if !strings.EqualFold(kind, "Series") && !strings.EqualFold(kind, "Movie") {
			return false
		}
	}
	return true
}

// hongGuoWorkScope 仅为候选排序汇总合集标题和全局时间，资格查询直接使用作品关系。
func (e *EmbyService) hongGuoWorkScope(ctx context.Context, p ItemsParams, dateAggregate string) (scoped, albums, states *gorm.DB) {
	scoped, states = e.hongGuoWorkMembers(ctx, p, dateAggregate)
	albums = e.repo.DB.WithContext(ctx).Table("hongguo_works").
		Where("kind = 'series' AND related_album_id <> '' AND season_index > 0").
		Select("related_album_id AS id, (ARRAY_AGG(title ORDER BY season_index,source_id))[1] AS title, MAX(latest_media_added_at) AS latest_at").
		Group("related_album_id")
	scoped = e.repo.DB.WithContext(ctx).Table("(?) w", scoped).
		Joins("LEFT JOIN albums g ON w.kind = 'series' AND g.id = w.related_album_id").
		Select(`w.work_id, w.source_id, w.rating, w.id, w.kind, COALESCE(g.title,w.title) AS title,
w.created_at, CASE WHEN w.kind = 'series' THEN g.latest_at ELSE w.latest_at END AS latest_at, w.played`)
	return
}

// hongGuoWorkMembers 只检查作品资格与状态；日期聚合仅接受固定 MIN/MAX，空串不读文件日期。
func (e *EmbyService) hongGuoWorkMembers(ctx context.Context, p ItemsParams, dateAggregate string) (scoped, states *gorm.DB) {
	db := e.repo.DB.WithContext(ctx)
	files := e.hongGuoVisibleFiles(ctx, p.UserID, p.ParentID).
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id")
	workFiles := files.Session(&gorm.Session{}).Where("b.work_id = w.id")
	playedFilter := containsEmbyFilter(p.Filters, "IsPlayed") || containsEmbyFilter(p.Filters, "IsUnplayed")
	played := "FALSE"
	completed := "COALESCE(COALESCE(ep.number,1) = ANY(ps.episodes),FALSE)"
	if playedFilter {
		states = db.Table("(?) effective", repository.CompletedPlaybackStates(ctx, e.repo.DB, "hongguo", p.UserID, e.mediaQueryFilter(ctx, p.UserID))).
			Select("source_id, ARRAY_AGG(episode_number) AS episodes").Group("source_id")
	}
	scoped = db.Table("hongguo_works w").Where(repository.HongGuoReadyWorkSQL)
	if p.ParentID != "" {
		scoped = repository.FilterWorkLibraries(scoped, "w.library_ids", e.mergedLibraryIDs(ctx, p.ParentID))
	} else {
		scoped = repository.FilterWorkLibraries(scoped, "w.library_ids", e.mediaVisibility(ctx, p.UserID).AllowedLibraryIDs)
	}
	if playedFilter {
		scoped = scoped.Joins("LEFT JOIN playback_states ps ON ps.source_id = w.source_id")
		unplayed := workFiles.Session(&gorm.Session{}).
			Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").Select("1").Where("NOT " + completed)
		// 无有效已看集时直接判未看；否则只探测是否仍有未看文件。
		scoped = scoped.Joins("JOIN LATERAL (SELECT ps.source_id IS NOT NULL AND NOT EXISTS (? OFFSET 0) AS played) v ON TRUE", unplayed)
		played = "v.played"
	}
	latest := "w.latest_media_added_at AS latest_at"
	dates := "NULL::timestamp AS created_at, " + latest
	if dateAggregate == "" {
		// 已维护的库归属就是现存绑定库集合；仅未知归属回查文件。
		filter := e.mediaQueryFilter(ctx, p.UserID)
		visible := db.Table("jsonb_array_elements_text(w.library_ids) AS library(id)").Select("1")
		if p.ParentID != "" {
			visible = visible.Where("library.id = ?", p.ParentID)
		}
		if len(filter.AllowedLibraryIDs) > 0 {
			visible = visible.Where("library.id = ANY(?)", &filter.AllowedLibraryIDs)
		}
		if len(filter.HiddenLibraryIDs) > 0 {
			visible = visible.Where("library.id <> ALL(?)", &filter.HiddenLibraryIDs)
		}
		scoped = scoped.Where("CASE WHEN w.library_ids IS NULL THEN EXISTS (? OFFSET 0) ELSE EXISTS (?) END", workFiles.Session(&gorm.Session{}).Select("1"), visible)
	} else {
		if p.ParentID == "" {
			// 全局日期排序必须读取全部可见文件日期，一次按源作品汇总避免逐作品随机回表。
			stats := files.Session(&gorm.Session{}).Select("b.work_id, " + dateAggregate + "(m.created_at) AS created_at").Group("b.work_id")
			scoped = scoped.Joins("JOIN (?) dates ON dates.work_id=w.id AND dates.created_at IS NOT NULL", stats)
		} else {
			stats := workFiles.Session(&gorm.Session{}).Select(dateAggregate + "(m.created_at) AS created_at")
			scoped = scoped.Joins("JOIN LATERAL (?) dates ON dates.created_at IS NOT NULL", stats)
		}
		dates = "dates.created_at, " + latest
	}
	scoped = scoped.Select(`w.id AS work_id, w.source_id, w.rating, ` + repository.HongGuoWorkIdentitySQL + ` AS id,
 w.kind, w.related_album_id, w.title, ` + dates + ", " + played + " AS played")
	return
}

// hongGuoLibraryLatestWorks 按合集一次汇总全局时间；可见成员只决定资格和后续批内状态范围。
func (e *EmbyService) hongGuoLibraryLatestWorks(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	unknown, _ := e.hongGuoWorkMembers(ctx, p, "")
	unknown = unknown.Where("w.library_ids IS NULL AND w.latest_media_added_at IS NOT NULL").Select("w.id")
	return db.Table("hongguo_works w").Where(repository.HongGuoReadyWorkSQL).
		Select(repository.HongGuoWorkIdentitySQL+` AS id, w.kind, MAX(w.latest_media_added_at) AS latest_at,
 MAX(w.related_album_id) AS related_album_id, MIN(w.id) AS work_id`).
		Group(repository.HongGuoWorkIdentitySQL+", w.kind").
		Having(`BOOL_OR(w.latest_media_added_at IS NOT NULL AND
 COALESCE(w.library_ids @> jsonb_build_array(?::text), w.id IN (?)))`, p.ParentID, unknown)
}

// hongGuoLibraryItems 在作品粒度排序分页，只为页内作品展开详情；Latest 不计总数。
func (e *EmbyService) hongGuoLibraryItems(ctx context.Context, p ItemsParams, count bool) ([]map[string]any, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	dateAggregate := ""
	if !embyRandomSort(p) && strings.Contains(strings.ToLower(p.SortBy), "datecreated") {
		dateAggregate = "MIN"
	}
	baseParams := p
	baseParams.Filters = nil
	order := "title"
	if embyRandomSort(p) {
		order = embyRandomOrder(p, "id")
	} else if strings.Contains(strings.ToLower(p.SortBy), "datecreated") {
		order = "created_at"
	} else if strings.Contains(strings.ToLower(p.SortBy), "datelastcontentadded") {
		order = "latest_at"
	}
	var works *gorm.DB
	latestInLibrary := p.ParentID != "" && order == "latest_at"
	if latestInLibrary {
		if !e.mediaVisibility(ctx, p.UserID).allows(p.ParentID) {
			return []map[string]any{}, 0, nil
		}
		works = e.hongGuoLibraryLatestWorks(ctx, baseParams)
	} else {
		scoped, albums, _ := e.hongGuoWorkScope(ctx, baseParams, dateAggregate)
		works = db.Table("(?) scoped", scoped.Where("w.latest_at IS NOT NULL")).
			Select("id,kind,title,MIN(created_at) AS created_at,MAX(latest_at) AS latest_at,ARRAY_AGG(work_id) AS work_ids").
			Group("id,kind,title")
		works = db.Table("(?) works", db.Raw("WITH albums AS MATERIALIZED (?) ?", albums, works))
	}
	if len(p.IncludeItemTypes) > 0 {
		works = works.Where("kind IN ?", lowerStrings(p.IncludeItemTypes))
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	order += " NULLS LAST"
	candidates := db.Table("(?) works", works).
		Select("*, ROW_NUMBER() OVER (ORDER BY " + order + ", id) AS ordinal").Order(order + ", id")
	var ids []string
	var total int64
	var err error
	played, unplayed := containsEmbyFilter(p.Filters, "IsPlayed"), containsEmbyFilter(p.Filters, "IsUnplayed")
	if played || unplayed {
		filtered, states := e.hongGuoWorkMembers(ctx, p, "")
		// 批次已经是归并后的卡片；只读取本批成员的有效状态和必要文件。
		members := db.Table("work_batch").Select("UNNEST(work_ids)")
		if latestInLibrary {
			visible, _ := e.hongGuoWorkMembers(ctx, baseParams, "")
			visible = visible.Where("(batch.kind = 'series' AND w.kind = 'series' AND w.related_album_id = batch.related_album_id) OR (batch.kind = 'movie' AND w.id = batch.work_id)").
				Where("w.latest_media_added_at IS NOT NULL").Select("w.id")
			members = db.Table("work_batch batch").Joins("JOIN LATERAL (? OFFSET 0) member ON TRUE", visible).Select("member.id")
		}
		filtered = filtered.Where("w.id IN (?)", members)
		states = db.Table("hongguo_works state_work").Where("state_work.id IN (?)", members).
			Joins("JOIN LATERAL (? OFFSET 0) state ON TRUE", states.Where("source_id=state_work.source_id")).Select("state.*")
		eligible := db.Table("(?) scoped", filtered).Select("id").Group("id")
		if played {
			eligible = eligible.Having("BOOL_AND(played)")
		}
		if unplayed {
			eligible = eligible.Having("NOT BOOL_AND(played)")
		}
		eligible = db.Raw("WITH playback_states AS MATERIALIZED (?) ?", states, eligible)
		eligible = db.Table("work_batch").Select("ordinal").Where("id IN (?)", eligible)
		ids, total, err = e.filteredWorkBatchPage(ctx, candidates, eligible, p.StartIndex, p.Limit, count)
	} else if count {
		ids, total, err = e.workCandidatePage(ctx, candidates, p.StartIndex, p.Limit)
	} else {
		err = db.Table("(?) works", candidates).Order("ordinal").Offset(p.StartIndex).Limit(p.Limit).Pluck("id", &ids).Error
	}
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return []map[string]any{}, total, nil
	}
	var members []struct {
		WorkID string
		Kind   string
	}
	pageMembers, _ := e.hongGuoWorkMembers(ctx, baseParams, "")
	pageMembers = repository.FilterHongGuoWorkIDs(pageMembers, ids).
		Where("w.latest_media_added_at IS NOT NULL").Select("w.id AS work_id, w.kind")
	if err := pageMembers.Scan(&members).Error; err != nil {
		return nil, 0, err
	}
	workIDs := make([]string, 0, len(members))
	seriesOnly := true
	for _, member := range members {
		workIDs = append(workIDs, member.WorkID)
		seriesOnly = seriesOnly && member.Kind == "series"
	}
	nodes, err := e.hongGuoLibraryNodes(ctx, p, workIDs, seriesOnly)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]hongGuoNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	nodes = nodes[:0]
	for _, id := range ids {
		if node, ok := byID[id]; ok {
			nodes = append(nodes, node)
		}
	}
	items, err := e.hongGuoNodePayloads(ctx, nodes, p.UserID, p.Fields)
	return items, total, err
}

// hongGuoLibraryNodes 先按源作品汇总页内文件，再关联一次作品资料、封面和收藏。
// 不展开季集节点；保留文件最早入库时间、分集去重及有效已看状态的原有语义。
func (e *EmbyService) hongGuoLibraryNodes(ctx context.Context, p ItemsParams, workIDs []string, seriesOnly bool) ([]hongGuoNode, error) {
	db := e.repo.DB.WithContext(ctx)
	works := db.Table("hongguo_works w").Where("w.id = ANY(?)", &workIDs).Joins(repository.HongGuoAlbumJoin).
		Select(`w.id AS work_id, w.source_id, w.overview, w.tags, w.rating, w.season_index,
 g.id AS group_id, COALESCE(g.title,w.title) AS title,
 CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END AS id,
 CASE WHEN w.kind = 'movie' THEN 'Movie' ELSE 'Series' END AS kind`)
	states := repository.PlaybackStates(ctx, e.repo.DB, "hongguo", p.UserID, e.mediaQueryFilter(ctx, p.UserID))
	completed := "COALESCE(s.completed,FALSE)"
	episodeFields := ", MIN(m.id) AS media_id"
	fileFields := ", MIN(v.media_id) AS media_id, MAX(s.watched_at) AS played_at, MAX(COALESCE(s.position_ms,0)) AS position_ms"
	nodeFields := ", MIN(v.media_id) AS media_id, MAX(v.played_at) AS played_at, MAX(v.position_ms) AS position_ms"
	if seriesOnly {
		states = repository.CompletedPlaybackStates(ctx, e.repo.DB, "hongguo", p.UserID, e.mediaQueryFilter(ctx, p.UserID))
		completed = "s.source_id IS NOT NULL"
		episodeFields, fileFields, nodeFields = "", "", ""
	}
	// 按页内源作品读取状态并物化一次，避免逐文件探测或展开整位用户的历史。
	states = db.Table("page_works state_work").
		Joins("JOIN LATERAL (? OFFSET 0) state ON TRUE", states.Where("source_id = state_work.source_id")).Select("state.*")
	files := e.hongGuoVisibleFiles(ctx, p.UserID, p.ParentID).
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").Where("b.work_id = ANY(?)", &workIDs).
		Joins("JOIN page_works w ON w.work_id = b.work_id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = b.work_id").
		Select("b.work_id, w.source_id, ep.id AS episode_id, COALESCE(ep.number,1) AS episode_number, MIN(m.created_at) AS created_at" + episodeFields).
		Group("b.work_id, w.source_id, ep.id, ep.number")
	// 先归并同集文件版本，再与状态集合连接，避免状态被嵌入每个文件的索引探测。
	stats := db.Table("page_files v").
		Joins("LEFT JOIN page_states s ON s.source_id = v.source_id AND s.episode_number = v.episode_number").
		Select(`v.work_id, MIN(v.created_at) AS created_at,
 COUNT(DISTINCT v.episode_id) AS episode_count, BOOL_AND(` + completed + `) AS played,
 COUNT(DISTINCT v.episode_id) FILTER (WHERE NOT (` + completed + `)) AS unplayed_item_count` + fileFields).Group("v.work_id")
	var nodes []hongGuoNode
	err := db.Raw(`WITH page_works AS MATERIALIZED (?), page_states AS MATERIALIZED (?),
 page_files AS MATERIALIZED (?), file_stats AS MATERIALIZED (?)
 SELECT w.id,w.kind,w.title, MIN(v.created_at) AS created_at`+nodeFields+`,
 SUM(v.episode_count) AS episode_count, BOOL_AND(v.played) AS played,
 SUM(v.unplayed_item_count) AS unplayed_item_count,
 BOOL_OR(COALESCE(f.favorite,FALSE)) AS favorite,
 CASE WHEN w.group_id IS NULL THEN MIN(w.source_id) ELSE '' END AS source_id,
 CASE WHEN w.group_id IS NULL THEN MIN(w.overview) ELSE '' END AS overview,
 CASE WHEN w.group_id IS NULL THEN MIN(w.tags) ELSE '[]' END AS tags,
 CASE WHEN w.group_id IS NULL THEN MAX(w.rating) ELSE 0 END AS rating,
 COALESCE((ARRAY_AGG(a.id ORDER BY NULLIF(w.season_index,0) NULLS LAST,w.work_id)
 FILTER (WHERE a.id IS NOT NULL))[1],'') AS artwork_id
 FROM page_works w JOIN file_stats v ON v.work_id=w.work_id
 LEFT JOIN hongguo_artworks a ON a.work_id=w.work_id AND a.local_key <> ''
 LEFT JOIN hongguo_user_states f ON f.user_id=? AND f.source_id=w.source_id AND f.episode_number=0
 GROUP BY w.id,w.kind,w.title,w.group_id`, works, states, files, stats, p.UserID).Scan(&nodes).Error
	return nodes, err
}
