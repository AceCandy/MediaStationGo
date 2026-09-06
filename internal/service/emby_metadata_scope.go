package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// metadataPage applies count, ordering, and pagination to logical works before
// loading their visible playable versions.
func (e *EmbyService) metadataPage(ctx context.Context, q *gorm.DB, userID, order string, start, limit int) ([]model.MediaView, int64, error) {
	grouped := q.Session(&gorm.Session{}).
		Select("media.metadata_id").
		Group("media.metadata_id")
	var total int64
	if err := e.repo.DB.WithContext(ctx).Table("(?) AS scoped_metadata", grouped).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type metadataIDRow struct {
		MetadataID string `gorm:"column:metadata_id"`
	}
	var idRows []metadataIDRow
	idQuery := q.Session(&gorm.Session{}).
		Select("media.metadata_id AS metadata_id").
		Group("media.metadata_id").
		Order(order).
		Offset(start)
	if limit > 0 {
		idQuery = idQuery.Limit(limit)
	}
	if err := idQuery.Scan(&idRows).Error; err != nil {
		return nil, 0, err
	}
	metadataIDs := make([]string, 0, len(idRows))
	for _, row := range idRows {
		if id := strings.TrimSpace(row.MetadataID); id != "" {
			metadataIDs = append(metadataIDs, id)
		}
	}
	if len(metadataIDs) == 0 {
		return []model.MediaView{}, total, nil
	}

	var mediaRows []model.Media
	if err := q.Session(&gorm.Session{}).
		Where("media.metadata_id IN ?", metadataIDs).
		Order("media.created_at DESC, media.id DESC").
		Find(&mediaRows).Error; err != nil {
		return nil, 0, err
	}
	views, err := e.mediaViewsForRows(ctx, mediaRows, userID)
	if err != nil {
		return nil, 0, err
	}
	return preferredMetadataViews(views, metadataIDs), total, nil
}

func preferredMetadataViews(views []model.MediaView, metadataIDs []string) []model.MediaView {
	views = collapseMediaPartViews(views)
	byMetadata := make(map[string]model.MediaView, len(metadataIDs))
	for _, view := range views {
		id := strings.TrimSpace(view.MetadataID)
		current, ok := byMetadata[id]
		if id == "" || (ok && !preferMediaVersion(view.Media, current.Media)) {
			continue
		}
		byMetadata[id] = view
	}
	out := make([]model.MediaView, 0, len(metadataIDs))
	for _, id := range metadataIDs {
		if view, ok := byMetadata[id]; ok {
			out = append(out, view)
		}
	}
	return out
}

func preferredMetadataViewsInOrder(views []model.MediaView) []model.MediaView {
	seen := make(map[string]struct{}, len(views))
	ids := make([]string, 0, len(views))
	for _, view := range views {
		id := strings.TrimSpace(view.MetadataID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return preferredMetadataViews(views, ids)
}

func seriesScopeQuery(q *gorm.DB) *gorm.DB {
	return q.
		Joins("JOIN metadata_items AS scope_season ON scope_season.id = emby_metadata.parent_id AND scope_season.kind = 'season'").
		Joins("JOIN metadata_items AS scope_series ON scope_series.id = scope_season.parent_id AND scope_series.kind = 'series'")
}

func (e *EmbyService) seriesMetadataPage(ctx context.Context, q *gorm.DB, userID string, p ItemsParams, start, limit int) ([]embySeriesGroup, int64, error) {
	grouped := q.Session(&gorm.Session{}).
		Select("scope_series.id").
		Group("scope_series.id")
	var total int64
	if err := e.repo.DB.WithContext(ctx).Table("(?) AS scoped_series", grouped).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type seriesIDRow struct {
		SeriesID string `gorm:"column:series_id"`
	}
	var idRows []seriesIDRow
	idQuery := q.Session(&gorm.Session{}).
		Select("scope_series.id AS series_id").
		Group("scope_series.id").
		Order(seriesOrderSQL(p)).
		Offset(start)
	if limit > 0 {
		idQuery = idQuery.Limit(limit)
	}
	if err := idQuery.Scan(&idRows).Error; err != nil {
		return nil, 0, err
	}
	seriesIDs := make([]string, 0, len(idRows))
	for _, row := range idRows {
		if id := strings.TrimSpace(row.SeriesID); id != "" {
			seriesIDs = append(seriesIDs, id)
		}
	}
	if len(seriesIDs) == 0 {
		return []embySeriesGroup{}, total, nil
	}

	groups, err := e.seriesSummaries(ctx, q, seriesIDs)
	return groups, total, err
}

// seriesSummaries 只统计当前页可见关联，不读取分集文件或探测数据。
func (e *EmbyService) seriesSummaries(ctx context.Context, q *gorm.DB, seriesIDs []string) ([]embySeriesGroup, error) {
	if len(seriesIDs) == 0 {
		return []embySeriesGroup{}, nil
	}
	var rows []struct {
		ID           string
		LibraryID    string
		CreatedAt    time.Time
		EpisodeCount int
		SeasonCount  int
	}
	err := q.Session(&gorm.Session{}).Where("scope_series.id IN ?", seriesIDs).
		Select(`scope_series.id AS id, MIN(media.library_id) AS library_id,
			MAX(media.created_at) AS created_at, COUNT(DISTINCT media.metadata_id) AS episode_count,
			COUNT(DISTINCT scope_season.id) AS season_count`).Group("scope_series.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byID := make(map[string]embySeriesGroup, len(rows))
	for _, row := range rows {
		byID[row.ID] = embySeriesGroup{ID: row.ID, LibraryID: row.LibraryID, CreatedAt: row.CreatedAt,
			Summary: &embySeriesSummary{EpisodeCount: row.EpisodeCount, SeasonCount: row.SeasonCount}}
	}
	groups := make([]embySeriesGroup, 0, len(rows))
	for _, id := range seriesIDs {
		if group, ok := byID[id]; ok {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func seriesOrderSQL(p ItemsParams) string {
	key := primarySupportedEmbySort(p.SortBy, false)
	dir := "ASC"
	if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
		dir = "DESC"
	}
	expression := "MIN(COALESCE(scope_series.title, ''))"
	secondary := ""
	switch key {
	case "sortname", "name":
	case "datecreated":
		expression = "MAX(media.created_at)"
	default:
		if !strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending") {
			dir = "DESC"
		}
		expression = "MAX(COALESCE(scope_series.release_date, ''))"
		secondary = fmt.Sprintf(", MAX(COALESCE(scope_series.year, 0)) %s, MAX(media.created_at) %s", dir, dir)
	}
	return fmt.Sprintf("%s %s%s, scope_series.id %s", expression, dir, secondary, dir)
}

func metadataOrderSQL(p ItemsParams, resumeFilter bool) string {
	key := primarySupportedEmbySort(p.SortBy, resumeFilter)
	dir := "DESC"
	expression := "MAX(COALESCE(emby_metadata.release_date, ''))"
	secondary := "MAX(COALESCE(emby_metadata.year, 0))"
	switch key {
	case "sortname", "name":
		dir = "ASC"
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
			dir = "DESC"
		}
		expression = "MIN(COALESCE(emby_metadata.title, media.scan_title))"
		secondary = ""
	case "datecreated":
		dir = "ASC"
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
			dir = "DESC"
		}
		expression = "MAX(media.created_at)"
		secondary = ""
	case "dateplayed":
		dir = "ASC"
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
			dir = "DESC"
		}
		expression = "MAX(resume.watched_at)"
		secondary = ""
	case "communityrating":
		dir = "ASC"
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
			dir = "DESC"
		}
		expression = "MAX(COALESCE(emby_metadata.rating, 0))"
		secondary = ""
	default:
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending") {
			dir = "ASC"
		}
	}
	parts := []string{fmt.Sprintf("%s %s", expression, dir)}
	if secondary != "" {
		parts = append(parts, fmt.Sprintf("%s %s", secondary, dir), fmt.Sprintf("MAX(media.created_at) %s", dir))
	}
	parts = append(parts, fmt.Sprintf("media.metadata_id %s", dir))
	return strings.Join(parts, ", ")
}
