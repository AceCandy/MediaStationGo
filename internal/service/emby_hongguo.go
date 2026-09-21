package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResumeItems 对两套独立状态各取有界最新候选，再按观看时间选全局前 N 项。
func (e *EmbyService) ResumeItems(ctx context.Context, userID string, limit int) (map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if has, err := e.repo.NFO.HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		result, _, err := e.hongGuoGlobalItems(ctx, ItemsParams{UserID: userID, Recursive: true, Limit: limit, Filters: []string{"IsResumable"}, SortBy: "DatePlayed", SortOrder: "Descending"})
		return result, err
	}
	v := e.mediaVisibility(ctx, userID)
	if v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0 {
		return emptyItemsEnvelope(0), nil
	}
	legacy, err := e.legacyResumeItems(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	var hasHongGuo bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source = 'hongguo')").Scan(&hasHongGuo).Error; err != nil {
		return nil, err
	}
	if !hasHongGuo || userID == "" {
		return legacy, nil
	}
	cards, _, err := e.repo.HongGuo.UserCards(ctx, userID, "continue", 1, limit, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	items, _ := legacy["Items"].([]map[string]any)
	ids := make([]string, len(cards))
	for i, card := range cards {
		ids[i] = card.MediaID
	}
	sourceIDs := make([]string, 0, len(cards))
	for _, card := range cards {
		sourceIDs = append(sourceIDs, card.SourceID)
	}
	people, err := e.hongGuoPeople(ctx, sourceIDs)
	if err != nil {
		return nil, err
	}
	views, err := e.repo.MediaView.FindByIDs(ctx, ids, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	byID := map[string]model.MediaView{}
	relations := &embyItemRelations{fields: newEmbyListFields(nil), versionsByMetadataID: map[string][]model.MediaView{}}
	relations.peopleByMetadataID = people
	itemIDs := make([]string, 0, len(views))
	for _, view := range views {
		byID[view.ID] = view
		itemIDs = append(itemIDs, view.CatalogItemID)
	}
	versions, err := e.repo.MediaView.HongGuoItemsViews(ctx, itemIDs, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	for _, view := range versions {
		relations.versionsByMetadataID[view.CatalogItemID] = append(relations.versionsByMetadataID[view.CatalogItemID], view)
	}
	for _, card := range cards {
		view, ok := byID[card.MediaID]
		if !ok {
			continue
		}
		relations.versionsByMetadataID[view.CatalogItemID] = orderMediaVersionSiblings(relations.versionsByMetadataID[view.CatalogItemID], card.MediaID)
		item := e.itemPayloadWithRelations(ctx, &view, userID, false, card.PositionMs, card.Completed, false, relations)
		item["UserData"].(map[string]any)["LastPlayedDate"] = formatEmbyDateTime(card.UpdatedAt)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := items[i]["UserData"].(map[string]any)["LastPlayedDate"].(string)
		right, _ := items[j]["UserData"].(map[string]any)["LastPlayedDate"].(string)
		if left == right {
			return items[i]["Id"].(string) < items[j]["Id"].(string)
		}
		leftTime, _ := time.Parse(time.RFC3339Nano, left)
		rightTime, _ := time.Parse(time.RFC3339Nano, right)
		return leftTime.After(rightTime)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

// hongGuoNode 是文件可见性过滤后的逻辑目录，不写入旧资料表。
type hongGuoNode struct {
	ID            string
	Kind          string
	Title         string
	ParentID      string
	MediaID       string
	SeasonNumber  int
	EpisodeNumber int
	CreatedAt     time.Time
	LatestAt      time.Time
	PlayedAt      time.Time
	Favorite      bool
	Played        bool
	ArtworkID     string
	PositionMs    int64
	SourceID      string
	Overview      string
	Tags          string
	Rating        float32
	EpisodeCount  int
}

func (e *EmbyService) hongGuoNodes(ctx context.Context, userID, libraryID string) *gorm.DB {
	v := e.mediaVisibility(ctx, userID)
	files := e.repo.DB.WithContext(ctx).Table("media AS m").Select("m.*").Where("m.catalog_source = ?", model.TaskSystemHongGuo)
	if libraryID != "" {
		files = files.Where("m.library_id = ?", libraryID)
	}
	if v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0 {
		files = files.Where("FALSE")
	}
	if len(v.AllowedLibraryIDs) > 0 {
		files = files.Where("m.library_id = ANY(?)", &v.AllowedLibraryIDs)
	}
	if len(v.HiddenLibraryIDs) > 0 {
		files = files.Where("m.library_id <> ALL(?)", &v.HiddenLibraryIDs)
	}
	// 同一文件贡献整剧、季、集节点；先过滤文件，再按逻辑身份聚合多版本。
	return e.repo.DB.WithContext(ctx).Table(`(?) AS nodes`, e.repo.DB.Raw(`
SELECT n.id, n.resume_key, n.kind, n.title, n.parent_id, n.season_number, n.episode_number,
 MIN(m.id) AS media_id, MIN(m.created_at) AS created_at, MAX(m.created_at) AS latest_at,
 MAX(s.watched_at) AS played_at,
 BOOL_OR(COALESCE(f.favorite,FALSE)) AS favorite,
 BOOL_AND(COALESCE(s.completed,FALSE)) AS played, MAX(COALESCE(s.position_ms,0)) AS position_ms,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN '' ELSE MIN(w.source_id) END AS source_id,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN '' ELSE MIN(w.overview) END AS overview,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN '[]' ELSE MIN(w.tags) END AS tags,
 CASE WHEN n.id LIKE 'hg-group-%%' THEN 0 ELSE MAX(w.rating) END AS rating,
 COUNT(DISTINCT ep.id) AS episode_count,
 CASE WHEN n.kind = 'Episode' THEN '' ELSE COALESCE((ARRAY_AGG(a.id ORDER BY NULLIF(w.season_index,0) NULLS LAST,w.id) FILTER (WHERE a.id IS NOT NULL))[1],'') END AS artwork_id
FROM (?) AS m
JOIN hongguo_media_bindings b ON b.media_id = m.id
JOIN hongguo_works w ON w.id = b.work_id
LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id
`+repository.HongGuoAlbumJoin+`
LEFT JOIN hongguo_artworks a ON a.work_id = w.id AND a.local_key <> ''
LEFT JOIN (?) s ON s.source_id = w.source_id AND s.episode_number = COALESCE(ep.number,1)
LEFT JOIN hongguo_user_states f ON f.user_id = ? AND f.source_id = w.source_id AND f.episode_number = 0
CROSS JOIN LATERAL (VALUES
	(CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END,
	 CASE WHEN w.kind = 'movie' THEN 'Movie' ELSE 'Series' END,
	 COALESCE(g.title,w.title), ''::text, 0, 0, CASE WHEN g.id IS NOT NULL THEN 'hongguo:group:' || g.id ELSE 'hongguo:work:' || w.source_id END),
	(CASE WHEN w.kind = 'series' THEN 'hg-season-' || w.id END, 'Season', w.title,
	 CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END, CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END, 0, CASE WHEN g.id IS NOT NULL THEN 'hongguo:group:' || g.id ELSE 'hongguo:work:' || w.source_id END),
	(CASE WHEN w.kind = 'series' AND ep.id IS NOT NULL THEN 'hg-episode-' || ep.id END, 'Episode', '第' || ep.number || '集', 'hg-season-' || w.id, CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END, ep.number,
	 CASE WHEN g.id IS NOT NULL THEN 'hongguo:group:' || g.id ELSE 'hongguo:work:' || w.source_id END)
) AS n(id,kind,title,parent_id,season_number,episode_number,resume_key)
WHERE n.id IS NOT NULL
GROUP BY n.id,n.resume_key,n.kind,n.title,n.parent_id,n.season_number,n.episode_number`, files, repository.PlaybackStates(ctx, e.repo.DB, "hongguo", userID, e.mediaQueryFilter(ctx, userID)), userID))
}

// LatestItems 使用可见文件的入库时间合并最新项，各来源只读取所需的前 N 个候选。
func (e *EmbyService) LatestItems(ctx context.Context, userID, parentID string, limit int, isPlayed bool, fields ...string) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if has, err := e.repo.NFO.HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		filter := "IsUnplayed"
		if isPlayed {
			filter = "IsPlayed"
		}
		params := ItemsParams{UserID: userID, ParentID: parentID, Limit: limit, Fields: fields, Filters: []string{filter}, SortBy: "DateCreated", SortOrder: "Descending"}
		if parentID == "" {
			params.Recursive = true
			result, _, err := e.hongGuoGlobalItems(ctx, params)
			if err != nil {
				return nil, err
			}
			items, _ := result["Items"].([]map[string]any)
			return items, nil
		}
		if result, handled, err := e.hongGuoHierarchyItems(ctx, params); handled {
			if err != nil {
				return nil, err
			}
			items, _ := result["Items"].([]map[string]any)
			return items, nil
		}
	}
	if parentID != "" {
		library, err := e.repo.Library.FindByID(ctx, parentID)
		if err != nil {
			return nil, err
		}
		if library == nil || library.Type != model.LibraryTypeHongGuo {
			return e.legacyLatestItems(ctx, userID, parentID, limit, isPlayed, fields...)
		}
	}
	var hasSource bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source = 'hongguo')").Scan(&hasSource).Error; err != nil {
		return nil, err
	}
	if !hasSource {
		return e.legacyLatestItems(ctx, userID, parentID, limit, isPlayed, fields...)
	}
	items := []map[string]any{}
	dates := map[string]time.Time{}
	if parentID == "" {
		legacy, err := e.legacyLatestItems(ctx, userID, parentID, limit, isPlayed, fields...)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for _, item := range legacy {
			if id, ok := item["Id"].(string); ok {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			var rows []struct {
				MetadataID string
				LatestAt   time.Time
			}
			q := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), userID)
			if err := q.Where("media.metadata_id IN ?", ids).Select("media.metadata_id, MAX(media.created_at) AS latest_at").Group("media.metadata_id").Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, row := range rows {
				dates[row.MetadataID] = row.LatestAt
			}
		}
		items = append(items, legacy...)
	}
	q := e.hongGuoNodes(ctx, userID, parentID).Where("played = ?", isPlayed)
	if parentID == "" {
		q = q.Where("kind IN ('Movie','Episode')")
	} else {
		q = q.Where("parent_id = ''")
	}
	var nodes []hongGuoNode
	if err := q.Order("latest_at DESC, id").Limit(limit).Scan(&nodes).Error; err != nil {
		return nil, err
	}
	sourceItems, err := e.hongGuoNodePayloads(ctx, nodes, userID, fields)
	if err != nil {
		return nil, err
	}
	items = append(items, sourceItems...)
	for _, node := range nodes {
		dates[node.ID] = node.LatestAt
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i]["Id"].(string), items[j]["Id"].(string)
		if dates[left].Equal(dates[right]) {
			return left < right
		}
		return dates[left].After(dates[right])
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (e *EmbyService) hongGuoHierarchyItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	libraryID := ""
	local := strings.HasPrefix(p.ParentID, "nfo-")
	if !local && !strings.HasPrefix(p.ParentID, "hg-") {
		if p.ParentID == "" {
			return nil, false, nil
		}
		lib, err := e.repo.Library.FindByID(ctx, p.ParentID)
		if err != nil {
			return nil, true, err
		}
		if lib == nil || (lib.Type != model.LibraryTypeHongGuo && !libraryUsesNFOOnly(lib)) {
			return nil, false, nil
		}
		libraryID = lib.ID
		local = libraryUsesNFOOnly(lib)
	}
	nodesQuery := e.hongGuoNodes
	if local {
		nodesQuery = e.nfoNodes
	}
	q := nodesQuery(ctx, p.UserID, libraryID)
	if libraryID != "" {
		if !p.Recursive {
			q = q.Where("parent_id = ''")
		}
	} else if p.Recursive || containsItemType(p.IncludeItemTypes, "Episode") {
		if local {
			seasons := nodesQuery(ctx, p.UserID, "").Select("id").Where("parent_id = ? AND kind = 'Season'", p.ParentID)
			q = q.Where("(parent_id = ? OR parent_id IN (?)) AND kind = 'Episode'", p.ParentID, seasons)
		} else if strings.HasPrefix(p.ParentID, "hg-season-") {
			q = q.Where("parent_id = ?", p.ParentID)
		} else {
			seasons := e.hongGuoNodes(ctx, p.UserID, "").Select("id").Where("parent_id = ? AND kind = 'Season'", p.ParentID)
			q = q.Where("parent_id IN (?) AND kind = 'Episode'", seasons)
		}
	} else {
		q = q.Where("parent_id = ?", p.ParentID)
	}
	if len(p.IncludeItemTypes) > 0 && !containsOnlyFolderItemTypes(p.IncludeItemTypes) {
		q = q.Where("LOWER(kind) IN ?", lowerStrings(p.IncludeItemTypes))
	}
	if p.SearchTerm != "" {
		q = q.Where("POSITION(LOWER(?) IN LOWER(title)) > 0", strings.TrimSpace(p.SearchTerm))
	}
	if local && len(p.PersonIDs) > 0 {
		q = q.Where("FALSE")
	} else if !local {
		q = e.hongGuoPersonFilter(ctx, q, p.UserID, libraryID, p.PersonIDs)
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		q = q.Where("favorite AND kind IN ('Movie','Series')")
	}
	if containsEmbyFilter(p.Filters, "IsPlayed") {
		q = q.Where("played")
	}
	if containsEmbyFilter(p.Filters, "IsUnplayed") {
		q = q.Where("NOT played")
	}
	resumeFilter := containsEmbyFilter(p.Filters, "IsResumable")
	if resumeFilter {
		q = q.Where("position_ms > 0 AND kind IN ('Movie','Episode')")
		q = e.repo.DB.WithContext(ctx).Table("(?) AS grouped_resume", q.Select("DISTINCT ON (resume_key) *").Order("resume_key, played_at DESC, id DESC"))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, true, err
	}
	order := "title"
	if strings.Contains(strings.ToLower(p.SortBy), "datecreated") {
		order = "created_at"
	} else if resumeFilter && strings.Contains(strings.ToLower(p.SortBy), "dateplayed") {
		order = "played_at"
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		order += " DESC"
	}
	var nodes []hongGuoNode
	if !resumeFilter {
		q = q.Order("season_number, episode_number")
	}
	if err := q.Order(order).Order("id").Limit(p.Limit).Offset(p.StartIndex).Scan(&nodes).Error; err != nil {
		return nil, true, err
	}
	payloads := e.hongGuoNodePayloads
	if local {
		payloads = e.nfoNodePayloads
	}
	items, err := payloads(ctx, nodes, p.UserID, p.Fields)
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, err
}

// hongGuoNodePayloads 只加载当前页的文件版本，列表、搜索和最新添加共用此边界。
func (e *EmbyService) hongGuoNodePayloads(ctx context.Context, nodes []hongGuoNode, userID string, fields []string) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(nodes))
	itemIDs := []string{}
	sourceIDs := []string{}
	for _, node := range nodes {
		if node.SourceID != "" {
			sourceIDs = append(sourceIDs, node.SourceID)
		}
		if node.Kind == "Movie" || node.Kind == "Episode" {
			itemIDs = append(itemIDs, node.ID)
		}
	}
	views, err := e.repo.MediaView.HongGuoItemsViews(ctx, itemIDs, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	relations := &embyItemRelations{fields: newEmbyListFields(fields), versionsByMetadataID: map[string][]model.MediaView{}}
	if relations.fields.people {
		relations.peopleByMetadataID, err = e.hongGuoPeople(ctx, sourceIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, view := range views {
		relations.versionsByMetadataID[view.CatalogItemID] = append(relations.versionsByMetadataID[view.CatalogItemID], view)
	}
	for _, node := range nodes {
		if node.Kind == "Movie" || node.Kind == "Episode" {
			versions := relations.versionsByMetadataID[node.ID]
			if len(versions) == 0 {
				continue
			}
			versions = orderMediaVersionSiblings(versions, node.MediaID)
			relations.versionsByMetadataID[node.ID] = versions
			items = append(items, e.itemPayloadWithRelations(ctx, &versions[0], userID, node.Kind == "Movie" && node.Favorite, node.PositionMs, node.Played, false, relations))
			continue
		}
		item, err := e.hongGuoNodePayload(ctx, node, userID)
		if err != nil {
			return nil, err
		}
		if item != nil {
			if !relations.fields.providerIDs {
				delete(item, "ProviderIds")
			}
			if relations.fields.people {
				item["People"] = []model.EmbyPerson{}
				if people := relations.peopleByMetadataID[node.SourceID]; people != nil {
					item["People"] = people
				}
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func (e *EmbyService) hongGuoNodePayload(ctx context.Context, node hongGuoNode, userID string) (map[string]any, error) {
	if node.Kind == "Movie" || node.Kind == "Episode" {
		return e.Item(ctx, node.MediaID, userID)
	}
	images := map[string]string{}
	if node.ArtworkID != "" {
		images["Primary"] = node.ArtworkID
	}
	genres := []string{}
	_ = json.Unmarshal([]byte(node.Tags), &genres)
	providers := map[string]string{}
	if node.SourceID != "" {
		providers["HongGuoDB"] = node.SourceID
	}
	return map[string]any{
		"Id": node.ID, "Name": node.Title, "Type": node.Kind, "ServerId": embyServerID,
		"IsFolder": true, "ParentId": node.ParentID, "IndexNumber": node.SeasonNumber,
		"DateCreated": formatEmbyDateTime(node.CreatedAt), "ImageTags": images,
		"Overview": node.Overview, "Genres": genres, "CommunityRating": node.Rating,
		"ProviderIds": providers, "RecursiveItemCount": node.EpisodeCount,
		"UserData": map[string]any{"IsFavorite": node.Kind == "Series" && node.Favorite, "Played": node.Played, "PlaybackPositionTicks": 0},
	}, nil
}

func lowerStrings(values []string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = strings.ToLower(value)
	}
	return result
}

// hongGuoContainerMutation 仅更改当前可见且有文件的成员，状态仍使用源作品及源集号。
func (e *EmbyService) hongGuoContainerMutation(ctx context.Context, userID, id string, favorite *bool, played *bool) (bool, error) {
	if !strings.HasPrefix(id, "hg-") {
		return false, nil
	}
	var nodes []hongGuoNode
	if err := e.hongGuoNodes(ctx, userID, "").Where("id = ?", id).Limit(1).Scan(&nodes).Error; err != nil {
		return true, err
	}
	if len(nodes) == 0 {
		return true, errors.New("media not found")
	}
	node := nodes[0]
	if node.Kind == "Movie" || node.Kind == "Episode" {
		return false, nil
	}
	if favorite != nil && node.Kind != "Series" {
		return true, repository.ErrFavoriteUnsupportedType
	}
	seasons := e.hongGuoNodes(ctx, userID, "").Select("id").Where("kind = 'Season'")
	if node.Kind == "Season" {
		seasons = seasons.Where("id = ?", id)
	} else {
		seasons = seasons.Where("parent_id = ?", id)
	}
	if favorite != nil {
		var sourceIDs []string
		if err := e.repo.DB.WithContext(ctx).Model(&model.HongGuoWork{}).Where("'hg-season-' || id IN (?)", seasons).Pluck("source_id", &sourceIDs).Error; err != nil {
			return true, err
		}
		states := make([]model.HongGuoUserState, 0, len(sourceIDs))
		for _, sourceID := range sourceIDs {
			states = append(states, model.HongGuoUserState{UserID: userID, SourceID: sourceID, Favorite: *favorite})
		}
		if len(states) == 0 {
			return true, errors.New("media not found")
		}
		err := e.repo.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: clause.AssignmentColumns([]string{"favorite", "updated_at"})}).CreateInBatches(states, 500).Error
		return true, err
	}
	if played == nil {
		return true, errors.New("missing mutation")
	}
	children := e.hongGuoNodes(ctx, userID, "").Where("kind = 'Episode' AND parent_id IN (?)", seasons)
	err := e.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repos := repository.New(tx)
		after := ""
		for {
			var batch []hongGuoNode
			if err := tx.Table("(?) AS children", children).Where("id > ?", after).Order("id").Limit(100).Scan(&batch).Error; err != nil {
				return err
			}
			if len(batch) == 0 {
				return nil
			}
			ids := make([]string, len(batch))
			for i, child := range batch {
				ids[i] = child.MediaID
			}
			views, err := repos.MediaView.FindByIDs(ctx, ids, e.mediaQueryFilter(ctx, userID))
			if err != nil {
				return err
			}
			for _, view := range views {
				if err := repos.HongGuo.MarkPlayed(ctx, userID, view, *played); err != nil {
					return err
				}
			}
			after = batch[len(batch)-1].ID
		}
	})
	return true, err
}
