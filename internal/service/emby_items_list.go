package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) mediaItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	cacheKey := e.embyItemsCacheKey("items", p)
	var cached embyItemsCacheValue
	if e.cache != nil && e.cache.GetJSON(ctx, cacheKey, &cached) {
		return map[string]any{"Items": cached.Items, "TotalRecordCount": cached.TotalRecordCount, "StartIndex": cached.StartIndex}, nil
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{})
	q = e.applyUserMediaVisibility(ctx, q, p.UserID)
	if p.ParentID != "" {
		q = q.Where("media.library_id IN ?", e.mergedLibraryIDs(ctx, p.ParentID))
	}
	if p.SearchTerm != "" {
		q = q.Where("COALESCE(emby_metadata.title, media.scan_title) LIKE ? OR COALESCE(emby_metadata.original_name, '') LIKE ?", "%"+p.SearchTerm+"%", "%"+p.SearchTerm+"%")
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": int64(0), "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins("JOIN favorites ON favorites.user_id = ? AND favorites.deleted_at IS NULL AND favorites.metadata_id = media.metadata_id", p.UserID)
	}
	resumeFilter := containsEmbyFilter(p.Filters, "IsResumable")
	if resumeFilter {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": int64(0), "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins(`JOIN (
			SELECT metadata_id AS item_id, MAX(watched_at) AS watched_at
			FROM playback_histories
			WHERE user_id = ? AND completed = ? AND position_ms > 0
			GROUP BY metadata_id
			) AS resume ON resume.item_id = media.metadata_id`, p.UserID, false)
	}
	filterBySeasonNumbers := true
	parentKnownNonEpisodic := false
	if p.ParentID != "" {
		if episodic, err := e.libraryIsEpisodic(ctx, p.ParentID); err == nil && !episodic {
			filterBySeasonNumbers = false
			parentKnownNonEpisodic = true
		}
	}
	if parentKnownNonEpisodic && containsItemType(p.IncludeItemTypes, "Episode") && !containsItemType(p.IncludeItemTypes, "Movie") {
		return emptyItemsEnvelope(p.StartIndex), nil
	}
	if filterBySeasonNumbers && containsItemType(p.IncludeItemTypes, "Movie") && !containsItemType(p.IncludeItemTypes, "Episode") {
		q = e.filterMovieItems(ctx, q)
	}
	if parentKnownNonEpisodic && containsItemType(p.IncludeItemTypes, "Movie") && !containsItemType(p.IncludeItemTypes, "Episode") {
		q = filterLikelyEpisodicPathsFromMovieQuery(q)
	}
	if filterBySeasonNumbers && containsItemType(p.IncludeItemTypes, "Episode") && !containsItemType(p.IncludeItemTypes, "Movie") {
		q = e.filterEpisodeItems(ctx, q)
	}

	views, total, err := e.metadataPage(ctx, q, p.UserID, metadataOrderSQL(p, resumeFilter), p.StartIndex, p.Limit)
	if err != nil {
		return nil, err
	}
	items := e.payloadsForViews(ctx, views, p.UserID)
	for _, item := range items {
		if item["Type"] == "Movie" {
			item["ParentId"] = p.ParentID
		}
	}
	out := map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}
	if e.cache != nil {
		e.cache.SetJSON(ctx, cacheKey, embyItemsCacheValue{Items: items, TotalRecordCount: total, StartIndex: p.StartIndex}, time.Duration(e.mediaCacheTTLSeconds())*time.Second)
	}
	return out, nil
}

func (e *EmbyService) episodeItems(ctx context.Context, rows []model.MediaView, p ItemsParams) (map[string]any, error) {
	rows = e.collapseMediaVersionViews(ctx, rows)
	if p.SearchTerm != "" {
		filtered := rows[:0]
		needle := strings.ToLower(p.SearchTerm)
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Title), needle) || strings.Contains(strings.ToLower(row.OriginalName), needle) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].SeasonNum != rows[j].SeasonNum {
			return rows[i].SeasonNum < rows[j].SeasonNum
		}
		if rows[i].EpisodeNum != rows[j].EpisodeNum {
			return rows[i].EpisodeNum < rows[j].EpisodeNum
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	total := len(rows)
	items := e.payloadsForViews(ctx, pageSlice(rows, p.StartIndex, p.Limit), p.UserID)
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

func (e *EmbyService) payloadsForMedia(ctx context.Context, rows []model.Media, userID string) ([]map[string]any, error) {
	views, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil {
		return nil, err
	}
	return e.payloadsForViews(ctx, views, userID), nil
}

func (e *EmbyService) payloadsForViews(ctx context.Context, views []model.MediaView, userID string) []map[string]any {
	views = e.collapseMediaVersionViews(ctx, views)
	userFavs := map[string]bool{}
	userPos := map[string]int64{}
	if userID != "" && len(views) > 0 {
		itemIDs := make([]string, 0, len(views))
		for _, view := range views {
			if strings.TrimSpace(view.ID) != "" {
				itemIDs = append(itemIDs, embyItemID(&view))
			}
		}
		var favs []model.Favorite
		favQuery := e.repo.DB.WithContext(ctx).Where("user_id = ?", userID).
			Where("metadata_id IN ?", itemIDs)
		_ = favQuery.Find(&favs).Error
		for _, f := range favs {
			userFavs[f.MetadataID] = true
		}
		var hist []model.PlaybackHistory
		histQuery := e.repo.DB.WithContext(ctx).Where("user_id = ?", userID).
			Where("metadata_id IN ?", itemIDs).
			Order("watched_at desc")
		_ = histQuery.Find(&hist).Error
		for _, h := range hist {
			if _, ok := userPos[h.MetadataID]; !ok {
				userPos[h.MetadataID] = h.PositionMs
			}
		}
	}

	items := make([]map[string]any, 0, len(views))
	for i := range views {
		m := &views[i]
		itemID := embyItemID(m)
		items = append(items, e.itemPayload(ctx, m, userID, userFavs[itemID], userPos[itemID], false))
	}
	return items
}

func (e *EmbyService) collapseMediaVersionViews(ctx context.Context, rows []model.MediaView) []model.MediaView {
	if len(rows) < 2 {
		return rows
	}
	out := make([]model.MediaView, 0, len(rows))
	indexByKey := make(map[string]int, len(rows))
	for _, row := range rows {
		key := e.mediaVersionKey(ctx, &row)
		if key == "" {
			out = append(out, row)
			continue
		}
		if idx, ok := indexByKey[key]; ok {
			if preferMediaVersion(row.Media, out[idx].Media) {
				out[idx] = row
			}
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, row)
	}
	return out
}

func (e *EmbyService) seriesItemsForLibrary(ctx context.Context, libraryID string, p ItemsParams) (map[string]any, error) {
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("media.season_num > 0 OR media.episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, p.UserID)
	q = seriesScopeQuery(q)
	if libraryID != "" {
		q = q.Where("media.library_id IN ?", e.mergedLibraryIDs(ctx, libraryID))
	}
	if p.SearchTerm != "" {
		q = q.Where("scope_series.title LIKE ? OR COALESCE(scope_series.original_name, '') LIKE ?", "%"+p.SearchTerm+"%", "%"+p.SearchTerm+"%")
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins("JOIN favorites ON favorites.user_id = ? AND favorites.deleted_at IS NULL AND favorites.metadata_id = scope_series.id", p.UserID)
	}
	groups, total, err := e.seriesMetadataPage(ctx, q, p.UserID, p, p.StartIndex, p.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, minInt(p.Limit, len(groups)))
	for _, group := range groups {
		item := e.seriesPayload(ctx, group, p.UserID)
		item["ParentId"] = libraryID
		items = append(items, item)
	}
	return map[string]any{"Items": items, "TotalRecordCount": int(total), "StartIndex": p.StartIndex}, nil
}
