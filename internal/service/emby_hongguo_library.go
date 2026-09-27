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

// hongGuoWorkScope 共享作品资格与状态；日期聚合仅接受调用方固定的 MIN/MAX，空串表示无需文件日期。
func (e *EmbyService) hongGuoWorkScope(ctx context.Context, p ItemsParams, dateAggregate string) (scoped, albums, states *gorm.DB) {
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
	// 合集代表和全局时间只在作品层汇总一次，不能按当前库文件限制成员。
	albums = db.Table("hongguo_works").
		Where("kind = 'series' AND related_album_id <> '' AND season_index > 0").
		Select("related_album_id AS id, (ARRAY_AGG(title ORDER BY season_index,source_id))[1] AS title, MAX(latest_media_added_at) AS latest_at").
		Group("related_album_id")
	scoped = db.Table("hongguo_works w").
		Joins("LEFT JOIN albums g ON w.kind = 'series' AND w.season_index > 0 AND g.id = w.related_album_id")
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
	latest := "CASE WHEN g.id IS NULL THEN w.latest_media_added_at ELSE g.latest_at END AS latest_at"
	dates := "NULL::timestamp AS created_at, " + latest
	if dateAggregate == "" {
		scoped = scoped.Where("EXISTS (? OFFSET 0)", workFiles.Session(&gorm.Session{}).Select("1"))
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
	scoped = scoped.Select(`w.id AS work_id, w.source_id, w.rating, CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END AS id,
 CASE WHEN w.kind = 'movie' THEN 'Movie' ELSE 'Series' END AS kind,
 COALESCE(g.title,w.title) AS title, ` + dates + ", " + played + " AS played")
	return
}

// hongGuoLibraryItems 在作品粒度排序分页，只为页内作品展开详情；Latest 不计总数。
func (e *EmbyService) hongGuoLibraryItems(ctx context.Context, p ItemsParams, count bool) ([]map[string]any, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	dateAggregate := ""
	if strings.Contains(strings.ToLower(p.SortBy), "datecreated") {
		dateAggregate = "MIN"
	}
	scoped, albums, states := e.hongGuoWorkScope(ctx, p, dateAggregate)
	scoped = scoped.Where("w.latest_media_added_at IS NOT NULL")
	works := db.Table("scoped").Select("id,kind,title,MIN(created_at) AS created_at,MAX(latest_at) AS latest_at").Group("id,kind,title")
	if len(p.IncludeItemTypes) > 0 {
		works = works.Where("LOWER(kind) IN ?", lowerStrings(p.IncludeItemTypes))
	}
	if containsEmbyFilter(p.Filters, "IsPlayed") {
		works = works.Having("BOOL_AND(played)")
	}
	if containsEmbyFilter(p.Filters, "IsUnplayed") {
		works = works.Having("NOT BOOL_AND(played)")
	}
	order := "title"
	if strings.Contains(strings.ToLower(p.SortBy), "datecreated") {
		order = "created_at"
	} else if strings.Contains(strings.ToLower(p.SortBy), "datelastcontentadded") {
		order = "latest_at"
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	order += " NULLS LAST"
	page := db.Table("works").Order(order).Order("id").Limit(p.Limit).Offset(p.StartIndex)
	var rows []struct {
		ID     string
		WorkID string
		Total  int64
	}
	totals := "SELECT 0::bigint AS total"
	if count {
		totals = "SELECT COUNT(*) AS total FROM works"
	}
	with, args := "WITH albums AS MATERIALIZED (?)", []any{albums}
	if states != nil {
		// 有效已看集号先按源作品汇总，避免相关子查询反复扫描用户的全部状态。
		with += ", playback_states AS MATERIALIZED (?)"
		args = append(args, states)
	}
	var query *gorm.DB
	if !count && order == "latest_at DESC NULLS LAST" {
		// 先按全局合集时间排序，不截断候选；逐合集复用原可见性和播放状态规则。
		ordered := db.Table("hongguo_works w").Where("w.latest_media_added_at IS NOT NULL").
			Joins("LEFT JOIN albums g ON w.kind = 'series' AND w.season_index > 0 AND g.id = w.related_album_id").
			Select(`CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END AS id,
 MAX(CASE WHEN g.id IS NULL THEN w.latest_media_added_at ELSE g.latest_at END) AS latest_at, ARRAY_AGG(w.id) AS work_ids`).
			Group("CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END").
			Order("latest_at DESC NULLS LAST, id")
		if p.ParentID != "" {
			ordered = repository.FilterWorkLibraries(ordered, "w.library_ids", e.mergedLibraryIDs(ctx, p.ParentID))
		}
		matched := db.Table("(?) scoped", scoped.Where("w.id = ANY(ordered.work_ids)")).
			Select("ARRAY_AGG(work_id) AS work_ids, BOOL_AND(played) AS played")
		if len(p.IncludeItemTypes) > 0 {
			matched = matched.Where("LOWER(kind) IN ?", lowerStrings(p.IncludeItemTypes))
		}
		page = db.Table("(? OFFSET 0) ordered", ordered).
			Joins("JOIN LATERAL (?) matched ON cardinality(matched.work_ids) > 0", matched).
			Select("ordered.id, ordered.latest_at, matched.work_ids").
			Order("ordered.latest_at DESC NULLS LAST, ordered.id").Limit(p.Limit).Offset(p.StartIndex)
		if containsEmbyFilter(p.Filters, "IsPlayed") {
			page = page.Where("matched.played")
		}
		if containsEmbyFilter(p.Filters, "IsUnplayed") {
			page = page.Where("NOT matched.played")
		}
		query = db.Raw(with+`, page AS (?)
 SELECT page.id, member.work_id, 0::bigint AS total FROM page
 CROSS JOIN LATERAL UNNEST(page.work_ids) member(work_id)
 ORDER BY page.latest_at DESC NULLS LAST, page.id, member.work_id`, append(args, page)...)
	} else {
		query = db.Raw(with+`, scoped AS MATERIALIZED (?), works AS MATERIALIZED (?), page AS MATERIALIZED (?)
 SELECT COALESCE(page.id,'') AS id, COALESCE(scoped.work_id,'') AS work_id, totals.total
 FROM (`+totals+`) totals LEFT JOIN page ON TRUE LEFT JOIN scoped ON scoped.id = page.id
 ORDER BY page.`+order+`, page.id, scoped.work_id`, append(args, scoped, works, page)...)
	}
	err := query.Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	var total int64
	ids, workIDs := []string{}, []string{}
	for _, row := range rows {
		total = row.Total
		if row.WorkID == "" {
			continue
		}
		workIDs = append(workIDs, row.WorkID)
		if len(ids) == 0 || ids[len(ids)-1] != row.ID {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return []map[string]any{}, total, nil
	}
	nodes, err := e.hongGuoLibraryNodes(ctx, p, workIDs)
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
func (e *EmbyService) hongGuoLibraryNodes(ctx context.Context, p ItemsParams, workIDs []string) ([]hongGuoNode, error) {
	db := e.repo.DB.WithContext(ctx)
	works := db.Table("hongguo_works w").Where("w.id = ANY(?)", &workIDs).Joins(repository.HongGuoAlbumJoin).
		Select(`w.id AS work_id, w.source_id, w.overview, w.tags, w.rating, w.season_index,
 g.id AS group_id, COALESCE(g.title,w.title) AS title,
 CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END AS id,
 CASE WHEN w.kind = 'movie' THEN 'Movie' ELSE 'Series' END AS kind`)
	files := e.hongGuoVisibleFiles(ctx, p.UserID, p.ParentID).
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").Where("b.work_id = ANY(?)", &workIDs).
		Joins("JOIN page_works w ON w.work_id = b.work_id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = b.work_id").
		Joins("LEFT JOIN (?) s ON s.source_id = w.source_id AND s.episode_number = COALESCE(ep.number,1)", repository.PlaybackStates(ctx, e.repo.DB, "hongguo", p.UserID, e.mediaQueryFilter(ctx, p.UserID))).
		Select(`b.work_id, MIN(m.id) AS media_id, MIN(m.created_at) AS created_at,
 COUNT(DISTINCT ep.id) AS episode_count, BOOL_AND(COALESCE(s.completed,FALSE)) AS played,
 COUNT(DISTINCT ep.id) FILTER (WHERE NOT COALESCE(s.completed,FALSE)) AS unplayed_item_count,
 MAX(s.watched_at) AS played_at, MAX(COALESCE(s.position_ms,0)) AS position_ms`).Group("b.work_id")
	var nodes []hongGuoNode
	err := db.Raw(`WITH page_works AS MATERIALIZED (?), file_stats AS MATERIALIZED (?)
 SELECT w.id,w.kind,w.title, MIN(v.media_id) AS media_id, MIN(v.created_at) AS created_at,
 SUM(v.episode_count) AS episode_count, BOOL_AND(v.played) AS played,
 SUM(v.unplayed_item_count) AS unplayed_item_count,
 MAX(v.played_at) AS played_at, MAX(v.position_ms) AS position_ms,
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
 GROUP BY w.id,w.kind,w.title,w.group_id`, works, files, p.UserID).Scan(&nodes).Error
	return nodes, err
}
