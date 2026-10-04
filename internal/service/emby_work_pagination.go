package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// workLibraryScope 用作品的现存库集合检查权限；未知归属留给后续精确资格查询。
func (e *EmbyService) workLibraryScope(ctx context.Context, q *gorm.DB, column string, p ItemsParams) *gorm.DB {
	filter := e.mediaQueryFilter(ctx, p.UserID)
	var libraries []string
	if p.ParentID != "" {
		libraries = e.mergedLibraryIDs(ctx, p.ParentID)
	}
	return repository.FilterVisibleWorkLibraries(e.repo.DB.WithContext(ctx), q, column, libraries, filter)
}

// orderedWorkLibraryScope 先限定候选库再排序，防止 LIMIT 诱使全局时间索引跳过大量他库资料。
// 类型和非空时间等作品谓词须先放入 q；无限制全局查询保留索引提前停止。
func (e *EmbyService) orderedWorkLibraryScope(ctx context.Context, q *gorm.DB, alias string, p ItemsParams, libraryIDs []string) *gorm.DB {
	q = repository.FilterWorkLibraries(q, alias+".library_ids", libraryIDs)
	q = e.workLibraryScope(ctx, q, alias+".library_ids", p)
	filter := e.mediaQueryFilter(ctx, p.UserID)
	if len(libraryIDs) == 0 && p.ParentID != "" {
		libraryIDs = e.mergedLibraryIDs(ctx, p.ParentID)
	}
	if len(libraryIDs) == 0 {
		libraryIDs = filter.AllowedLibraryIDs
	}
	if len(libraryIDs) > 0 {
		// 将 NULL 回退与已知归属分开，使两个分支各用自己的索引，避免 OR 退化成全表扫描。
		members := make([]string, len(libraryIDs))
		for i, id := range libraryIDs {
			value, _ := json.Marshal([]string{id})
			members[i] = string(value)
		}
		known := q.Session(&gorm.Session{}).Where(alias+".library_ids @> ANY(?::jsonb[])", &members)
		unknown := q.Session(&gorm.Session{}).Where(alias + ".library_ids IS NULL")
		q = e.repo.DB.WithContext(ctx).Raw("? UNION ALL ?", known, unknown)
	}
	if p.ParentID != "" || len(libraryIDs) > 0 || len(filter.AllowedLibraryIDs) > 0 || len(filter.HiddenLibraryIDs) > 0 {
		q = e.repo.DB.WithContext(ctx).Table("(? OFFSET 0) "+alias, q)
	}
	return q
}

// filteredWorkBatchPage 的候选含稳定 ordinal；资格 SQL 只读取 work_batch。
// 每批先取至少 50 个、可容纳一页的作品再筛选，offset 只跳过合格作品；计数与补取共用只读快照。
// countEligible 仅用于同资格的批量计数计划；不得改变其文件/状态谓词。
func (e *EmbyService) filteredWorkBatchPage(ctx context.Context, candidates, eligible *gorm.DB, start, limit int, count bool, countEligible ...*gorm.DB) (ids []string, total int64, err error) {
	return e.repo.MediaView.WorkBatchPage(ctx, candidates, eligible, start, limit, count, countEligible...)
}

// workBatchFileEligibility 对本批作品检查文件与有效已看状态；identity 仅为固定关联表达式。
func (e *EmbyService) workBatchFileEligibility(ctx context.Context, p ItemsParams, files *gorm.DB, system, identity string) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	q := db.Table("(SELECT 1) presence").Select("1").Where("EXISTS (? OFFSET 0)", files.Session(&gorm.Session{}).Select("1"))
	if containsEmbyFilter(p.Filters, "IsPlayed") || containsEmbyFilter(p.Filters, "IsUnplayed") {
		states := repository.CompletedPlaybackStates(ctx, db, system, p.UserID, e.mediaQueryFilter(ctx, p.UserID)).Select("1").Where(identity)
		unplayed := files.Session(&gorm.Session{}).Select("1").Where("NOT EXISTS (? OFFSET 0)", states)
		if containsEmbyFilter(p.Filters, "IsPlayed") {
			q = q.Where("NOT EXISTS (? OFFSET 0)", unplayed)
		}
		if containsEmbyFilter(p.Filters, "IsUnplayed") {
			q = q.Where("EXISTS (? OFFSET 0)", unplayed)
		}
	}
	return q
}

// workCandidatePage 的输入只包含 id 与稳定排序 ordinal；计数和分页共用资格结果。
func (e *EmbyService) workCandidatePage(ctx context.Context, candidates *gorm.DB, start, limit int, countTotal bool) ([]string, int64, error) {
	page := e.repo.DB.Table("work_candidates").Order("ordinal").Offset(start)
	if limit > 0 {
		page = page.Limit(limit)
	}
	var rows []struct {
		ID    string
		Total int64
	}
	totals := "SELECT 0::bigint AS total"
	if countTotal {
		totals = "SELECT COUNT(*) AS total FROM work_candidates"
	}
	err := e.repo.DB.WithContext(ctx).Raw(`WITH work_candidates AS MATERIALIZED (?), page AS (?)
SELECT COALESCE(page.id,'') AS id, totals.total FROM (`+totals+`) totals LEFT JOIN page ON TRUE ORDER BY page.ordinal`, candidates, page).Scan(&rows).Error
	var total int64
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		total = row.Total
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	return ids, total, err
}

// metadataWorkPage 保留单项版本资格和排序；只有文件依赖的排序才读取作品内日期/标题。
func (e *EmbyService) metadataWorkPage(ctx context.Context, files *gorm.DB, p ItemsParams, membershipOnly bool) ([]model.MediaView, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	q := e.orderedWorkLibraryScope(ctx, db.Table("metadata_items recent"), "recent", p, nil)
	scope := files.Session(&gorm.Session{}).Where("media.metadata_id=recent.id")
	order := metadataOrderSQL(p, false)
	fileDateSort := strings.Contains(order, "MAX(media.created_at)")
	if fileDateSort || strings.Contains(order, "media.scan_title") {
		stats := scope.Session(&gorm.Session{})
		if fileDateSort {
			stats = stats.Select("MAX(media.created_at) AS created_at")
			if primarySupportedEmbySort(p.SortBy, false) == "premieredate" {
				// 文件日期只影响上映日期和年份均并列的作品；仍按原可见文件范围取值。
				q = db.Table("(?) recent", q.Select("recent.*, COUNT(*) OVER (PARTITION BY COALESCE(recent.release_date,''), COALESCE(recent.year,0)) AS release_ties"))
				stats = stats.Where("recent.release_ties > 1")
			}
		} else {
			stats = stats.Select("MIN(media.scan_title) AS title").Where("recent.title IS NULL")
		}
		q = q.Joins("LEFT JOIN LATERAL (?) sort_values ON TRUE", stats)
	}
	// 仅替换 metadataOrderSQL 的固定表达式，方向、NULL 顺序和并列键仍由同一排序方法拥有。
	order = strings.NewReplacer(
		"MIN(COALESCE(emby_metadata.title, media.scan_title))", "COALESCE(recent.title, sort_values.title)",
		"MAX(COALESCE(emby_metadata.release_date, ''))", "COALESCE(recent.release_date, '')",
		"MAX(COALESCE(emby_metadata.year, 0))", "COALESCE(recent.year, 0)",
		"MAX(COALESCE(emby_metadata.rating, 0))", "COALESCE(recent.rating, 0)",
		"MAX(emby_metadata.latest_media_added_at)", "recent.latest_media_added_at",
		"MAX(media.created_at)", "sort_values.created_at", "media.metadata_id", "recent.id",
	).Replace(order)
	q = q.Select("recent.id, recent.kind, recent.library_ids, recent.latest_media_added_at, ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal").Order(order)
	eligibility := "EXISTS (? OFFSET 0)"
	if membershipOnly {
		// 叶子作品没有后代汇总歧义；普通无文件筛选列表直接使用已维护的归属。
		eligibility = "CASE WHEN recent.library_ids IS NOT NULL AND recent.library_ids <> '[]'::jsonb AND recent.latest_media_added_at IS NOT NULL AND recent.kind IN ('movie','episode') THEN TRUE ELSE EXISTS (? OFFSET 0) END"
	}
	eligible := db.Table("work_batch recent").Select("recent.ordinal").Where(eligibility, scope.Session(&gorm.Session{}).Select("1"))
	if embyRandomSort(p) {
		eligible = db.Table("work_batch recent").Select("recent.ordinal").Where("EXISTS (?)",
			e.workBatchFileEligibility(ctx, p, scope, "legacy", "metadata_id=media.metadata_id"))
	}
	var ids []string
	var total int64
	var err error
	if fileDateSort {
		// 文件日期排序已遍历候选；一次物化资格和分页，避免补批时重复计算日期。
		qualified := db.Raw(`WITH work_batch AS MATERIALIZED (?), qualified AS MATERIALIZED (?)
SELECT b.id, b.ordinal FROM work_batch b JOIN qualified q ON q.ordinal=b.ordinal`, q, eligible)
		ids, total, err = e.workCandidatePage(ctx, qualified, p.StartIndex, p.Limit, !p.SkipTotalRecordCount)
	} else {
		ids, total, err = e.filteredWorkBatchPage(ctx, q, eligible, p.StartIndex, p.Limit, !p.SkipTotalRecordCount)
	}
	if err != nil {
		return nil, 0, err
	}
	views, err := e.metadataViewsForIDs(ctx, files, p.UserID, ids)
	return views, total, err
}

// seriesWorkPage 与 Latest 同样先选作品，保留原分集资格、作品筛选及文件日期排序。
func (e *EmbyService) seriesWorkPage(ctx context.Context, files *gorm.DB, p ItemsParams, start, limit int) ([]embySeriesGroup, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	q := e.applySeriesPageFilters(ctx, db.Table("metadata_items scope_series").Where("scope_series.kind='series'"), p)
	q = e.orderedWorkLibraryScope(ctx, q, "scope_series", p, nil)
	scope := files.Session(&gorm.Session{}).Where(`emby_metadata.parent_id IN (
SELECT id FROM metadata_items WHERE parent_id=scope_series.id AND kind='season')`)
	order := seriesOrderSQL(p)
	if p.ParentID != "" && primarySupportedEmbySort(p.SortBy, false) == "communityrating" {
		dir := "ASC"
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
			dir = "DESC"
		}
		order = "COALESCE(scope_series.rating,0) " + dir + ", scope_series.id " + dir
	}

	fileDateSort := strings.Contains(order, "MAX(media.created_at)")
	if fileDateSort {
		stats := scope.Session(&gorm.Session{}).Select("MAX(media.created_at) AS created_at")
		if primarySupportedEmbySort(p.SortBy, false) == "premieredate" {
			q = db.Table("(?) scope_series", q.Select("scope_series.*, COUNT(*) OVER (PARTITION BY COALESCE(scope_series.release_date,''), COALESCE(scope_series.year,0)) AS release_ties"))
			stats = stats.Where("scope_series.release_ties > 1")
		}
		q = q.Joins("JOIN LATERAL (?) sort_values ON TRUE", stats)
	}
	order = strings.NewReplacer(
		"MIN(COALESCE(scope_series.title, ''))", "COALESCE(scope_series.title, '')",
		"MAX(COALESCE(scope_series.release_date, ''))", "COALESCE(scope_series.release_date, '')",
		"MAX(COALESCE(scope_series.year, 0))", "COALESCE(scope_series.year, 0)",
		"MAX(scope_series.latest_media_added_at)", "scope_series.latest_media_added_at",
		"MAX(media.created_at)", "sort_values.created_at",
	).Replace(order)
	q = q.Select("scope_series.id, scope_series.latest_media_added_at, ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal").Order(order)
	qualifiedFiles := e.seriesWorkFiles(db, files).Select("1")
	eligible := db.Table("work_batch recent").Select("recent.ordinal").Where("EXISTS (? OFFSET 0)", qualifiedFiles)
	if embyRandomSort(p) {
		eligible = db.Table("work_batch recent").Select("recent.ordinal").Where("EXISTS (?)",
			e.workBatchFileEligibility(ctx, p, qualifiedFiles, "legacy", "metadata_id=media.metadata_id"))
	}
	// 有入库汇总的作品找到一个合格分集即停；未知汇总仍批量读受限文件，避免逐个探测空目录。
	known := eligible.Session(&gorm.Session{}).Where("recent.latest_media_added_at IS NOT NULL")
	unknown := db.Table("work_batch").Where("latest_media_added_at IS NULL")
	countFiles := files.Session(&gorm.Session{}).Select("emby_metadata.parent_id").Where("EXISTS (?)", unknown.Select("1"))
	fallback := db.Table("(? OFFSET 0) files", countFiles).
		Joins("JOIN metadata_items season ON season.id=files.parent_id AND season.kind='season'").
		Joins("JOIN (?) work ON work.id=season.parent_id", unknown.Session(&gorm.Session{}).Select("id,ordinal")).Select("DISTINCT work.ordinal")
	countEligible := db.Raw("? UNION ALL ?", known, fallback)
	if embyRandomSort(p) {
		countEligible = eligible
	}
	var ids []string
	var total int64
	var err error
	if fileDateSort {
		qualified := db.Raw(`WITH work_batch AS MATERIALIZED (?), qualified AS MATERIALIZED (?)
SELECT b.id, b.ordinal FROM work_batch b JOIN qualified q ON q.ordinal=b.ordinal`, q, eligible)
		ids, total, err = e.workCandidatePage(ctx, qualified, start, limit, !p.SkipTotalRecordCount)
	} else {
		ids, total, err = e.filteredWorkBatchPage(ctx, q, eligible, start, limit, !p.SkipTotalRecordCount, countEligible)
	}
	if err != nil {
		return nil, 0, err
	}
	summaryScope := e.applySeriesPageFilters(ctx, seriesScopeQuery(files.Session(&gorm.Session{})), p)
	groups, err := e.seriesSummaries(ctx, summaryScope, ids)
	return groups, total, err
}

// seriesWorkFiles 沿批内作品的季定位分集，再按分集索引查文件，避免反复扫描无关季集。
func (e *EmbyService) seriesWorkFiles(db, files *gorm.DB) *gorm.DB {
	leaves := db.Table("metadata_items season").Select("episode.id").
		Joins("JOIN LATERAL (SELECT id FROM metadata_items WHERE parent_id=season.id OFFSET 0) episode ON TRUE").
		Where("season.parent_id=recent.id AND season.kind='season'")
	return files.Session(&gorm.Session{}).Joins("JOIN (?) leaf ON leaf.id=media.metadata_id", leaves)
}
