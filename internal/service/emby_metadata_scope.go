package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// favoriteAdditionTimes 按作品汇总收藏时间，避免重复收藏行放大候选和分页总数。
func (e *EmbyService) favoriteAdditionTimes(ctx context.Context, userID string) *gorm.DB {
	return e.repo.DB.WithContext(ctx).Model(&model.Favorite{}).
		Where("user_id = ?", userID).Select("metadata_id, MAX(created_at) AS created_at").Group("metadata_id")
}

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

func (e *EmbyService) latestMetadataViews(ctx context.Context, q *gorm.DB, userID string, libraryIDs []string, limit int) ([]model.MediaView, error) {
	ids, err := e.latestMetadataIDs(ctx, q, userID, libraryIDs, limit)
	if err != nil {
		return nil, err
	}
	return e.metadataViewsForIDs(ctx, q, userID, ids)
}

// latestMetadataIDs 让混合 Latest 先合并身份，仅水合最终页；保留普通来源的原筛选和并列顺序。
func (e *EmbyService) latestMetadataIDs(ctx context.Context, q *gorm.DB, userID string, libraryIDs []string, limit int) ([]string, error) {
	db := e.repo.DB.WithContext(ctx)
	recent := e.orderedWorkLibraryScope(ctx, db.Table("metadata_items recent").Where("recent.latest_media_added_at IS NOT NULL"),
		"recent", ItemsParams{UserID: userID}, libraryIDs).
		Select("recent.id, ROW_NUMBER() OVER (ORDER BY recent.latest_media_added_at DESC NULLS LAST, recent.id DESC) AS ordinal").
		Order("recent.latest_media_added_at DESC NULLS LAST, recent.id DESC")
	files := q.Session(&gorm.Session{}).Select("1").Where("media.metadata_id=recent.id")
	eligible := db.Table("work_batch recent").Select("recent.ordinal").Where("EXISTS (? OFFSET 0)", files)
	metadataIDs, _, err := e.filteredWorkBatchPage(ctx, recent, eligible, 0, limit, false)
	if err != nil {
		return nil, err
	}
	return metadataIDs, nil
}

func (e *EmbyService) latestSeriesGroups(ctx context.Context, q *gorm.DB, libraryIDs []string, limit int) ([]embySeriesGroup, error) {
	db := e.repo.DB.WithContext(ctx)
	ordered := db.Table("metadata_items").Where("kind = 'series' AND latest_media_added_at IS NOT NULL")
	if len(libraryIDs) > 0 {
		ordered = db.Table("(? OFFSET 0) metadata_items", repository.FilterWorkLibraries(ordered, "library_ids", libraryIDs))
	}
	ordered = ordered.
		Select("id, ROW_NUMBER() OVER (ORDER BY latest_media_added_at DESC NULLS LAST, id DESC) AS ordinal").
		Order("latest_media_added_at DESC NULLS LAST, id DESC")
	eligible := db.Table("work_batch recent").Select("recent.ordinal").
		Where("EXISTS (? OFFSET 0)", e.seriesWorkFiles(db, q).Select("1"))
	seriesIDs, _, err := e.filteredWorkBatchPage(ctx, ordered, eligible, 0, limit, false)
	if err != nil {
		return nil, err
	}
	if len(seriesIDs) == 0 {
		return []embySeriesGroup{}, nil
	}
	return e.seriesSummaries(ctx, seriesScopeQuery(q.Session(&gorm.Session{})), seriesIDs)
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
	return e.seriesWorkPage(ctx, q, p, start, limit)
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
	case "random":
		return embyRandomOrder(p, "scope_series.id") + ", scope_series.id"
	case "sortname", "name":
	case "productionyear":
		if !strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending") {
			dir = "DESC"
		}
		expression = "MAX(COALESCE(scope_series.year, 0))"
	case "datecreated":
		expression = "MAX(media.created_at)"
	case "datelastcontentadded":
		return fmt.Sprintf("MAX(scope_series.latest_media_added_at) %s NULLS LAST, scope_series.id %s", dir, dir)
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
	case "random":
		return embyRandomOrder(p, "media.metadata_id") + ", media.metadata_id"
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
	case "datelastcontentadded":
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending") {
			dir = "ASC"
		}
		return fmt.Sprintf("MAX(emby_metadata.latest_media_added_at) %s NULLS LAST, media.metadata_id %s", dir, dir)
	case "productionyear":
		if strings.EqualFold(firstCSVValue(p.SortOrder), "Ascending") {
			dir = "ASC"
		}
		expression = "MAX(COALESCE(emby_metadata.year, 0))"
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
