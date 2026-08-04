package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

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

	collapseVersions := e.shouldCollapseMediaVersions(ctx, p)
	var total int64
	totalQuery := q.Session(&gorm.Session{})
	if collapseVersions {
		totalQuery = totalQuery.Distinct("media.metadata_id")
	}
	if err := totalQuery.Count(&total).Error; err != nil {
		return nil, err
	}
	desc := !strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending")
	order := mediaReleaseOrderSQL(true)
	orderIncludesDirection := true
	switch primarySupportedEmbySort(p.SortBy, resumeFilter) {
	case "sortname", "name":
		order = "COALESCE(emby_metadata.title, media.scan_title)"
		orderIncludesDirection = false
	case "premieredate", "productionyear":
		order = mediaReleaseOrderSQL(desc)
	case "datecreated":
		order = "media.created_at"
		orderIncludesDirection = false
	case "dateplayed":
		order = "resume.watched_at"
		orderIncludesDirection = false
	case "communityrating":
		order = "COALESCE(emby_metadata.rating, 0)"
		orderIncludesDirection = false
	}
	if !orderIncludesDirection && strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
		if !strings.HasSuffix(order, " desc") {
			order = order + " desc"
		}
	}

	var views []model.MediaView
	if collapseVersions {
		// Read ordered media in batches until the requested logical page is
		// filled. A fixed over-fetch is not enough when one work has many
		// versions, because all rows in the first batch may collapse to one item.
		fetchOffset := 0
		fetchLimit := maxInt(p.Limit*4, p.Limit)
		target := p.StartIndex + p.Limit
		for {
			var batch []model.Media
			if err := q.Order(order).Offset(fetchOffset).Limit(fetchLimit).Find(&batch).Error; err != nil {
				return nil, err
			}
			batchViews, err := e.mediaViewsForRows(ctx, batch, p.UserID)
			if err != nil {
				return nil, err
			}
			views = append(views, batchViews...)
			views = e.collapseMediaVersionViews(ctx, views)
			if len(views) >= target || len(batch) < fetchLimit {
				break
			}
			fetchOffset += len(batch)
		}
		views = pageSlice(views, p.StartIndex, p.Limit)
	} else {
		var rows []model.Media
		if err := q.Order(order).Offset(p.StartIndex).Limit(p.Limit).Find(&rows).Error; err != nil {
			return nil, err
		}
		var err error
		views, err = e.mediaViewsForRows(ctx, rows, p.UserID)
		if err != nil {
			return nil, err
		}
	}
	items := e.payloadsForViews(ctx, views, p.UserID)
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
		items = append(items, e.itemPayload(ctx, m, userFavs[itemID], userPos[itemID], false))
	}
	return items
}

func (e *EmbyService) shouldCollapseMediaVersions(ctx context.Context, p ItemsParams) bool {
	if containsItemType(p.IncludeItemTypes, "Series") || containsItemType(p.IncludeItemTypes, "Season") {
		return false
	}
	if containsItemType(p.IncludeItemTypes, "Episode") && !containsItemType(p.IncludeItemTypes, "Movie") {
		return true
	}
	if p.ParentID == "" {
		return true
	}
	episodic, err := e.libraryIsEpisodic(ctx, p.ParentID)
	return err == nil && !episodic
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
	if libraryID != "" {
		q = q.Where("media.library_id IN ?", e.mergedLibraryIDs(ctx, libraryID))
	}
	if p.SearchTerm != "" {
		q = q.Where("COALESCE(emby_metadata.title, media.scan_title) LIKE ? OR COALESCE(emby_metadata.original_name, '') LIKE ?", "%"+p.SearchTerm+"%", "%"+p.SearchTerm+"%")
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins("JOIN metadata_items AS favorite_season ON favorite_season.id = emby_metadata.parent_id AND favorite_season.kind = 'season' AND favorite_season.deleted_at IS NULL").
			Joins("JOIN favorites ON favorites.user_id = ? AND favorites.deleted_at IS NULL AND favorites.metadata_id = favorite_season.parent_id", p.UserID)
	}
	var rows []model.Media
	if err := q.Order(mediaReleaseOrderSQL(true)).Limit(embySeriesGroupingLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	displayRows, err := e.mediaViewsForRows(ctx, rows, p.UserID)
	if err != nil {
		return nil, err
	}
	groups := e.seriesGroupsFromMedia(displayRows)
	sortSeriesGroups(groups, p)
	total := len(groups)
	items := make([]map[string]any, 0, minInt(p.Limit, len(groups)))
	for _, group := range pageSlice(groups, p.StartIndex, p.Limit) {
		items = append(items, e.seriesPayload(ctx, group, p.UserID))
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}
