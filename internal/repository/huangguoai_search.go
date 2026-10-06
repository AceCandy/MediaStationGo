package repository

import (
	"context"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"strings"
)

// SearchCandidates 仅召回有可见文件的作品，后续分页只水合命中页。
func (r *HuangGuoAIRepository) SearchCandidates(ctx context.Context, query string, f MetadataSearchFilter) ([]MetadataSearchCandidate, error) {
	rows := []MetadataSearchCandidate{}
	if len(f.PersonIDs) > 0 || len(f.Kinds) == 0 {
		return rows, nil
	}
	files := r.db.WithContext(ctx).Table("media fm").Joins("JOIN huangguoai_media_bindings fb ON fb.media_id=fm.id").Joins("JOIN huangguoai_episodes fe ON fe.id=fb.episode_id AND fe.work_id=w.id").Where("fb.work_id=w.id AND fm.catalog_source='huangguoai' AND (w.kind='series' OR fe.number=1)").Select("1")
	if len(f.AllowedLibraryIDs) > 0 {
		files = files.Where("fm.library_id=ANY(?)", &f.AllowedLibraryIDs)
	}
	if len(f.HiddenLibraryIDs) > 0 {
		files = files.Where("fm.library_id<>ALL(?)", &f.HiddenLibraryIDs)
	}
	q := r.db.WithContext(ctx).Table("huangguoai_works w").Where(huangGuoAIReadyWorkSQL).Where("w.kind IN ?", f.Kinds).Where("EXISTS (? OFFSET 0)", files)
	if f.FavoriteUserID != "" {
		q = q.Where("EXISTS (SELECT 1 FROM huangguoai_favorites f WHERE f.source_id=w.source_id AND f.user_id=? AND f.favorite)", f.FavoriteUserID)
	}
	if query != "" {
		q = q.Where("POSITION(LOWER(?) IN LOWER(w.title))>0", strings.TrimSpace(query))
	}
	err := q.Select(huangGuoAIWorkIdentitySQL + " AS id,w.kind,w.title").Order("w.title,w.id").Limit(MetadataSearchCandidateLimit).Scan(&rows).Error
	return rows, err
}
func (r *MediaViewRepository) huangGuoAISearchRepresentatives(ctx context.Context, ids []string, f MediaQueryFilter) ([]model.MediaView, error) {
	rows, err := r.HuangGuoAIItemsViews(ctx, ids, f)
	if err != nil {
		return nil, err
	}
	presentations, err := r.huangGuoAIPresentations(ctx, ids, false)
	if err != nil {
		return nil, err
	}
	out := []model.MediaView{}
	seen := map[string]bool{}
	for _, row := range rows {
		id := row.CatalogItemID
		if row.SeriesID != "" {
			id = row.SeriesID
		}
		if seen[id] {
			continue
		}
		p, ok := presentations[id]
		if !ok {
			continue
		}
		seen[id] = true
		row.CatalogItemID = id
		row.Title = p.Title
		row.MetadataKind = p.MetadataKind
		row.Overview = p.Overview
		row.Genres = p.Genres
		if p.MetadataKind == model.MetadataKindSeries {
			row.SeasonNum, row.EpisodeNum = 0, 0
		}
		out = append(out, row)
	}
	return out, nil
}
