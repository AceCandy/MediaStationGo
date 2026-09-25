package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
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

// hongGuoLibraryItems 在作品粒度排序分页，只为页内作品展开详情；Latest 不计总数。
func (e *EmbyService) hongGuoLibraryItems(ctx context.Context, p ItemsParams, count bool) ([]map[string]any, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	files := e.hongGuoVisibleFiles(ctx, p.UserID, p.ParentID).
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").Where("b.work_id = w.id")
	stats := files.Select("MIN(m.created_at) AS created_at, MAX(m.created_at) AS latest_at, COUNT(*) AS file_count")
	playedFilter := containsEmbyFilter(p.Filters, "IsPlayed") || containsEmbyFilter(p.Filters, "IsUnplayed")
	played := "FALSE"
	if playedFilter {
		states := repository.PlaybackStates(ctx, e.repo.DB, "hongguo", p.UserID, e.mediaQueryFilter(ctx, p.UserID))
		stats = stats.Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").
			Joins("LEFT JOIN (?) s ON s.source_id = w.source_id AND s.episode_number = COALESCE(ep.number,1)", states).
			Select("MIN(m.created_at) AS created_at, MAX(m.created_at) AS latest_at, COUNT(*) AS file_count, BOOL_AND(COALESCE(s.completed,FALSE)) AS played")
		played = "v.played"
	}
	scoped := db.Table("hongguo_works w").Joins(repository.HongGuoAlbumJoin)
	dates := "v.created_at, v.latest_at"
	dateSort := strings.Contains(strings.ToLower(p.SortBy), "datecreated") || strings.Contains(strings.ToLower(p.SortBy), "datelastcontentadded")
	if !dateSort && !playedFilter {
		// 标题浏览只判断可见文件存在；完整时间和状态留到当前页详情读取。
		scoped = scoped.Where("EXISTS (? OFFSET 0)", files.Select("1"))
		dates = "NULL::timestamp AS created_at, NULL::timestamp AS latest_at"
	} else {
		scoped = scoped.Joins("JOIN LATERAL (?) v ON v.file_count > 0", stats)
	}
	scoped = scoped.Select(`w.id AS work_id, CASE WHEN g.id IS NULL THEN 'hg-work-' || w.id ELSE 'hg-group-' || g.id END AS id,
 CASE WHEN w.kind = 'movie' THEN 'Movie' ELSE 'Series' END AS kind,
 COALESCE(g.title,w.title) AS title, ` + dates + ", " + played + " AS played")
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
	err := db.Raw(`WITH scoped AS MATERIALIZED (?), works AS MATERIALIZED (?), page AS MATERIALIZED (?)
 SELECT COALESCE(page.id,'') AS id, COALESCE(scoped.work_id,'') AS work_id, totals.total
 FROM (`+totals+`) totals LEFT JOIN page ON TRUE LEFT JOIN scoped ON scoped.id = page.id
 ORDER BY page.`+order+`, page.id, scoped.work_id`, scoped, works, page).Scan(&rows).Error
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
	// 显式绑定当前页来源作品，避免详情查询重新扫描全库绑定。
	pageFiles := e.hongGuoVisibleFiles(ctx, p.UserID, p.ParentID).
		Where("m.id IN (?)", db.Table("hongguo_media_bindings").Select("media_id").Where("work_id = ANY(?)", &workIDs))
	var nodes []hongGuoNode
	if err := e.hongGuoFileNodes(ctx, p.UserID, pageFiles).Where("parent_id = ''").Scan(&nodes).Error; err != nil {
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
