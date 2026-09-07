package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

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
		metadata, err := e.repo.Metadata.FindByID(ctx, mediaID)
		if err != nil {
			return nil, err
		}
		if metadata != nil && (metadata.Kind == model.MetadataKindSeries || metadata.Kind == model.MetadataKindSeason) {
			return e.containerDetail(ctx, metadata, userID)
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
	fav, pos, completed := e.userDataForTarget(ctx, userID, target)
	return e.itemPayloadWithRelations(ctx, m, userID, fav, pos, completed, true, nil), nil
}

// AdditionalParts 返回当前播放版本除首 Part 外的物理文件。
func (e *EmbyService) AdditionalParts(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	m, _, err := e.playableMediaWithSiblings(ctx, mediaID, userID)
	if err != nil || m == nil {
		return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, err
	}
	parts, err := e.mediaPartViews(ctx, m, userID)
	if err != nil {
		return nil, err
	}
	if len(parts) < 2 {
		return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, nil
	}
	parts = parts[1:]
	favorites, _, _ := e.userDataForMetadataIDs(ctx, userID, []string{m.MetadataID})
	positions := map[string]int64{}
	completed := map[string]bool{}
	if strings.TrimSpace(userID) != "" {
		ids := make([]string, 0, len(parts))
		for i := range parts {
			ids = append(ids, parts[i].ID)
		}
		var histories []model.PlaybackHistory
		if err := e.repo.DB.WithContext(ctx).Where("user_id = ? AND media_id IN ?", userID, ids).Find(&histories).Error; err == nil {
			for _, history := range histories {
				positions[history.MediaID] = history.PositionMs
				completed[history.MediaID] = history.Completed
			}
		}
	}
	relations := e.itemRelationsForViews(ctx, parts, userID, embyListFields{people: true, providerIDs: true})
	items := make([]map[string]any, 0, len(parts))
	for i := range parts {
		part := &parts[i]
		itemID := embyItemID(part)
		item := e.itemPayloadWithRelations(ctx, part, userID, favorites[itemID], positions[part.ID], completed[part.ID], false, relations)
		item["Id"] = part.ID
		item["MediaSources"] = e.mediaSourcesForViews(ctx, []model.MediaView{*part}, false, true)
		delete(item, "PartCount")
		items = append(items, item)
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

// LatestItems 最近添加，全库或指定库，并按调用方指定的播放状态过滤。
func (e *EmbyService) LatestItems(ctx context.Context, userID, parentID string, limit int, isPlayed bool) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{})
	q = e.applyUserMediaVisibility(ctx, q, userID)
	q = e.applyLatestPlayedFilter(ctx, q, userID, isPlayed)
	if parentID != "" {
		if episodic, err := e.libraryIsEpisodic(ctx, parentID); err == nil && episodic {
			return e.latestSeriesItemsForLibrary(ctx, userID, parentID, limit, isPlayed)
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
	return out, nil
}

func (e *EmbyService) applyLatestPlayedFilter(ctx context.Context, q *gorm.DB, userID string, isPlayed bool) *gorm.DB {
	completed := e.repo.DB.WithContext(ctx).Model(&model.PlaybackHistory{}).
		Select("1").
		Where("playback_histories.user_id = ?", userID).
		Where("playback_histories.metadata_id = media.metadata_id").
		Where("playback_histories.completed = ?", true)
	if isPlayed {
		return q.Where("EXISTS (?)", completed)
	}
	return q.Where("NOT EXISTS (?)", completed)
}

func (e *EmbyService) latestSeriesItemsForLibrary(ctx context.Context, userID, libraryID string, limit int, isPlayed bool) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("media.library_id IN ? AND (media.season_num > 0 OR media.episode_num > 0)", e.mergedLibraryIDs(ctx, libraryID))
	q = e.applyUserMediaVisibility(ctx, q, userID)
	q = e.applyLatestPlayedFilter(ctx, q, userID, isPlayed)
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
	preferredPartGroupByMetadata := make(map[string]string, len(hist))
	for _, view := range versions {
		if view.ID == lastMediaByMetadata[view.MetadataID] {
			preferredPartGroupByMetadata[view.MetadataID] = view.PartGroupKey
		}
	}
	versions = collapseMediaPartViews(versions)
	viewsByMetadata := make(map[string]model.MediaView, len(hist))
	for _, view := range versions {
		current, ok := viewsByMetadata[view.MetadataID]
		preferredID := lastMediaByMetadata[view.MetadataID]
		preferredPartGroup := preferredPartGroupByMetadata[view.MetadataID]
		viewIsPreferred := view.ID == preferredID || (preferredPartGroup != "" && view.PartGroupKey == preferredPartGroup)
		currentIsPreferred := current.ID == preferredID || (preferredPartGroup != "" && current.PartGroupKey == preferredPartGroup)
		if !ok || viewIsPreferred || (!currentIsPreferred && preferMediaVersion(view.Media, current.Media)) {
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
		items = append(items, e.itemPayloadWithRelations(ctx, view, userID, false, positions[view.MetadataID], false, false, relations))
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

func (e *EmbyService) itemPayload(ctx context.Context, m *model.MediaView, userID string, fav bool, posMs int64, completeStreams bool) map[string]any {
	return e.itemPayloadWithRelations(ctx, m, userID, fav, posMs, false, completeStreams, nil)
}

func (e *EmbyService) itemPayloadWithRelations(ctx context.Context, m *model.MediaView, userID string, fav bool, posMs int64, completed, completeStreams bool, relations *embyItemRelations) map[string]any {
	var episode bool
	var people []model.EmbyPerson
	var providerIDs map[string]string
	var mediaSources []map[string]any
	partCount := 0
	if relations == nil {
		episode = e.mediaShouldBeEpisode(ctx, &m.Media)
		people = e.peopleForMetadata(ctx, m.MetadataID)
		providerIDs = e.metadataProviderIDs(ctx, m.MetadataID)
		mediaSources = e.mediaSourcesForView(ctx, m, userID, true, completeStreams)
		if parts, err := e.mediaPartViews(ctx, m, userID); err == nil {
			partCount = len(parts)
		}
	} else {
		episode = relations.episodeByMediaID[m.ID]
		partCount = relations.partCountByGroupKey[m.PartGroupKey]
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

	durationMs := m.ProbeDurationMS
	runTimeTicks := durationMs * 10_000
	played := completed || playbackCompleted(posMs, durationMs)
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
	container := embyMediaContainer(&m.Media, m.ProbeContainer)
	if completed {
		pct = 100
	}
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
	if partCount > 1 {
		item["PartCount"] = partCount
	}
	if premiered, ok := embyPremiereDate(m.ReleaseDate); ok {
		item["PremiereDate"] = premiered
	}
	return item
}
