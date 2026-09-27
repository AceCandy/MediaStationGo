package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// workCandidatePage 的输入只包含 id 与稳定排序 ordinal；计数和分页共用资格结果。
func (e *EmbyService) workCandidatePage(ctx context.Context, candidates *gorm.DB, start, limit int) ([]string, int64, error) {
	page := e.repo.DB.Table("work_candidates").Order("ordinal").Offset(start)
	if limit > 0 {
		page = page.Limit(limit)
	}
	var rows []struct {
		ID    string
		Total int64
	}
	err := e.repo.DB.WithContext(ctx).Raw(`WITH work_candidates AS MATERIALIZED (?), page AS (?)
SELECT COALESCE(page.id,'') AS id, totals.total FROM (SELECT COUNT(*) AS total FROM work_candidates) totals LEFT JOIN page ON TRUE ORDER BY page.ordinal`, candidates, page).Scan(&rows).Error
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
func (e *EmbyService) metadataWorkPage(ctx context.Context, files *gorm.DB, p ItemsParams) ([]model.MediaView, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	libraries := e.mediaVisibility(ctx, p.UserID).AllowedLibraryIDs
	if p.ParentID != "" {
		libraries = e.mergedLibraryIDs(ctx, p.ParentID)
	}
	q := repository.FilterWorkLibraries(db.Table("metadata_items recent"), "recent.library_ids", libraries)
	scope := files.Session(&gorm.Session{}).Where("media.metadata_id=recent.id")
	q = q.Where("EXISTS (?)", scope.Session(&gorm.Session{}).Select("1"))
	order := metadataOrderSQL(p, false)
	if strings.Contains(order, "MAX(media.created_at)") || strings.Contains(order, "media.scan_title") {
		stats := scope.Session(&gorm.Session{}).Select("MAX(media.created_at) AS created_at, MIN(media.scan_title) AS title")
		if strings.Contains(order, "media.scan_title") {
			stats = stats.Where("recent.title IS NULL")
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
	q = q.Select("recent.id, ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal")
	ids, total, err := e.workCandidatePage(ctx, q, p.StartIndex, p.Limit)
	if err != nil {
		return nil, 0, err
	}
	views, err := e.metadataViewsForIDs(ctx, files, p.UserID, ids)
	return views, total, err
}

// seriesWorkPage 与 Latest 同样先选作品，保留原分集资格、作品筛选及文件日期排序。
func (e *EmbyService) seriesWorkPage(ctx context.Context, files *gorm.DB, p ItemsParams, start, limit int) ([]embySeriesGroup, int64, error) {
	db := e.repo.DB.WithContext(ctx)
	libraries := e.mediaVisibility(ctx, p.UserID).AllowedLibraryIDs
	if p.ParentID != "" {
		libraries = e.mergedLibraryIDs(ctx, p.ParentID)
	}
	q := repository.FilterWorkLibraries(db.Table("metadata_items scope_series").Where("scope_series.kind='series'"), "scope_series.library_ids", libraries)
	q = e.applySeriesPageFilters(ctx, q, p)
	scope := files.Session(&gorm.Session{}).Where(`emby_metadata.parent_id IN (
SELECT id FROM metadata_items WHERE parent_id=scope_series.id AND kind='season')`)
	q = q.Where("EXISTS (?)", scope.Session(&gorm.Session{}).Select("1"))
	order := seriesOrderSQL(p)
	if strings.Contains(order, "MAX(media.created_at)") {
		q = q.Joins("JOIN LATERAL (?) sort_values ON TRUE", scope.Session(&gorm.Session{}).Select("MAX(media.created_at) AS created_at"))
	}
	order = strings.NewReplacer(
		"MIN(COALESCE(scope_series.title, ''))", "COALESCE(scope_series.title, '')",
		"MAX(COALESCE(scope_series.release_date, ''))", "COALESCE(scope_series.release_date, '')",
		"MAX(COALESCE(scope_series.year, 0))", "COALESCE(scope_series.year, 0)",
		"MAX(scope_series.latest_media_added_at)", "scope_series.latest_media_added_at",
		"MAX(media.created_at)", "sort_values.created_at",
	).Replace(order)
	q = q.Select("scope_series.id, ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal")
	ids, total, err := e.workCandidatePage(ctx, q, start, limit)
	if err != nil {
		return nil, 0, err
	}
	summaryScope := e.applySeriesPageFilters(ctx, seriesScopeQuery(files.Session(&gorm.Session{})), p)
	groups, err := e.seriesSummaries(ctx, summaryScope, ids)
	return groups, total, err
}
