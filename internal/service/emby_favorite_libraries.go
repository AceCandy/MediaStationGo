package service

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// favoriteLibraryMembership 只补充最终收藏页的可见库归属，不改变父级、分页或收藏身份。
// 已维护的归属直接读取；历史 NULL 才回查本页作品的文件，不逐条发查询。
func (e *EmbyService) favoriteLibraryMembership(ctx context.Context, userID string, items []map[string]any) ([]map[string]any, error) {
	if len(items) == 0 {
		return items, nil
	}
	var ordinary, hongGuo, huangGuoAI, nfo []string
	for _, item := range items {
		if item["Type"] != "Movie" && item["Type"] != "Series" {
			continue
		}
		id, _ := item["Id"].(string)
		switch {
		case strings.HasPrefix(id, "hg-work-"), strings.HasPrefix(id, "hg-group-"):
			hongGuo = append(hongGuo, id)
		case strings.HasPrefix(id, "hga-work-"), strings.HasPrefix(id, "hga-group-"):
			huangGuoAI = append(huangGuoAI, id)
		case strings.HasPrefix(id, "nfo-"):
			nfo = append(nfo, strings.TrimPrefix(id, "nfo-"))
		default:
			ordinary = append(ordinary, id)
		}
	}
	db := e.repo.DB.WithContext(ctx)
	queries := []*gorm.DB{}
	if len(ordinary) > 0 {
		known := db.Table("jsonb_array_elements_text(item.library_ids) AS membership(id)").Select("id")
		files := metadataWorkFiles(db, db.Model(&model.Media{})).
			Where("item.library_ids IS NULL").Select("DISTINCT media.library_id AS id")
		queries = append(queries, db.Table("metadata_items item").Where("item.id IN ?", ordinary).
			Joins("JOIN LATERAL (? UNION ?) membership ON TRUE", known, files).
			Select("item.id, membership.id AS library_id"))
	}
	if len(hongGuo) > 0 {
		known := db.Table("jsonb_array_elements_text(w.library_ids) AS membership(id)").Select("id")
		files := db.Table("hongguo_media_bindings b").Joins("JOIN media m ON m.id=b.media_id").
			Where("b.work_id=w.id AND w.library_ids IS NULL AND m.catalog_source=?", model.TaskSystemHongGuo).
			Select("DISTINCT m.library_id AS id")
		queries = append(queries, repository.FilterHongGuoWorkIDs(db.Table("hongguo_works w"), hongGuo).
			Joins(repository.HongGuoAlbumJoin).
			Joins("JOIN LATERAL (? UNION ?) membership ON TRUE", known, files).
			Select("CASE WHEN g.id IS NULL THEN 'hg-work-'||w.id ELSE 'hg-group-'||g.id END AS id, membership.id AS library_id"))
	}
	if len(huangGuoAI) > 0 {
		queries = append(queries, e.huangGuoAIFiles(ctx, userID, "", huangGuoAI...).
			Select("CASE WHEN w.kind='movie' THEN 'hga-work-'||w.id ELSE 'hga-group-'||w.source_id END AS id,m.library_id"))
	}

	if len(nfo) > 0 {
		queries = append(queries, db.Table("nfo_items item").Where("item.id IN ?", nfo).
			Select("'nfo-'||item.id AS id, item.library_id"))
	}
	filter := e.mediaQueryFilter(ctx, userID)
	byID := map[string][]string{}
	for _, query := range queries {
		q := db.Table("(?) memberships", query).Where("library_id <> ''")
		if len(filter.AllowedLibraryIDs) > 0 {
			q = q.Where("library_id = ANY(?)", &filter.AllowedLibraryIDs)
		}
		if len(filter.HiddenLibraryIDs) > 0 {
			q = q.Where("library_id <> ALL(?)", &filter.HiddenLibraryIDs)
		}
		var rows []struct{ ID, LibraryID string }
		if err := q.Distinct("id", "library_id").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			byID[row.ID] = append(byID[row.ID], row.LibraryID)
		}
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		out[i] = maps.Clone(item)
		if item["Type"] == "Movie" || item["Type"] == "Series" {
			id, _ := item["Id"].(string)
			ids := append([]string{}, byID[id]...)
			slices.Sort(ids)
			out[i]["LibraryIds"] = slices.Compact(ids)
		}
	}
	return out, nil
}
