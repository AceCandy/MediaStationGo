package service

import (
	"context"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ordinaryPersonWorkItems 头像入口按人物 ID 选择常规作品，分页后才加载详情。
func (e *EmbyService) ordinaryPersonWorkItems(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	for _, id := range p.PersonIDs {
		if strings.HasPrefix(id, "hg-person-") {
			return nil, false, nil
		}
	}
	v := e.mediaVisibility(ctx, p.UserID)
	if v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0 {
		return emptyItemsEnvelope(p.StartIndex), true, nil
	}
	filter := repository.MetadataSearchFilter{MediaQueryFilter: e.mediaQueryFilter(ctx, p.UserID), Kinds: embySearchKinds(p.IncludeItemTypes)}
	if p.ParentID != "" {
		library, err := FindLibraryBasic(ctx, e.repo, e.cache, p.ParentID)
		if err != nil {
			return nil, true, err
		}
		if library == nil {
			return nil, false, nil
		}
		visibility := e.mediaVisibility(ctx, p.UserID)
		if visibility.LibraryRestricted && !containsString(visibility.AllowedLibraryIDs, library.ID) {
			return emptyItemsEnvelope(p.StartIndex), true, nil
		}
		filter.AllowedLibraryIDs = e.mergedLibraryIDs(ctx, library.ID)
		p = libraryWorkSortParams(p)
	}
	p = workDateSortParams(p)
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if p.UserID == "" {
			return emptyItemsEnvelope(p.StartIndex), true, nil
		}
		filter.FavoriteUserID = p.UserID
	}
	if containsEmbyFilter(p.Filters, "IsResumable") {
		if p.UserID == "" {
			return emptyItemsEnvelope(p.StartIndex), true, nil
		}
		filter.ResumableUserID = p.UserID
	}
	q, err := e.repo.MediaView.PersonWorkQuery(ctx, "", p.PersonIDs, filter)
	if err != nil {
		return nil, true, err
	}
	db := e.repo.DB.WithContext(ctx)
	works := db.Table("(?) item", q.Select("search_metadata.*"))
	order := globalItemsOrder(p)
	if primarySupportedEmbySort(p.SortBy, false) == "productionyear" {
		dir := "ASC"
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
			dir = "DESC"
		}
		order = "COALESCE(year,0) " + dir + ", id " + dir
	}
	order = strings.ReplaceAll(order, "latest_at", "latest_media_added_at")
	if primarySupportedEmbySort(p.SortBy, false) == "favoriteadded" {
		works = works.Joins("JOIN (?) favorite_order ON favorite_order.metadata_id=item.id", e.favoriteAdditionTimes(ctx, p.UserID))
		order = "favorite_order.created_at DESC, item.id DESC"
	}
	// 文件时间只用于原有的文件依赖排序；名称、年份、评分及最近添加不展开版本。
	files := metadataWorkFiles(db, e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID))
	if p.ParentID != "" {
		files = files.Where("media.library_id IN ?", filter.AllowedLibraryIDs)
	}
	if strings.Contains(order, "created_at") && primarySupportedEmbySort(p.SortBy, false) != "favoriteadded" {
		works = works.Joins("LEFT JOIN LATERAL (?) file_dates ON TRUE", files.Select("MAX(media.created_at) AS created_at"))
		order = strings.ReplaceAll(order, "created_at", "file_dates.created_at")
	}
	if strings.Contains(order, "played_at") {
		states := repository.PlaybackStates(ctx, db, "legacy", p.UserID, filter.MediaQueryFilter)
		works = works.Joins("LEFT JOIN LATERAL (?) play_dates ON TRUE", files.
			Joins("LEFT JOIN (?) state ON state.metadata_id=media.metadata_id", states).
			Select("COALESCE(MAX(state.watched_at),MAX(media.created_at)) AS played_at"))
		order = strings.ReplaceAll(order, "played_at", "play_dates.played_at")
	}
	candidates := works.Select("item.id, ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal").Order(order)
	pageFiles := metadataWorkFiles(db, e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID))
	if p.ParentID != "" {
		pageFiles = pageFiles.Where("media.library_id IN ?", filter.AllowedLibraryIDs)
	}
	eligible := db.Table("work_batch item").Select("item.ordinal").Where("EXISTS (?)",
		e.workBatchFileEligibility(ctx, p, pageFiles, "legacy", "metadata_id=media.metadata_id"))
	ids, total, err := e.filteredWorkBatchPage(ctx, candidates, eligible, p.StartIndex, p.Limit, !p.SkipTotalRecordCount)
	if err != nil {
		return nil, true, err
	}
	items, err := e.personWorkPayloads(ctx, ids, p)
	if err != nil {
		return nil, true, err
	}
	if p.ParentID != "" {
		for _, item := range items {
			item["ParentId"] = p.ParentID
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, nil
}

// personWorkPayloads 为当前页补全没有分集、但存在直接绑定文件的整剧。
func (e *EmbyService) personWorkPayloads(ctx context.Context, ids []string, p ItemsParams) ([]map[string]any, error) {
	items, err := e.globalItemPayloads(ctx, ids, p)
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, item := range items {
		id, _ := item["Id"].(string)
		byID[id] = item
	}
	var missing []string
	for _, id := range ids {
		if byID[id] == nil && !strings.HasPrefix(id, "hg-") && !strings.HasPrefix(id, "hga-") && !strings.HasPrefix(id, "nfo-") {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		db := e.repo.DB.WithContext(ctx)
		identity := "CASE WHEN emby_metadata.kind='season' THEN emby_metadata.parent_id ELSE emby_metadata.id END"
		q := e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID).
			Where("emby_metadata.kind IN ('series','season') AND "+identity+" = ANY(?)", &missing)
		if p.ParentID != "" {
			q = q.Where("media.library_id IN ?", e.mergedLibraryIDs(ctx, p.ParentID))
		}
		var rows []struct {
			ID, LibraryID string
			CreatedAt     time.Time
			SeasonCount   int
		}
		if err := q.Select(identity + " AS id, MIN(media.library_id) AS library_id, MAX(media.created_at) AS created_at, COUNT(DISTINCT CASE WHEN emby_metadata.kind='season' THEN emby_metadata.id END) AS season_count").Group(identity).Scan(&rows).Error; err != nil {
			return nil, err
		}
		groups := make([]embySeriesGroup, 0, len(rows))
		for _, row := range rows {
			groups = append(groups, embySeriesGroup{ID: row.ID, LibraryID: row.LibraryID, CreatedAt: row.CreatedAt, Summary: &embySeriesSummary{SeasonCount: row.SeasonCount}})
		}
		for _, item := range e.seriesPayloadsWithFields(ctx, groups, p.UserID, p.Fields) {
			id, _ := item["Id"].(string)
			byID[id] = item
		}
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if item := byID[id]; item != nil {
			out = append(out, item)
		}
	}
	return out, nil
}
