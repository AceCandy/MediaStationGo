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
	dateSort := strings.Contains(strings.ToLower(p.SortBy), "datecreated")
	played := containsEmbyFilter(p.Filters, "IsPlayed")
	unplayed := containsEmbyFilter(p.Filters, "IsUnplayed")
	q := db.Table("(?) AS candidates", e.repo.MediaView.NFOLibraryCandidates(ctx, p.UserID, p.ParentID, filter, dateSort, played || unplayed))
	if len(p.IncludeItemTypes) > 0 {
		q = q.Where("kind IN ?", lowerStrings(p.IncludeItemTypes))
	}
	if played {
		q = q.Where("played")
	}
	if unplayed {
		q = q.Where("NOT played")
	}
	order := "title"
	if dateSort {
		order = "created_at"
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	page := db.Table("candidates").Order("season_number, episode_number").Order(order).Order("id").Limit(p.Limit).Offset(p.StartIndex)
	totals := "SELECT 0::bigint AS total"
	if count {
		totals = "SELECT COUNT(*) AS total FROM candidates"
	}
	var rows []struct {
		ID    string
		Total int64
	}
	err := db.Raw(`WITH candidates AS MATERIALIZED (?), page AS (?)
SELECT COALESCE(page.id,'') AS id, totals.total FROM (`+totals+`) totals LEFT JOIN page ON TRUE
ORDER BY page.season_number, page.episode_number, page.`+order+`, page.id`, q, page).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	var total int64
	var ids []string
	for _, row := range rows {
		total = row.Total
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return []map[string]any{}, total, nil
	}
	var nodes []hongGuoNode
	err = e.repo.MediaView.NFOWorkNodes(ctx, p.UserID, p.ParentID, filter, ids).Where("parent_id = ''").Scan(&nodes).Error
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
			"UserData": map[string]any{"IsFavorite": node.Kind == "Series" && node.Favorite, "Played": node.Played, "PlaybackPositionTicks": 0},
		})
	}
	return items, nil
}
