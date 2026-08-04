package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Item 单条目详情。
func (e *EmbyService) Item(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	if lib, err := e.repo.Library.FindByID(ctx, mediaID); err != nil {
		return nil, err
	} else if lib != nil {
		libs := FilterDisplayCloudLibraries(ctx, e.repo, []model.Library{*lib})
		if len(libs) == 0 {
			return nil, nil
		}
		visibility := e.mediaVisibility(ctx, userID)
		if !e.libraryVisibleFromCachedVisibility(libs[0], visibility) {
			return nil, nil
		}
		return e.libraryAsView(&libs[0]), nil
	}
	m, err := e.mediaViewForItemID(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if season, ok, err := e.findSeasonGroup(ctx, mediaID, userID); err != nil {
			return nil, err
		} else if ok {
			return e.seasonPayload(ctx, season, userID), nil
		}
		if series, ok, err := e.findSeriesGroup(ctx, mediaID, userID); err != nil {
			return nil, err
		} else if ok {
			return e.seriesPayload(ctx, series, userID), nil
		}
		return nil, nil
	}
	if !e.mediaVisibility(ctx, userID).AllowsView(m) {
		return nil, nil
	}
	target, err := e.itemTarget(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	fav, pos := e.userDataForTarget(ctx, userID, target)
	return e.itemPayload(ctx, m, fav, pos, true), nil
}

// LatestItems 最近添加，全库或指定库。
func (e *EmbyService) LatestItems(ctx context.Context, userID, parentID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	cacheKey := e.embyLatestCacheKey(userID, parentID, limit)
	var cached embyLatestCacheValue
	if e.cache != nil && e.cache.GetJSON(ctx, cacheKey, &cached) {
		return cached.Items, nil
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("media.deleted_at IS NULL")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	if parentID != "" {
		if episodic, err := e.libraryIsEpisodic(ctx, parentID); err == nil && episodic {
			out, err := e.latestSeriesItemsForLibrary(ctx, userID, parentID, limit)
			if err == nil && e.cache != nil {
				e.cache.SetJSON(ctx, cacheKey, embyLatestCacheValue{Items: out}, time.Duration(e.mediaCacheTTLSeconds())*time.Second)
			}
			return out, err
		}
		q = q.Where("media.library_id IN ?", e.mergedLibraryIDs(ctx, parentID))
	}
	rowLimit := limit * 4
	if rowLimit < 100 {
		rowLimit = 100
	}
	if rowLimit > 500 {
		rowLimit = 500
	}
	var rows []model.Media
	if err := q.Order(mediaReleaseOrderSQL(true)).Limit(rowLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	views, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil {
		return nil, err
	}
	views = e.collapseMediaVersionViews(ctx, views)
	if len(views) > limit {
		views = views[:limit]
	}
	out := e.payloadsForViews(ctx, views, userID)
	if e.cache != nil {
		e.cache.SetJSON(ctx, cacheKey, embyLatestCacheValue{Items: out}, time.Duration(e.mediaCacheTTLSeconds())*time.Second)
	}
	return out, nil
}

func (e *EmbyService) latestSeriesItemsForLibrary(ctx context.Context, userID, libraryID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("media.library_id IN ? AND (media.season_num > 0 OR media.episode_num > 0)", e.mergedLibraryIDs(ctx, libraryID))
	q = e.applyUserMediaVisibility(ctx, q, userID)
	var rows []model.Media
	if err := q.Order(mediaReleaseOrderSQL(true)).Limit(embySeriesGroupingLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	displayRows, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil {
		return nil, err
	}
	groups := e.seriesGroupsFromMedia(displayRows)
	sortSeriesGroups(groups, ItemsParams{SortBy: "premieredate", SortOrder: "Descending"})
	if len(groups) > limit {
		groups = groups[:limit]
	}
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, e.seriesPayload(ctx, group, userID))
	}
	return items, nil
}

// ResumeItems 列出有未完成播放进度的媒体。
func (e *EmbyService) ResumeItems(ctx context.Context, userID string, limit int) (map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var hist []model.PlaybackHistory
	q := e.repo.DB.WithContext(ctx).
		Table("playback_histories").
		Joins("JOIN media ON media.metadata_id = playback_histories.metadata_id AND media.deleted_at IS NULL")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	if err := q.Select("DISTINCT playback_histories.*").
		Where("playback_histories.user_id = ? AND playback_histories.completed = ? AND playback_histories.position_ms > 0", userID, false).
		Order("playback_histories.watched_at desc").Limit(limit).Scan(&hist).Error; err != nil {
		return nil, err
	}
	if len(hist) == 0 {
		return map[string]any{"Items": []any{}, "TotalRecordCount": 0}, nil
	}
	items := make([]map[string]any, 0, len(hist))
	for _, h := range hist {
		m, err := e.mediaViewForItemID(ctx, h.MetadataID, userID)
		if err != nil {
			return nil, err
		}
		if m != nil {
			items = append(items, e.itemPayload(ctx, m, false, h.PositionMs, false))
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

func (e *EmbyService) itemPayload(ctx context.Context, m *model.MediaView, fav bool, posMs int64, completeStreams bool) map[string]any {
	itemType := "Movie"
	name := m.Title
	parentID := m.LibraryID
	seriesID := m.SeriesID
	seriesName := ""
	seasonItemID := ""
	if e.mediaShouldBeEpisode(ctx, &m.Media) {
		itemType = "Episode"
		seriesID = e.seriesIDForMedia(m)
		seriesName = strings.TrimSpace(m.Title)
		if seriesName == "" {
			seriesName = e.seriesNameForMedia(m)
		}
		seasonItemID = m.SeasonID
		parentID = seasonItemID
		episodeTitle := strings.TrimSpace(m.EpisodeTitle)
		if episodeTitle != "" {
			name = episodeTitle
		} else if m.EpisodeNum > 0 {
			name = fmt.Sprintf("第 %d 集", m.EpisodeNum)
		}
	}
	imageTags := map[string]string{}
	backdropTags := []string{}
	primaryArtwork := e.mediaPrimaryArtwork(ctx, m)
	backdropArtwork := e.mediaBackdropArtwork(ctx, m)
	itemID := embyItemID(m)
	if primaryArtwork != "" {
		imageTags["Primary"] = itemID
	}
	if backdropArtwork != "" {
		backdropTags = append(backdropTags, itemID+"-bd")
	}

	runTimeTicks := int64(m.DurationSec) * 10_000_000
	durationMs := int64(m.DurationSec) * 1000
	played := posMs > 0 && durationMs > 0 && posMs >= durationMs*9/10
	pct := 0.0
	if durationMs > 0 {
		pct = float64(posMs) / float64(durationMs) * 100
	}
	container := embyMediaContainer(&m.Media)
	isLocalSTRM := localSTRMFileTarget(&m.Media) != ""
	isCloud := strings.TrimSpace(m.STRMURL) != "" && !isLocalSTRM
	playURL := e.embyMediaPlayURL(ctx, &m.Media, container, isCloud)

	item := map[string]any{
		"Id":                itemID,
		"Name":              name,
		"OriginalTitle":     m.OriginalName,
		"ServerId":          embyServerID,
		"Type":              itemType,
		"MediaType":         "Video",
		"IsFolder":          false,
		"ProductionYear":    m.Year,
		"ParentIndexNumber": m.SeasonNum,
		"IndexNumber":       m.EpisodeNum,
		"Overview":          m.Overview,
		"RunTimeTicks":      runTimeTicks,
		"CommunityRating":   m.Rating,
		"Container":         container,
		"Width":             m.Width,
		"Height":            m.Height,
		"DateCreated":       m.CreatedAt,
		"Path":              embyMediaSourcePath(&m.Media, playURL, isLocalSTRM, isCloud),
		"ParentId":          parentID,
		"SeasonId":          seasonItemID,
		"SeasonName":        seasonName(m.SeasonNum),
		"SeriesId":          seriesID,
		"SeriesName":        seriesName,
		"ImageTags":         imageTags,
		"BackdropImageTags": backdropTags,
		"Genres":            splitCSV(m.Genres),
		"ProviderIds": map[string]string{
			"Tmdb":    intToStr(m.TMDbID),
			"Bangumi": intToStr(m.BangumiID),
		},
		"UserData": map[string]any{
			"PlaybackPositionTicks": posMs * 10_000,
			"PlayCount":             0,
			"IsFavorite":            fav,
			"Played":                played,
			"PlayedPercentage":      pct,
		},
		"MediaSources": e.mediaSourcesForView(ctx, m, true, false, completeStreams),
	}
	if premiered, ok := embyPremiereDate(m.ReleaseDate); ok {
		item["PremiereDate"] = premiered
	}
	return item
}
