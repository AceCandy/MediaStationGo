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
	if person, err := e.personItem(ctx, mediaID); err != nil {
		return nil, err
	} else if person != nil {
		return person, nil
	}
	if lib, err := e.repo.Library.FindByID(ctx, mediaID); err != nil {
		return nil, err
	} else if lib != nil {
		visibility := e.mediaVisibility(ctx, userID)
		if !e.libraryVisibleFromCachedVisibility(*lib, visibility) {
			return nil, nil
		}
		return e.libraryAsView(lib), nil
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
	return e.itemPayload(ctx, m, userID, fav, pos, true), nil
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
	views, _, err := e.metadataPage(ctx, q, userID, metadataOrderSQL(ItemsParams{SortBy: "datecreated", SortOrder: "Descending"}, false), 0, limit)
	if err != nil {
		return nil, err
	}
	out := e.payloadsForViews(ctx, views, userID)
	for _, item := range out {
		if item["Type"] == "Movie" {
			item["ParentId"] = parentID
		}
	}
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
	q = seriesScopeQuery(q)
	groups, _, err := e.seriesMetadataPage(ctx, q, userID, ItemsParams{SortBy: "datecreated", SortOrder: "Descending"}, 0, limit)
	if err != nil {
		return nil, err
	}
	items := e.seriesPayloadsWithFields(ctx, groups, userID, nil)
	for _, item := range items {
		item["ParentId"] = libraryID
	}
	return items, nil
}

// ResumeItems 列出有未完成播放进度的媒体。
func (e *EmbyService) ResumeItems(ctx context.Context, userID string, limit int) (map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	completed := false
	hist, err := e.repo.History.ListByUserFiltered(ctx, userID, limit, &completed, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	if len(hist) == 0 {
		return map[string]any{"Items": []any{}, "TotalRecordCount": 0}, nil
	}
	metadataIDs := make([]string, 0, len(hist))
	lastMediaByMetadata := make(map[string]string, len(hist))
	for i := range hist {
		metadataIDs = append(metadataIDs, hist[i].MetadataID)
		lastMediaByMetadata[hist[i].MetadataID] = hist[i].MediaID
	}
	versions, err := e.repo.MediaView.FindByMetadataIDs(ctx, metadataIDs, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	viewsByMetadata := make(map[string]model.MediaView, len(hist))
	for _, view := range versions {
		current, ok := viewsByMetadata[view.MetadataID]
		preferredID := lastMediaByMetadata[view.MetadataID]
		if !ok || view.ID == preferredID || (current.ID != preferredID && preferMediaVersion(view.Media, current.Media)) {
			viewsByMetadata[view.MetadataID] = view
		}
	}
	views := make([]model.MediaView, 0, len(hist))
	positions := make(map[string]int64, len(hist))
	for _, history := range hist {
		if view, ok := viewsByMetadata[history.MetadataID]; ok {
			views = append(views, view)
			positions[history.MetadataID] = history.PositionMs
		}
	}
	relations := e.itemRelationsForViews(ctx, views, userID, newEmbyListFields(nil))
	items := make([]map[string]any, 0, len(views))
	for i := range views {
		view := &views[i]
		items = append(items, e.itemPayloadWithRelations(ctx, view, userID, false, positions[view.MetadataID], false, relations))
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

func (e *EmbyService) itemPayload(ctx context.Context, m *model.MediaView, userID string, fav bool, posMs int64, completeStreams bool) map[string]any {
	return e.itemPayloadWithRelations(ctx, m, userID, fav, posMs, completeStreams, nil)
}

func (e *EmbyService) itemPayloadWithRelations(ctx context.Context, m *model.MediaView, userID string, fav bool, posMs int64, completeStreams bool, relations *embyItemRelations) map[string]any {
	var episode bool
	var people []model.EmbyPerson
	var providerIDs map[string]string
	var mediaSources []map[string]any
	if relations == nil {
		episode = e.mediaShouldBeEpisode(ctx, &m.Media)
		people = e.peopleForMetadata(ctx, m.MetadataID)
		providerIDs = e.metadataProviderIDs(ctx, m.MetadataID)
		mediaSources = e.mediaSourcesForView(ctx, m, userID, true, completeStreams)
	} else {
		episode = relations.episodeByMediaID[m.ID]
		if relations.fields.people {
			people = []model.EmbyPerson{}
			if loaded, ok := relations.peopleByMetadataID[m.MetadataID]; ok {
				people = loaded
			}
		}
		if relations.fields.providerIDs {
			providerIDs = map[string]string{}
			if loaded, ok := relations.providerIDsByMetadataID[m.MetadataID]; ok {
				providerIDs = loaded
			}
		}
		if relations.fields.mediaSources {
			siblings := []model.MediaView{*m}
			if loaded := relations.versionsByMetadataID[m.MetadataID]; len(loaded) > 0 {
				siblings = append([]model.MediaView(nil), loaded...)
				siblings = orderMediaVersionSiblings(siblings, m.ID)
			}
			mediaSources = e.mediaSourcesForViews(ctx, siblings, true, completeStreams)
		}
	}

	itemType := "Movie"
	name := m.Title
	parentID := m.LibraryID
	seriesID := m.SeriesID
	seriesName := ""
	seasonItemID := ""
	if episode {
		itemType = "Episode"
		seriesID = e.seriesIDForMedia(m)
		seriesName = e.seriesNameForMedia(m)
		seasonItemID = m.SeasonID
		parentID = seasonItemID
		name = strings.TrimSpace(m.Title)
		if name == "" && m.EpisodeNum > 0 {
			name = fmt.Sprintf("第 %d 集", m.EpisodeNum)
		}
	}
	imageTags := map[string]string{}
	backdropTags := []string{}
	primaryArtwork := mediaPrimaryArtworkForType(m, episode)
	backdropArtwork := mediaBackdropArtworkForType(m, episode)
	itemID := embyItemID(m)
	if primaryArtwork != "" {
		imageTags["Primary"] = itemID
	}
	if backdropArtwork != "" {
		backdropTags = append(backdropTags, itemID+"-bd")
	}

	runTimeTicks := int64(m.DurationSec) * 10_000_000
	durationMs := int64(m.DurationSec) * 1000
	played := playbackCompleted(posMs, durationMs)
	pct := 0.0
	if durationMs > 0 {
		pct = float64(posMs) / float64(durationMs) * 100
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
	}
	container := embyMediaContainer(&m.Media)
	isLocalSTRM := localSTRMFileTarget(&m.Media) != ""
	isRemote := strings.TrimSpace(m.STRMURL) != "" && !isLocalSTRM
	playURL := embyDirectStreamURL(m.ID, container)

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
		"DateCreated":       formatEmbyDateTime(m.CreatedAt),
		"Path":              embyMediaSourcePath(&m.Media, playURL, isLocalSTRM, isRemote),
		"ParentId":          parentID,
		"SeasonId":          seasonItemID,
		"SeasonName":        seasonName(m.SeasonNum),
		"SeriesId":          seriesID,
		"SeriesName":        seriesName,
		"ImageTags":         imageTags,
		"BackdropImageTags": backdropTags,
		"Genres":            splitCSV(m.Genres),
		"UserData": map[string]any{
			"PlaybackPositionTicks": posMs * 10_000,
			"PlayCount":             0,
			"IsFavorite":            fav,
			"Played":                played,
			"PlayedPercentage":      pct,
		},
	}
	if relations == nil || relations.fields.people {
		item["People"] = people
	}
	if relations == nil || relations.fields.providerIDs {
		item["ProviderIds"] = providerIDs
	}
	if relations == nil || relations.fields.mediaSources {
		item["MediaSources"] = mediaSources
	}
	if premiered, ok := embyPremiereDate(m.ReleaseDate); ok {
		item["PremiereDate"] = premiered
	}
	return item
}
