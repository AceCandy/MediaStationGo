package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (r *HongGuoRepository) SetSearchBackend(backend MediaSearchBackend) {
	r.searchBackend = backend
	_, syncBackend := backend.(MediaSearchSyncBackend)
	// 进程启动后必须先安全重建，不能信任上次进程可能中断的增量同步。
	r.searchFailed.Store(syncBackend)
}

// hongGuoSearchWorks 将官方合集投影为一个搜索身份，不依赖文件或用户状态。
func (r *HongGuoRepository) hongGuoSearchWorks(ctx context.Context, ids ...[]string) *gorm.DB {
	works := r.db.WithContext(ctx).Table("hongguo_works")
	if len(ids) > 0 {
		workIDs, albumIDs := []string{}, []string{}
		for _, id := range ids[0] {
			if strings.HasPrefix(id, "hg-work-") {
				workIDs = append(workIDs, strings.TrimPrefix(id, "hg-work-"))
			} else if strings.HasPrefix(id, "hg-group-") {
				albumIDs = append(albumIDs, strings.TrimPrefix(id, "hg-group-"))
			}
		}
		// 在合集标题和文件存在性计算前，利用作品主键/合集索引限定命中范围。
		works = works.Where("id = ANY(?) OR (related_album_id = ANY(?) AND kind = 'series' AND season_index > 0)", &workIDs, &albumIDs)
	}
	return r.db.WithContext(ctx).Table("(?) w", works.Select("*")).Joins(HongGuoAlbumJoin).
		Select(`DISTINCT CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END AS id,
w.kind, COALESCE(g.title,w.title) AS title, '' AS original_name, '' AS overview, '' AS genres, 0 AS year`)
}

func (r *HongGuoRepository) searchDocuments(ctx context.Context, ids []string) ([]MetadataSearchDocument, error) {
	rows := []MetadataSearchDocument{}
	if len(ids) == 0 {
		return rows, nil
	}
	works := r.hongGuoSearchWorks(ctx, ids).
		Joins("JOIN hongguo_media_bindings b ON b.work_id = w.id").
		Joins("JOIN media m ON m.id = b.media_id AND m.catalog_source = 'hongguo'").
		Select("CASE WHEN g.id IS NOT NULL THEN 'hg-group-' || g.id ELSE 'hg-work-' || w.id END AS id, w.kind, COALESCE(g.title,w.title) AS title, m.library_id")
	var projected []struct {
		ID, Kind, Title, Libraries string
	}
	err := r.db.WithContext(ctx).Table("(?) AS search_metadata", works).Where("id = ANY(?)", &ids).
		Select("id, kind, title, json_agg(DISTINCT library_id ORDER BY library_id)::text AS libraries").Group("id, kind, title").Scan(&projected).Error
	if err != nil {
		return nil, err
	}
	for _, row := range projected {
		doc := MetadataSearchDocument{ID: row.ID, Kind: row.Kind, Title: row.Title}
		if err := json.Unmarshal([]byte(row.Libraries), &doc.LibraryIDs); err != nil {
			return nil, err
		}
		rows = append(rows, doc)
	}
	return rows, nil
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

// SearchCandidates 在索引内限定媒体库，数据库只复核命中身份；不可用时回退数据库。
func (r *HongGuoRepository) SearchCandidates(ctx context.Context, query string, filter MetadataSearchFilter) ([]MetadataSearchCandidate, error) {
	groups := buildMetadataSearchTermGroups(MediaSearchTerms(query))
	if len(groups) == 0 || len(filter.Kinds) == 0 || (filter.LibraryRestricted && len(filter.AllowedLibraryIDs) == 0) {
		return []MetadataSearchCandidate{}, nil
	}
	files := (&MediaViewRepository{db: r.db}).hongGuoFileScope(ctx, "", filter.MediaQueryFilter).
		Select("1").Where("b.work_id = w.id")
	// 与媒体库分页保持相同的存在性边界，避免展开全部分集后再计算可见作品。
	workScope := func(ids ...[]string) *gorm.DB {
		works := FilterVisibleWorkLibraries(r.db.WithContext(ctx), r.hongGuoSearchWorks(ctx, ids...), "w.library_ids", nil, filter.MediaQueryFilter).
			Where("w.kind IN ?", filter.Kinds).
			Where("CASE WHEN w.library_ids IS NOT NULL AND w.library_ids <> '[]'::jsonb AND w.latest_media_added_at IS NOT NULL THEN TRUE ELSE EXISTS (? OFFSET 0) END", files)
		if len(filter.PersonIDs) > 0 {
			works = works.Where("EXISTS (SELECT 1 FROM hongguo_credits c WHERE c.work_id = w.id AND 'hg-person-' || c.person_id = ANY(?))", &filter.PersonIDs)
		}
		if filter.FavoriteUserID != "" {
			works = works.Where("EXISTS (SELECT 1 FROM hongguo_user_states s WHERE s.source_id = w.source_id AND s.user_id = ? AND s.episode_number = 0 AND s.favorite)", filter.FavoriteUserID)
		}
		return works
	}
	scope := func(ids ...[]string) *gorm.DB {
		works := workScope(ids...)
		return r.db.WithContext(ctx).Table("(?) AS search_metadata", works)
	}
	var ids []string
	backend, failed := r.searchBackend, r.searchFailed.Load()
	if backend != nil && !failed && !filter.ForcePostgres {
		searchFilter, err := (&MediaViewRepository{db: r.db}).prepareMetadataSearchFilter(ctx, MetadataSearchFilter{
			MediaQueryFilter: filter.MediaQueryFilter, Fields: MetadataSearchFieldsTitle, Kinds: filter.Kinds,
		})
		if err != nil {
			return nil, err
		}
		if searchFilter.LibraryRestricted && len(searchFilter.VisibleLibraryIDs) == 0 {
			return []MetadataSearchCandidate{}, nil
		}
		if len(filter.PersonIDs) > 0 || filter.FavoriteUserID != "" {
			searchFilter.CandidateIDs = []string{}
			if err := scope().Pluck("id", &searchFilter.CandidateIDs).Error; err != nil {
				return nil, err
			}
			if len(searchFilter.CandidateIDs) == 0 {
				return []MetadataSearchCandidate{}, nil
			}
		}
		// 人物/收藏仍须在候选上限前过滤；超出 terms 容量时使用数据库。
		if len(searchFilter.CandidateIDs) <= 65536 {
			var err error
			ids, _, err = backend.SearchMetadataIDs(ctx, query, 0, maxMetadataSearchCandidates, searchFilter)
			if err != nil {
				ids = nil
			} else if len(ids) == 0 {
				return []MetadataSearchCandidate{}, ctx.Err()
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	q := scope()
	if ids != nil {
		q = scope(ids).Where("id = ANY(?)", &ids)
	}
	q = q.Where("POSITION(LOWER(?) IN LOWER(title)) > 0", strings.TrimSpace(query))
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
