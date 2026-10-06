package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func (r *MediaViewRepository) huangGuoAILibraryPage(ctx context.Context, libraryID, identity string, offset, limit int, filter MediaQueryFilter) ([]model.MediaView, []LibraryMetadataSummary, int64, error) {
	db := r.db.WithContext(ctx)
	eligible := db.Table("huangguoai_works w").Where(huangGuoAIReadyWorkSQL)
	visible := db.Table("media fm").Joins("JOIN huangguoai_media_bindings fb ON fb.media_id=fm.id").Where("fb.work_id=w.id AND fm.library_id=? AND fm.catalog_source='huangguoai'", libraryID).Select("1")
	if len(filter.AllowedLibraryIDs) > 0 {
		visible = visible.Where("fm.library_id=ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		visible = visible.Where("fm.library_id<>ALL(?)", &filter.HiddenLibraryIDs)
	}
	eligible = FilterVisibleWorkLibraries(db, eligible, "w.library_ids", []string{libraryID}, filter).Where("EXISTS (? OFFSET 0)", visible)
	if identity != "" {
		eligible = filterHuangGuoAIWorkIDs(eligible, []string{identity}).Where(huangGuoAIWorkIdentitySQL+"=?", identity)
	}
	if filter.MissingPoster {
		eligible = eligible.Where("NOT EXISTS (SELECT 1 FROM huangguoai_artworks a WHERE a.work_id=w.id AND a.local_key<>'')")
	}
	if filter.MissingChineseTitle {
		eligible = eligible.Where("w.title !~ '[㐀-䶿一-鿿豈-﫿]'")
	}
	var total int64
	if err := eligible.Count(&total).Error; err != nil {
		return nil, nil, 0, err
	}
	var workIDs []string
	if err := eligible.Order("w.title,w.id").Offset(offset).Limit(limit).Pluck("w.id", &workIDs).Error; err != nil {
		return nil, nil, 0, err
	}
	if len(workIDs) == 0 {
		return []model.MediaView{}, []LibraryMetadataSummary{}, total, nil
	}
	var stats []struct {
		LibraryMetadataSummary
		WorkID string
	}
	err := r.huangGuoAIFileScope(ctx, filter).Where("m.library_id=? AND w.id=ANY(?)", libraryID, &workIDs).
		Select(huangGuoAIWorkIdentitySQL + ` AS metadata_id,w.id AS work_id,(ARRAY_AGG(m.id ORDER BY ep.number,m.created_at DESC,m.id))[1] AS media_id,COUNT(DISTINCT ep.id) AS count,COUNT(*) AS version_count`).Group("w.id,w.kind,w.source_id").Scan(&stats).Error
	if err != nil {
		return nil, nil, 0, err
	}
	byWork := map[string]LibraryMetadataSummary{}
	for _, x := range stats {
		byWork[x.WorkID] = x.LibraryMetadataSummary
	}
	summaries := []LibraryMetadataSummary{}
	ids := []string{}
	for _, id := range workIDs {
		if x, ok := byWork[id]; ok {
			summaries = append(summaries, x)
			ids = append(ids, x.MediaID)
		}
	}
	filter.MissingPoster = false
	filter.MissingChineseTitle = false
	rows, err := r.huangGuoAIViewsByIDs(ctx, ids, filter)
	byID := map[string]model.MediaView{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	rows = rows[:0]
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			rows = append(rows, row)
		}
	}
	return rows, summaries, total, err
}

func filterHuangGuoAIWorkIDs(q *gorm.DB, ids []string) *gorm.DB {
	var workIDs, sourceIDs []string
	for _, id := range ids {
		if v, ok := strings.CutPrefix(id, "hga-group-"); ok {
			sourceIDs = append(sourceIDs, v)
		} else if v, ok := strings.CutPrefix(id, "hga-season-"); ok {
			workIDs = append(workIDs, v)
		} else if v, ok := strings.CutPrefix(id, "hga-work-"); ok {
			workIDs = append(workIDs, v)
		}
	}
	return q.Where("w.id=ANY(?) OR w.source_id=ANY(?)", &workIDs, &sourceIDs)
}
func (r *MediaViewRepository) huangGuoAIPresentations(ctx context.Context, ids []string, season bool) (map[string]model.MediaView, error) {
	out := map[string]model.MediaView{}
	if len(ids) == 0 {
		return out, nil
	}
	identity := huangGuoAIWorkIdentitySQL
	if season {
		identity = "'hga-season-' || w.id"
	}
	var rows []struct {
		model.HuangGuoAIWork
		Identity  string
		ArtworkID string
	}
	q := filterHuangGuoAIWorkIDs(r.db.WithContext(ctx).Table("huangguoai_works w"), ids)
	err := q.Joins("LEFT JOIN huangguoai_artworks a ON a.work_id=w.id AND a.local_key<>''").Where(huangGuoAIReadyWorkSQL).Where(identity+"=ANY(?)", &ids).Select("w.*," + identity + " AS identity,COALESCE(a.id,'') AS artwork_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var tags []string
		if e := json.Unmarshal([]byte(row.Tags), &tags); e != nil {
			return nil, e
		}
		view := model.MediaView{Media: model.Media{PermanentBase: row.PermanentBase, CatalogSource: model.TaskSystemHuangGuoAI, LookupCatalogID: row.SourceID}, CatalogItemID: row.Identity, Title: row.Title, Overview: row.Overview, Rating: row.Rating, Genres: strings.Join(tags, ","), MetadataKind: row.Kind, MetadataSource: model.TaskSystemHuangGuoAI}
		view.ID = row.Identity
		if row.ArtworkID != "" {
			view.PosterURL = "/api/catalogs/huangguoai/artwork/" + row.ArtworkID
		}
		if season {
			if row.Kind != model.MetadataKindSeries {
				continue
			}
			view.MetadataKind = model.MetadataKindSeason
			view.SeasonNum = 1
			view.SeriesID = "hga-group-" + row.SourceID
			view.SeriesTitle = row.Title
		} else if row.Kind == model.MetadataKindSeries {
			view.SeriesID = row.Identity
			view.SeriesTitle = row.Title
		}
		out[row.Identity] = view
	}
	return out, nil
}
