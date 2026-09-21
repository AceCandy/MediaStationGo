package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (r *HongGuoRepository) SetSearchBackend(backend MediaSearchBackend) {
	r.searchBackend = backend
}

// hongGuoSearchWorks 将官方合集投影为一个搜索身份，不依赖文件或用户状态。
func (r *HongGuoRepository) hongGuoSearchWorks(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Table("hongguo_works w").Joins(HongGuoAlbumJoin).
		Select(`DISTINCT CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END AS id,
w.kind, COALESCE(g.title,w.title) AS title, '' AS original_name, '' AS overview, '' AS genres, 0 AS year`)
}

func (r *HongGuoRepository) searchDocuments(ctx context.Context, ids []string) ([]MetadataSearchDocument, error) {
	rows := []MetadataSearchDocument{}
	if len(ids) == 0 {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Table("(?) AS search_metadata", r.hongGuoSearchWorks(ctx)).Where("id = ANY(?)", &ids).Scan(&rows).Error
	return rows, err
}

func (r *HongGuoRepository) searchDocumentIDs(ctx context.Context, after string, limit int) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Table("(?) AS search_metadata", r.hongGuoSearchWorks(ctx)).
		Where("id > ?", after).Order("id").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (r *HongGuoRepository) BackfillSearchIndex(ctx context.Context, batchLimit int, pause time.Duration) (int64, error) {
	return r.searchIndex.backfill(ctx, batchLimit, pause, r.searchDocumentIDs, r.searchDocuments)
}

// refreshSearchWork 提交后同时刷新旧、新合集；已不存在的身份从索引删除。
func (r *HongGuoRepository) refreshSearchWork(ctx context.Context, workID string, albumIDs ...string) {
	ids := []string{"hg-work-" + workID}
	for _, id := range uniqueNonEmptyStrings(albumIDs) {
		ids = append(ids, "hg-group-"+id)
	}
	r.searchIndex.refresh(ctx, ids, r.searchDocuments)
}

// SearchCandidates 先限定当前可见文件身份，再检索独立索引；数据库始终复核返回项。
func (r *HongGuoRepository) SearchCandidates(ctx context.Context, query string, filter MetadataSearchFilter) ([]MetadataSearchCandidate, error) {
	groups := buildMetadataSearchTermGroups(MediaSearchTerms(query))
	if len(groups) == 0 || len(filter.Kinds) == 0 || (filter.LibraryRestricted && len(filter.AllowedLibraryIDs) == 0) {
		return []MetadataSearchCandidate{}, nil
	}
	var hasMedia bool
	if err := r.db.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source = 'hongguo')").Scan(&hasMedia).Error; err != nil {
		return nil, err
	} else if !hasMedia {
		return []MetadataSearchCandidate{}, nil
	}
	files := r.db.WithContext(ctx).Table("media m").Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
		Select("b.work_id").Where("m.catalog_source = 'hongguo'")
	if len(filter.AllowedLibraryIDs) > 0 {
		files = files.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		files = files.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	works := r.hongGuoSearchWorks(ctx).Where("w.id IN (?)", files).Where("w.kind IN ?", filter.Kinds)
	if len(filter.PersonIDs) > 0 {
		works = works.Where("EXISTS (SELECT 1 FROM hongguo_credits c WHERE c.work_id = w.id AND 'hg-person-' || c.person_id = ANY(?))", &filter.PersonIDs)
	}
	if filter.FavoriteUserID != "" {
		works = works.Where("EXISTS (SELECT 1 FROM hongguo_user_states s WHERE s.source_id = w.source_id AND s.user_id = ? AND s.episode_number = 0 AND s.favorite)", filter.FavoriteUserID)
	}
	scope := func() *gorm.DB {
		return r.db.WithContext(ctx).Table("(?) AS search_metadata", works)
	}
	var ids []string
	backend, failed := r.searchBackend, r.searchFailed.Load()
	if backend != nil && !failed && !filter.ForcePostgres {
		visible := []string{}
		if err := scope().Pluck("id", &visible).Error; err != nil {
			return nil, err
		}
		if len(visible) == 0 {
			return []MetadataSearchCandidate{}, nil
		}
		// ponytail: OpenSearch terms 默认最多 65536 项；更大可见集合走数据库，规模增长后改用索引内权限字段。
		if len(visible) <= 65536 {
			searchFilter := MetadataSearchFilter{MediaQueryFilter: MediaQueryFilter{IncludeNSFW: true}, Fields: MetadataSearchFieldsTitle, Kinds: filter.Kinds, CandidateIDs: visible}
			var err error
			ids, _, err = backend.SearchMetadataIDs(ctx, query, 0, maxMetadataSearchCandidates, searchFilter)
			if err != nil {
				ids = nil
			} else if ids == nil {
				ids = []string{}
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	q := scope().Where("POSITION(LOWER(?) IN LOWER(title)) > 0", strings.TrimSpace(query))
	if ids != nil {
		q = q.Where("id = ANY(?)", &ids)
	}
	var rows []MetadataSearchDocument
	err := q.Order(gorm.Expr("CASE WHEN title = ? THEN 0 WHEN title LIKE ? ESCAPE '\\' THEN 1 ELSE 2 END, title, id", query, EscapeLike(query)+"%")).
		Limit(maxMetadataSearchCandidates).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	candidates := make([]MetadataSearchCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, MetadataSearchCandidate{ID: row.ID, Kind: row.Kind, Title: row.Title})
	}
	return candidates, nil
}
