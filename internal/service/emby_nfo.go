package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

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
