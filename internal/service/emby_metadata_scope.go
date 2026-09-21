package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// metadataPage applies count, ordering, and pagination to logical works before
// loading their visible playable versions.
func (e *EmbyService) metadataPage(ctx context.Context, q *gorm.DB, userID, order string, start, limit int) ([]model.MediaView, int64, error) {
	return e.metadataPageWithCount(ctx, q, userID, order, start, limit, true)
}

func (e *EmbyService) metadataPageWithCount(ctx context.Context, q *gorm.DB, userID, order string, start, limit int, countTotal bool) ([]model.MediaView, int64, error) {
	var total int64
	if countTotal {
		grouped := q.Session(&gorm.Session{}).Select("media.metadata_id").Group("media.metadata_id")
		if err := e.repo.DB.WithContext(ctx).Table("(?) AS scoped_metadata", grouped).Count(&total).Error; err != nil {
			return nil, 0, err
		}
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
	views, err := e.metadataViewsForIDs(ctx, q, userID, metadataIDs)
	return views, total, err
}

func (e *EmbyService) metadataViewsForIDs(ctx context.Context, q *gorm.DB, userID string, metadataIDs []string) ([]model.MediaView, error) {
	if len(metadataIDs) == 0 {
		return []model.MediaView{}, nil
	}
	var mediaRows []model.Media
	if err := q.Session(&gorm.Session{}).
		Where("media.metadata_id IN ?", metadataIDs).
		Order("media.created_at DESC, media.id DESC").
		Find(&mediaRows).Error; err != nil {
		return nil, err
	}
	views, err := e.mediaViewsForRows(ctx, mediaRows, userID)
	if err != nil {
		return nil, err
	}
	return preferredMetadataViews(views, metadataIDs), nil
}

const latestMediaCandidateBatchSize = 128

type latestMediaCandidate struct {
	ID         string    `gorm:"column:id"`
	MetadataID string    `gorm:"column:metadata_id"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (e *EmbyService) latestMetadataViews(ctx context.Context, q *gorm.DB, userID string, limit int) ([]model.MediaView, error) {
	metadataIDs, err := e.latestLogicalIDs(ctx, q, limit, func(_ context.Context, candidates []latestMediaCandidate) (map[string]string, error) {
		ids := make(map[string]string, len(candidates))
		for _, candidate := range candidates {
			if id := strings.TrimSpace(candidate.MetadataID); id != "" {
				ids[id] = id
			}
		}
		return ids, nil
	})
	if err != nil {
		return nil, err
	}
	return e.metadataViewsForIDs(ctx, q, userID, metadataIDs)
}

func (e *EmbyService) latestSeriesGroups(ctx context.Context, q *gorm.DB, limit int) ([]embySeriesGroup, error) {
	seriesIDs, err := e.latestLogicalIDs(ctx, q, limit, e.seriesIDsForLatestCandidates)
	if err != nil {
		return nil, err
	}
	if len(seriesIDs) == 0 {
		return []embySeriesGroup{}, nil
	}
	return e.seriesSummaries(ctx, seriesScopeQuery(q.Session(&gorm.Session{})), seriesIDs)
}

// latestLogicalIDs clears the final time boundary before applying ID tie breaks.
func (e *EmbyService) latestLogicalIDs(ctx context.Context, q *gorm.DB, limit int, resolve func(context.Context, []latestMediaCandidate) (map[string]string, error)) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	latestByID := make(map[string]time.Time, limit)
	var cursor latestMediaCandidate
	for hasCursor := false; ; hasCursor = true {
		candidates, err := e.latestMediaCandidates(ctx, q, cursor, hasCursor)
		if err != nil {
			return nil, err
		}
		resolved, err := resolve(ctx, candidates)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			metadataID := strings.TrimSpace(candidate.MetadataID)
			logicalID := strings.TrimSpace(resolved[metadataID])
			if metadataID == "" || logicalID == "" {
				continue
			}
			if createdAt, exists := latestByID[logicalID]; !exists || candidate.CreatedAt.After(createdAt) {
				latestByID[logicalID] = candidate.CreatedAt
			}
		}
		logicalIDs := orderedLatestLogicalIDs(latestByID, limit)
		if len(candidates) < latestMediaCandidateBatchSize ||
			(len(logicalIDs) == limit && candidates[len(candidates)-1].CreatedAt.Before(latestByID[logicalIDs[len(logicalIDs)-1]])) {
			return logicalIDs, nil
		}
		cursor = candidates[len(candidates)-1]
	}
}

func (e *EmbyService) latestMediaCandidates(ctx context.Context, q *gorm.DB, cursor latestMediaCandidate, hasCursor bool) ([]latestMediaCandidate, error) {
	candidatesQuery := q.Session(&gorm.Session{}).
		Select("media.id, media.metadata_id, media.created_at").
		Order("media.created_at DESC, media.id DESC").
		Limit(latestMediaCandidateBatchSize)
	if hasCursor {
		candidatesQuery = candidatesQuery.Where("(media.created_at < ? OR (media.created_at = ? AND media.id < ?))", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	var candidates []latestMediaCandidate
	if err := candidatesQuery.Scan(&candidates).Error; err != nil {
		return nil, err
	}
	return candidates, nil
}

func (e *EmbyService) seriesIDsForLatestCandidates(ctx context.Context, candidates []latestMediaCandidate) (map[string]string, error) {
	metadataIDs := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate.MetadataID); id != "" {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				metadataIDs = append(metadataIDs, id)
			}
		}
	}
	if len(metadataIDs) == 0 {
		return map[string]string{}, nil
	}
	var rows []struct {
		MetadataID string `gorm:"column:metadata_id"`
		SeriesID   string `gorm:"column:series_id"`
	}
	err := e.repo.DB.WithContext(ctx).Table("metadata_items AS scope_episode").
		Joins("JOIN metadata_items AS scope_season ON scope_season.id = scope_episode.parent_id AND scope_season.kind = 'season'").
		Joins("JOIN metadata_items AS scope_series ON scope_series.id = scope_season.parent_id AND scope_series.kind = 'series'").
		Where("scope_episode.id IN ?", metadataIDs).
		Select("scope_episode.id AS metadata_id, scope_series.id AS series_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	seriesByMetadataID := make(map[string]string, len(rows))
	for _, row := range rows {
		if metadataID, seriesID := strings.TrimSpace(row.MetadataID), strings.TrimSpace(row.SeriesID); metadataID != "" && seriesID != "" {
			seriesByMetadataID[metadataID] = seriesID
		}
	}
	return seriesByMetadataID, nil
}

func orderedLatestLogicalIDs(latestByID map[string]time.Time, limit int) []string {
	ids := make([]string, 0, len(latestByID))
	for id := range latestByID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := latestByID[ids[i]], latestByID[ids[j]]
		if left.Equal(right) {
			return ids[i] > ids[j]
		}
		return left.After(right)
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
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
	return e.seriesMetadataPageWithCount(ctx, q, userID, p, start, limit, true)
}

func (e *EmbyService) seriesMetadataPageWithCount(ctx context.Context, q *gorm.DB, userID string, p ItemsParams, start, limit int, countTotal bool) ([]embySeriesGroup, int64, error) {
	// 在可见文件层物化，防止规划器先展开全量元数据、再逐集查询媒体文件。
	// 仅保留父季和入库时间；演员、收藏等整剧条件在关联父级之后应用。
	files := q.Session(&gorm.Session{}).Select("emby_metadata.parent_id, media.created_at")
	cte := "WITH scoped_media AS MATERIALIZED (?) "
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		// 收藏范围通常很小，允许先筛整剧再找文件，避免物化全部可见文件。
		cte = "WITH scoped_media AS NOT MATERIALIZED (?) "
	} else {
		// 每季仅保留最新文件时间，避免后续父级关联和排序展开全部文件版本。
		files = files.Select("emby_metadata.parent_id, MAX(media.created_at) AS created_at").Group("emby_metadata.parent_id")
	}
	pageScope := e.repo.DB.WithContext(ctx).Table("scoped_media AS media").
		Joins("JOIN metadata_items AS scope_season ON scope_season.id = media.parent_id AND scope_season.kind = 'season'").
		Joins("JOIN metadata_items AS scope_series ON scope_series.id = scope_season.parent_id AND scope_series.kind = 'series'")
	pageScope = e.applySeriesPageFilters(ctx, pageScope, p)
	var total int64

	type seriesIDRow struct {
		SeriesID string `gorm:"column:series_id"`
		Total    int64  `gorm:"column:total"`
	}
	var idRows []seriesIDRow
	idQuery := pageScope.Session(&gorm.Session{}).
		Select("scope_series.id AS series_id").
		Group("scope_series.id").
		Order(seriesOrderSQL(p)).
		Offset(start)
	if limit > 0 {
		idQuery = idQuery.Limit(limit)
	}
	query := e.repo.DB.WithContext(ctx).Raw(cte+"?", files, idQuery)
	if countTotal {
		grouped := pageScope.Session(&gorm.Session{}).
			Select("scope_series.id AS series_id, ROW_NUMBER() OVER (ORDER BY " + seriesOrderSQL(p) + ") AS ordinal").Group("scope_series.id")
		page := e.repo.DB.Table("scoped_series").Select("series_id, ordinal").Order("ordinal").Offset(start)
		if limit > 0 {
			page = page.Limit(limit)
		}
		// 左连接保留越界空页的总数；计数和分页共享一次文件扫描。
		query = e.repo.DB.WithContext(ctx).Raw(strings.TrimSpace(cte)+", scoped_series AS MATERIALIZED (?) SELECT totals.total, COALESCE(page.series_id, '') AS series_id FROM (SELECT COUNT(*) AS total FROM scoped_series) totals LEFT JOIN (?) page ON TRUE ORDER BY page.ordinal", files, grouped, page)
	}
	if err := query.Scan(&idRows).Error; err != nil {
		return nil, 0, err
	}
	seriesIDs := make([]string, 0, len(idRows))
	for _, row := range idRows {
		total = row.Total
		if id := strings.TrimSpace(row.SeriesID); id != "" {
			seriesIDs = append(seriesIDs, id)
		}
	}
	if len(seriesIDs) == 0 {
		return []embySeriesGroup{}, total, nil
	}

	summaryScope := e.applySeriesPageFilters(ctx, seriesScopeQuery(q.Session(&gorm.Session{})), p)
	groups, err := e.seriesSummaries(ctx, summaryScope, seriesIDs)
	return groups, total, err
}

// applySeriesPageFilters 在整剧别名可用后统一约束计数、分页和摘要。
func (e *EmbyService) applySeriesPageFilters(ctx context.Context, q *gorm.DB, p ItemsParams) *gorm.DB {
	if len(p.PersonIDs) > 0 {
		credits := e.repo.DB.WithContext(ctx).Table("metadata_credits AS credit").
			Select("CASE WHEN work.kind = 'season' THEN work.parent_id ELSE work.id END").
			Joins("JOIN metadata_items AS work ON work.id = credit.metadata_id").Where("credit.person_id = ANY(?)", &p.PersonIDs)
		q = q.Where("scope_series.id IN (?)", credits)
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		favorites := e.repo.DB.WithContext(ctx).Model(&model.Favorite{}).Select("1").
			Where("favorites.user_id = ? AND favorites.metadata_id = scope_series.id", p.UserID)
		q = q.Where("EXISTS (?)", favorites)
	}
	return q
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
