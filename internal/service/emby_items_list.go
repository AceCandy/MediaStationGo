package service

import (
	"context"
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
	if len(p.PersonIDs) > 0 {
		credits := e.repo.DB.WithContext(ctx).Table("metadata_items AS work").Select("work.id").
			Joins("LEFT JOIN metadata_items AS season ON season.id = work.parent_id AND season.kind = 'season'").
			Joins("JOIN metadata_credits AS credit ON credit.metadata_id = CASE WHEN work.kind = 'episode' THEN season.id ELSE work.id END").
			Where("credit.person_id = ANY(?)", &p.PersonIDs)
		q = q.Where("media.metadata_id IN (?)", credits)
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
	items := e.payloadsForViewsWithFields(ctx, views, p.UserID, p.Fields)
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

func (e *EmbyService) payloadsForMedia(ctx context.Context, rows []model.Media, userID string) ([]map[string]any, error) {
	views, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil {
		return nil, err
	}
	return e.payloadsForViews(ctx, views, userID), nil
}

type embyItemRelations struct {
	peopleByMetadataID      map[string][]model.EmbyPerson
	providerIDsByMetadataID map[string]map[string]string
	versionsByMetadataID    map[string][]model.MediaView
	partCountByGroupKey     map[string]int
	episodeByMediaID        map[string]bool
	fields                  embyListFields
}

type embyListFields struct {
	people       bool
	providerIDs  bool
	mediaSources bool
}

func newEmbyListFields(requested []string) embyListFields {
	if len(requested) == 0 {
		return embyListFields{people: true, providerIDs: true, mediaSources: true}
	}
	var fields embyListFields
	for _, field := range requested {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "people":
			fields.people = true
		case "providerids":
			fields.providerIDs = true
		case "mediasources", "mediastreams":
			fields.mediaSources = true
		}
	}
	return fields
}

func (e *EmbyService) payloadsForViews(ctx context.Context, views []model.MediaView, userID string) []map[string]any {
	return e.payloadsForViewsWithFields(ctx, views, userID, nil)
}

func (e *EmbyService) payloadsForViewsWithFields(ctx context.Context, views []model.MediaView, userID string, requestedFields []string) []map[string]any {
	views = e.collapseMediaVersionViews(ctx, views)
	metadataIDs := make([]string, 0, len(views))
	for i := range views {
		metadataIDs = append(metadataIDs, views[i].MetadataID)
	}
	userFavs, userPos, userCompleted := e.userDataForMetadataIDs(ctx, userID, metadataIDs)

	relations := e.itemRelationsForViews(ctx, views, userID, newEmbyListFields(requestedFields))
	items := make([]map[string]any, 0, len(views))
	for i := range views {
		m := &views[i]
		itemID := embyItemID(m)
		items = append(items, e.itemPayloadWithRelations(ctx, m, userID, userFavs[itemID], userPos[itemID], userCompleted[itemID], false, relations))
	}
	return items
}

func (e *EmbyService) itemRelationsForViews(ctx context.Context, views []model.MediaView, userID string, fields embyListFields) *embyItemRelations {
	relations := &embyItemRelations{
		peopleByMetadataID:      map[string][]model.EmbyPerson{},
		providerIDsByMetadataID: map[string]map[string]string{},
		versionsByMetadataID:    map[string][]model.MediaView{},
		partCountByGroupKey:     map[string]int{},
		episodeByMediaID:        map[string]bool{},
		fields:                  fields,
	}
	if e == nil || e.repo == nil || len(views) == 0 {
		return relations
	}
	metadataIDs := make([]string, 0, len(views))
	seen := make(map[string]struct{}, len(views))
	needsEpisodeLibraries := false
	for i := range views {
		if id := strings.TrimSpace(views[i].MetadataID); id != "" {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				metadataIDs = append(metadataIDs, id)
			}
		}
		if views[i].Media.SeasonNum > 0 || views[i].Media.EpisodeNum > 0 {
			needsEpisodeLibraries = true
		}
	}

	if fields.people && e.repo.Person != nil {
		if rows, err := e.repo.Person.ListCreditsWithPeopleByMetadataIDs(ctx, metadataIDs); err == nil {
			grouped := make(map[string][]model.MetadataCredit)
			for _, row := range rows {
				grouped[row.MetadataID] = append(grouped[row.MetadataID], row)
			}
			for id, credits := range grouped {
				relations.peopleByMetadataID[id] = embyPeopleFromCredits(credits)
			}
		}
	}
	if fields.providerIDs && e.repo.Metadata != nil {
		if rows, err := e.repo.Metadata.ListIdentifiersByMetadataIDs(ctx, metadataIDs); err == nil {
			grouped := make(map[string][]model.MetadataIdentifier)
			for _, row := range rows {
				grouped[row.MetadataID] = append(grouped[row.MetadataID], row)
			}
			for id, identifiers := range grouped {
				relations.providerIDsByMetadataID[id] = metadataProviderIDsFromIdentifiers(identifiers)
			}
		}
	}
	if e.repo.MediaView != nil {
		if rows, err := e.repo.MediaView.FindByMetadataIDs(ctx, metadataIDs, e.mediaQueryFilter(ctx, userID)); err == nil {
			for _, row := range rows {
				if row.PartGroupKey != "" && row.PartIndex > 0 {
					relations.partCountByGroupKey[row.PartGroupKey]++
				}
				if fields.mediaSources {
					relations.versionsByMetadataID[row.MetadataID] = append(relations.versionsByMetadataID[row.MetadataID], row)
				}
			}
		}
	}

	episodicLibraries := map[string]struct{}{}
	if needsEpisodeLibraries {
		for _, id := range e.episodicLibraryIDs(ctx) {
			episodicLibraries[id] = struct{}{}
		}
	}
	for i := range views {
		m := &views[i].Media
		if m.SeasonNum <= 0 && m.EpisodeNum <= 0 {
			continue
		}
		_, episodic := episodicLibraries[m.LibraryID]
		relations.episodeByMediaID[m.ID] = episodic || embyMediaPathLooksEpisodic(m.Path)
	}
	return relations
}

func (e *EmbyService) collapseMediaVersionViews(ctx context.Context, rows []model.MediaView) []model.MediaView {
	rows = collapseMediaPartViews(rows)
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
	if containsEmbyFilter(p.Filters, "IsFavorite") && strings.TrimSpace(p.UserID) == "" {
		return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": p.StartIndex}, nil
	}
	groups, total, err := e.seriesMetadataPage(ctx, q, p.UserID, p, p.StartIndex, p.Limit)
	if err != nil {
		return nil, err
	}
	items := e.seriesPayloadsWithFields(ctx, groups, p.UserID, p.Fields)
	for _, item := range items {
		item["ParentId"] = libraryID
	}
	return map[string]any{"Items": items, "TotalRecordCount": int(total), "StartIndex": p.StartIndex}, nil
}
