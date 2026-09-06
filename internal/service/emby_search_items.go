package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// searchTopLevelItems handles every non-empty Emby SearchTerm before the
// Series -> Season -> Episode browse branches can consume it.
func (e *EmbyService) searchTopLevelItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	searchTerm := []rune(strings.TrimSpace(p.SearchTerm))
	if len(searchTerm) == 2 && searchTerm[0] != '%' && searchTerm[1] == '%' {
		// Yamby/Emby 播放器只输入一个字符时会自动追加 `%`，此处去掉通配后缀再搜索。
		p.SearchTerm = string(searchTerm[0])
	}
	if containsItemType(p.IncludeItemTypes, "Person") {
		return e.searchPersonAndMediaItems(ctx, p)
	}
	kinds := embySearchKinds(p.IncludeItemTypes)
	if len(kinds) == 0 {
		return emptyItemsEnvelope(p.StartIndex), nil
	}
	visibility := e.mediaVisibility(ctx, p.UserID)
	if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
		return emptyItemsEnvelope(p.StartIndex), nil
	}
	viewFilter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	if p.ParentID != "" {
		library, err := e.repo.Library.FindByID(ctx, p.ParentID)
		if err != nil {
			return nil, err
		}
		if library == nil {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		if len(viewFilter.AllowedLibraryIDs) > 0 && !containsString(viewFilter.AllowedLibraryIDs, library.ID) {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		viewFilter.AllowedLibraryIDs = e.mergedLibraryIDs(ctx, library.ID)
	}
	searchFilter := repository.MetadataSearchFilter{
		MediaQueryFilter:  viewFilter,
		Fields:            repository.MetadataSearchFieldsTitle,
		Kinds:             kinds,
		PersonIDs:         p.PersonIDs,
		LibraryRestricted: visibility.LibraryRestricted || p.ParentID != "",
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		searchFilter.FavoriteUserID = p.UserID
	}
	if containsEmbyFilter(p.Filters, "IsResumable") {
		if strings.TrimSpace(p.UserID) == "" {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		searchFilter.ResumableUserID = p.UserID
	}
	ids, total, err := e.repo.MediaView.SearchMetadataIDs(ctx, p.SearchTerm, p.StartIndex, p.Limit, searchFilter)
	if err != nil {
		return nil, err
	}
	representatives, err := e.repo.MediaView.FindMetadataSearchRepresentatives(ctx, ids, viewFilter)
	if err != nil {
		return nil, err
	}
	representativeByID := make(map[string]model.MediaView, len(representatives))
	for _, view := range representatives {
		representativeByID[view.MetadataID] = view
	}

	movieViews := make([]model.MediaView, 0, len(representatives))
	seriesIDs := make([]string, 0, len(representatives))
	for _, id := range ids {
		view, visible := representativeByID[id]
		if !visible {
			continue
		}
		if view.MetadataKind == model.MetadataKindSeries {
			seriesIDs = append(seriesIDs, id)
		} else {
			movieViews = append(movieViews, view)
		}
	}
	payloadByID := make(map[string]map[string]any, len(representatives))
	for _, item := range e.payloadsForViewsWithFields(ctx, movieViews, p.UserID, p.Fields) {
		if id, _ := item["Id"].(string); id != "" {
			payloadByID[id] = item
		}
	}
	if len(seriesIDs) > 0 {
		q := seriesScopeQuery(e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), p.UserID))
		groups, loadErr := e.seriesSummaries(ctx, q, seriesIDs)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, item := range e.seriesPayloadsWithFields(ctx, groups, p.UserID, p.Fields) {
			if id, _ := item["Id"].(string); id != "" {
				payloadByID[id] = item
			}
		}
	}
	items := make([]map[string]any, 0, len(payloadByID))
	for _, id := range ids {
		if item, ok := payloadByID[id]; ok {
			if p.ParentID != "" {
				item["ParentId"] = p.ParentID
			}
			items = append(items, item)
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

func (e *EmbyService) searchPersonAndMediaItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	candidates := make([]repository.MetadataSearchCandidate, 0, repository.MetadataSearchCandidateLimit*2)
	payloadByKey := make(map[string]map[string]any, repository.MetadataSearchCandidateLimit*2)
	if kinds := embySearchKinds(p.IncludeItemTypes); len(kinds) > 0 {
		mediaParams := p
		mediaParams.IncludeItemTypes = kinds
		mediaParams.StartIndex = 0
		mediaParams.Limit = repository.MetadataSearchCandidateLimit
		result, err := e.searchTopLevelItems(ctx, mediaParams)
		if err != nil {
			return nil, err
		}
		for _, item := range result["Items"].([]map[string]any) {
			id, _ := item["Id"].(string)
			name, _ := item["Name"].(string)
			originalName, _ := item["OriginalTitle"].(string)
			kind, _ := item["Type"].(string)
			year, _ := item["ProductionYear"].(int)
			candidate := repository.MetadataSearchCandidate{
				Kind: strings.ToLower(kind), ID: id, Title: name, OriginalName: originalName, Year: year,
			}
			candidates = append(candidates, candidate)
			payloadByKey[embySearchCandidateKey(candidate.Kind, id)] = item
		}
	}
	people, _, err := e.repo.Person.List(ctx, p.SearchTerm, nil, 0, repository.MetadataSearchCandidateLimit)
	if err != nil {
		return nil, err
	}
	for _, person := range people {
		candidate := repository.MetadataSearchCandidate{
			Kind: "person", ID: person.ID, Title: person.Name, OriginalName: person.OriginalName,
		}
		candidates = append(candidates, candidate)
		payloadByKey[embySearchCandidateKey(candidate.Kind, candidate.ID)] = personPayload(person)
	}
	ranked, total := repository.RankMetadataSearchCandidatePage(p.SearchTerm, candidates, p.StartIndex, p.Limit)
	items := make([]map[string]any, 0, len(ranked))
	for _, candidate := range ranked {
		if item, ok := payloadByKey[embySearchCandidateKey(candidate.Kind, candidate.ID)]; ok {
			items = append(items, item)
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

func embySearchCandidateKey(kind, id string) string {
	return strings.ToLower(kind) + "\x00" + id
}

func embySearchKinds(includeItemTypes []string) []string {
	if len(includeItemTypes) == 0 {
		return []string{model.MetadataKindMovie, model.MetadataKindSeries}
	}
	kinds := make([]string, 0, 2)
	if containsItemType(includeItemTypes, "Movie") {
		kinds = append(kinds, model.MetadataKindMovie)
	}
	if containsItemType(includeItemTypes, "Series") {
		kinds = append(kinds, model.MetadataKindSeries)
	}
	return kinds
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
