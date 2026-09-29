package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// nfoLibraryItems 保留 NFO 原排序与状态规则，只为当前页加载展示节点。
func (e *EmbyService) nfoLibraryItems(ctx context.Context, p ItemsParams, count bool) ([]map[string]any, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	filter := e.mediaQueryFilter(ctx, p.UserID)
	dateSort := !embyRandomSort(p) && strings.Contains(strings.ToLower(p.SortBy), "datecreated")
	latest := !count && !dateSort && strings.Contains(strings.ToLower(p.SortBy), "datelastcontentadded") && strings.EqualFold(p.SortOrder, "Descending")
	played := containsEmbyFilter(p.Filters, "IsPlayed")
	unplayed := containsEmbyFilter(p.Filters, "IsUnplayed")
	q := db.Table("(?) AS candidates", e.repo.MediaView.NFOWorkCandidates(ctx, p.UserID, p.ParentID, filter, false))
	columns := "id, kind, title, season_number, episode_number, latest_at"
	if dateSort {
		columns += ", created_at"
	}
	if embyRandomSort(p) {
		columns += ", " + embyRandomOrder(p, "id") + " AS random_order"
	}
	// 播放状态只用于过滤，不在物化结果中再次计算。
	q = q.Select(columns)
	if len(p.IncludeItemTypes) > 0 {
		q = q.Where("kind IN ?", lowerStrings(p.IncludeItemTypes))
	}
	order := "title"
	if embyRandomSort(p) {
		order = "random_order"
	} else if dateSort {
		order = "created_at"
	} else if strings.Contains(strings.ToLower(p.SortBy), "datelastcontentadded") {
		order = "latest_at"
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	order += " NULLS LAST"
	prefix := "season_number, episode_number, "
	pagePrefix := "page.season_number, page.episode_number, "
	if embyRandomSort(p) {
		prefix, pagePrefix = "", ""
	}
	page := db.Table("candidates").Order(prefix + order + ", id").Limit(p.Limit).Offset(p.StartIndex)
	totals := "SELECT 0::bigint AS total"
	materialization := "MATERIALIZED"
	if latest {
		materialization = "NOT MATERIALIZED"
	}
	if count {
		totals = "SELECT COUNT(*) AS total FROM candidates"
	}
	var rows []struct {
		ID    string
		Kind  string
		Total int64
	}
	var err error
	var selected []string
	var selectedTotal int64
	if played || unplayed {
		candidates := db.Table("(?) candidates", q).Select("*, ROW_NUMBER() OVER (ORDER BY " + prefix + order + ", id) AS ordinal").Order(prefix + order + ", id")
		states := e.repo.MediaView.NFOWorkCandidates(ctx, p.UserID, p.ParentID, filter, true).
			Where("root.id IN (SELECT id FROM work_batch)")
		eligible := db.Table("(?) states", states).Select("id")
		if played {
			eligible = eligible.Where("played")
		}
		if unplayed {
			eligible = eligible.Where("NOT played")
		}
		eligible = db.Table("work_batch").Select("ordinal").Where("id IN (?)", eligible)
		selected, selectedTotal, err = e.filteredWorkBatchPage(ctx, candidates, eligible, p.StartIndex, p.Limit, count)
		if err == nil && len(selected) > 0 {
			err = q.Session(&gorm.Session{}).Where("id IN ?", selected).Select("id, kind").Scan(&rows).Error
		}
	} else {
		err = db.Raw(`WITH candidates AS `+materialization+` (?), page AS (?)
SELECT COALESCE(page.id,'') AS id, page.kind, totals.total FROM (`+totals+`) totals LEFT JOIN page ON TRUE
ORDER BY `+pagePrefix+`page.`+order+`, page.id`, q, page).Scan(&rows).Error
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	var ids []string
	containersOnly := true
	for _, row := range rows {
		total = row.Total
		if row.ID != "" {
			ids = append(ids, row.ID)
			containersOnly = containersOnly && row.Kind == "series"
		}
	}
	if played || unplayed {
		ids, total = selected, selectedTotal
	}
	if len(ids) == 0 {
		return []map[string]any{}, total, nil
	}
	var nodes []hongGuoNode
	err = e.repo.MediaView.NFOWorkNodes(ctx, p.UserID, p.ParentID, filter, ids, containersOnly).Where("parent_id = ''").Scan(&nodes).Error
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]hongGuoNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	nodes = nodes[:0]
	for _, id := range ids {
		if node, ok := byID["nfo-"+id]; ok {
			nodes = append(nodes, node)
		}
	}
	items, err := e.nfoNodePayloads(ctx, nodes, p.UserID, p.Fields)
	return items, total, err
}

func (e *EmbyService) nfoNodes(ctx context.Context, userID, libraryID string) *gorm.DB {
	q := e.repo.MediaView.NFONodes(ctx, userID, libraryID, e.mediaQueryFilter(ctx, userID))
	v := e.mediaVisibility(ctx, userID)
	if v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0 {
		q = q.Where("FALSE")
	}
	return q
}

// nfoItemNodes 供单项详情和子层级查询使用；调用方仍须筛选目标节点或父身份。
func (e *EmbyService) nfoItemNodes(ctx context.Context, userID string, ids ...string) *gorm.DB {
	itemIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		itemIDs = append(itemIDs, strings.TrimPrefix(id, "nfo-"))
	}
	return e.repo.MediaView.NFOWorkNodes(ctx, userID, "", e.mediaQueryFilter(ctx, userID), itemIDs, false)
}

// nfoNodePayloads 复用文件播放响应；容器不伪装为可播放文件。
func (e *EmbyService) nfoNodePayloads(ctx context.Context, nodes []hongGuoNode, userID string, fields []string) ([]map[string]any, error) {
	ids := []string{}
	for _, node := range nodes {
		if node.Kind == "Movie" || node.Kind == "Episode" {
			ids = append(ids, node.ID)
		}
	}
	views, err := e.repo.MediaView.FindByLogicalMetadataIDs(ctx, ids, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	relations := &embyItemRelations{fields: newEmbyListFields(fields), versionsByMetadataID: map[string][]model.MediaView{}}
	for _, view := range views {
		relations.versionsByMetadataID[view.CatalogItemID] = append(relations.versionsByMetadataID[view.CatalogItemID], view)
	}
	items := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == "Movie" || node.Kind == "Episode" {
			versions := orderMediaVersionSiblings(relations.versionsByMetadataID[node.ID], node.MediaID)
			if len(versions) == 0 {
				continue
			}
			relations.versionsByMetadataID[node.ID] = versions
			item := e.itemPayloadWithRelations(ctx, &versions[0], userID, node.Favorite, node.PositionMs, node.Played, false, relations)
			if !node.PlayedAt.IsZero() {
				item["UserData"].(map[string]any)["LastPlayedDate"] = formatEmbyDateTime(node.PlayedAt)
			}
			items = append(items, item)
			continue
		}
		images := map[string]string{}
		if node.ArtworkID != "" {
			images["Primary"] = node.ArtworkID
		}
		items = append(items, map[string]any{
			"Id": node.ID, "Name": node.Title, "Type": node.Kind, "ServerId": embyServerID,
			"IsFolder": true, "ParentId": node.ParentID, "IndexNumber": node.SeasonNumber,
			"DateCreated": formatEmbyDateTime(node.CreatedAt), "ImageTags": images,
			"Overview": node.Overview, "CommunityRating": node.Rating, "RecursiveItemCount": node.EpisodeCount,
			"UserData": map[string]any{"IsFavorite": node.Kind == "Series" && node.Favorite, "Played": node.Played, "PlaybackPositionTicks": 0, "UnplayedItemCount": node.UnplayedItemCount},
		})
	}
	return items, nil
}
