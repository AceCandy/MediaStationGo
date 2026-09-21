package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// searchTopLevelItems 各来源先检索候选，统一排序分页后只加载当前页详情。
func (e *EmbyService) searchTopLevelItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	query := []rune(strings.TrimSpace(p.SearchTerm))
	if len(query) == 2 && query[0] != '%' && query[1] == '%' {
		// 播放器单字符输入会追加百分号，此处去掉兼容后缀。
		p.SearchTerm = string(query[0])
	}
	if containsItemType(p.IncludeItemTypes, "Person") {
		return e.searchPersonAndMediaItems(ctx, p)
	}
	kinds := embySearchKinds(p.IncludeItemTypes)
	v := e.mediaVisibility(ctx, p.UserID)
	if len(kinds) == 0 || (v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0) {
		return emptyItemsEnvelope(p.StartIndex), nil
	}
	filter := repository.MetadataSearchFilter{
		MediaQueryFilter: e.mediaQueryFilter(ctx, p.UserID),
		Fields:           repository.MetadataSearchFieldsTitle, Kinds: kinds, PersonIDs: p.PersonIDs,
	}
	if p.ParentID != "" {
		library, err := e.repo.Library.FindByID(ctx, p.ParentID)
		if err != nil {
			return nil, err
		}
		if library == nil || (len(v.AllowedLibraryIDs) > 0 && !containsString(v.AllowedLibraryIDs, library.ID)) {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		filter.AllowedLibraryIDs = e.mergedLibraryIDs(ctx, library.ID)
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		filter.FavoriteUserID = p.UserID
	}
	if containsEmbyFilter(p.Filters, "IsResumable") {
		if strings.TrimSpace(p.UserID) == "" {
			return emptyItemsEnvelope(p.StartIndex), nil
		}
		filter.ResumableUserID = p.UserID
	}
	ids, _, err := e.repo.MediaView.SearchMetadataIDs(ctx, p.SearchTerm, 0, repository.MetadataSearchCandidateLimit, filter)
	if err != nil {
		return nil, err
	}
	candidates, err := e.repo.MediaView.SearchCandidateDetails(ctx, ids)
	if err != nil {
		return nil, err
	}
	if p.ParentID == "" {
		source, err := e.hongGuoSearchCandidates(ctx, p, filter)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, source...)
	}
	ranked, total := repository.RankMetadataSearchCandidatePage(p.SearchTerm, candidates, p.StartIndex, p.Limit)
	ids = make([]string, 0, len(ranked))
	for _, candidate := range ranked {
		ids = append(ids, candidate.ID)
	}
	items, err := e.globalItemPayloads(ctx, ids, p)
	if err != nil {
		return nil, err
	}
	if p.ParentID != "" {
		for _, item := range items {
			item["ParentId"] = p.ParentID
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

// hongGuoSearchCandidates 播放状态按当前用户实时查询，其余作品搜索使用独立索引。
func (e *EmbyService) hongGuoSearchCandidates(ctx context.Context, p ItemsParams, filter repository.MetadataSearchFilter) ([]repository.MetadataSearchCandidate, error) {
	if !containsEmbyFilter(p.Filters, "IsPlayed") && !containsEmbyFilter(p.Filters, "IsUnplayed") && !containsEmbyFilter(p.Filters, "IsResumable") {
		return e.repo.HongGuo.SearchCandidates(ctx, p.SearchTerm, filter)
	}
	var hasSource bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source = 'hongguo')").Scan(&hasSource).Error; err != nil {
		return nil, err
	} else if !hasSource {
		return nil, nil
	}
	q := e.hongGuoNodes(ctx, p.UserID, "").Where("LOWER(kind) IN ? AND POSITION(LOWER(?) IN LOWER(title)) > 0", filter.Kinds, p.SearchTerm)
	q = e.hongGuoPersonFilter(ctx, q, p.UserID, "", p.PersonIDs)
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		q = q.Where("favorite")
	}
	if containsEmbyFilter(p.Filters, "IsPlayed") {
		q = q.Where("played")
	}
	if containsEmbyFilter(p.Filters, "IsUnplayed") {
		q = q.Where("NOT played")
	}
	if containsEmbyFilter(p.Filters, "IsResumable") {
		q = q.Where("kind = 'Movie' AND position_ms > 0")
	}
	var nodes []hongGuoNode
	if err := q.Order("title, id").Limit(repository.MetadataSearchCandidateLimit).Scan(&nodes).Error; err != nil {
		return nil, err
	}
	candidates := make([]repository.MetadataSearchCandidate, 0, len(nodes))
	for _, node := range nodes {
		candidates = append(candidates, repository.MetadataSearchCandidate{ID: node.ID, Kind: strings.ToLower(node.Kind), Title: node.Title})
	}
	return candidates, nil
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
	people, err := e.Persons(ctx, ItemsParams{UserID: p.UserID, SearchTerm: p.SearchTerm, Filters: p.Filters, Limit: repository.MetadataSearchCandidateLimit})
	if err != nil {
		return nil, err
	}
	for _, person := range people["Items"].([]map[string]any) {
		id, _ := person["Id"].(string)
		name, _ := person["Name"].(string)
		originalName, _ := person["OriginalTitle"].(string)
		candidate := repository.MetadataSearchCandidate{
			Kind: "person", ID: id, Title: name, OriginalName: originalName,
		}
		candidates = append(candidates, candidate)
		payloadByKey[embySearchCandidateKey(candidate.Kind, candidate.ID)] = person
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
