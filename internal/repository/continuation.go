package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Continuation 是只读续播候选；下一集借用前序观看时间排序，不生成观看记录。
type Continuation struct {
	ItemID     string
	MediaID    string
	GroupID    string
	HistoryID  string
	PositionMs int64
	DurationMs int64
	WatchedAt  time.Time
	IsNext     bool
	Completed  bool
}

// ContinuationMode 区分网页续播、Emby 首页续播和仅下一集的筛选与计数需求。
type ContinuationMode int

const (
	ContinuationWeb ContinuationMode = iota
	ContinuationResume
	ContinuationNextUp
)

// Continuations 在分页前按剧归组；Emby 返回精确总数，网页只取有界候选。
func (r *HistoryRepository) Continuations(ctx context.Context, userID string, filter MediaQueryFilter, mode ContinuationMode, seriesID string, start, limit int) ([]Continuation, int64, error) {
	rows := []Continuation{}
	if userID == "" {
		return rows, 0, nil
	}
	db := r.db.WithContext(ctx)
	sources := []string{"legacy"}
	for _, source := range []string{"nfo", "hongguo"} {
		var exists bool
		// 固定来源字面量让预编译计划可利用来源索引。
		if err := db.Raw("SELECT EXISTS(SELECT 1 FROM media WHERE catalog_source = '" + source + "')").Scan(&exists).Error; err != nil {
			return nil, 0, err
		}
		if exists {
			sources = append(sources, source)
		}
	}
	var total int64
	var pages []*gorm.DB
	for _, source := range sources {
		q := r.continuationSource(ctx, userID, filter, source, mode, seriesID)
		if mode != ContinuationWeb {
			var count int64
			if err := db.Table("(?) AS candidates", q).Count(&count).Error; err != nil {
				return nil, 0, err
			}
			total += count
		}
		page := db.Table("(?) AS candidates", q).Order("watched_at DESC, item_id DESC")
		if limit > 0 {
			page = page.Limit(start + limit)
		}
		pages = append(pages, page)
	}
	combined := pages[0]
	for _, page := range pages[1:] {
		combined = db.Raw("(?) UNION ALL (?)", combined, page)
	}
	q := db.Table("(?) AS continuation_page", combined).Order("watched_at DESC, item_id DESC").Offset(start)
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Scan(&rows).Error
	return rows, total, err
}

// continuationSource 先归组有状态的可见条目，再按组定位后续单集，避免展开全目录及逐剧请求。
func (r *HistoryRepository) continuationSource(ctx context.Context, userID string, filter MediaQueryFilter, source string, mode ContinuationMode, seriesID string) *gorm.DB {
	db := r.db.WithContext(ctx)
	q := db.Table("media AS m")
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	var projection, scope string
	threshold := int64(20000)
	switch source {
	case "nfo":
		q = q.Joins("JOIN nfo_media_bindings b ON b.media_id = m.id").
			Joins("JOIN nfo_items i ON i.id = b.item_id").
			Joins("LEFT JOIN nfo_items season ON season.id = i.parent_id AND i.kind = 'episode'").
			Joins("LEFT JOIN nfo_items series ON series.id = season.parent_id").
			Joins("LEFT JOIN (?) st ON st.item_id = i.id", PlaybackStates(ctx, r.db, source, userID, filter)).
			Where("m.catalog_source = 'nfo' AND i.kind IN ('movie','episode')")
		if !filter.IncludeNSFW {
			q = q.Where("NOT COALESCE(b.nsfw,FALSE) AND NOT COALESCE(i.nsfw,FALSE) AND NOT COALESCE(season.nsfw,FALSE) AND NOT COALESCE(series.nsfw,FALSE)")
		}
		projection = `'nfo-' || i.id AS item_id, COALESCE(series.id,i.id) AS group_id,
 COALESCE('nfo-' || series.id,'') AS series_id, i.kind, COALESCE(season.season_num,0) AS season_num,
 '' AS work_order, '' AS album_id, i.episode_num, 'nfo-' || i.id AS history_id`
		scope = "series.id = a.group_id"
	case "hongguo":
		q = q.Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
			Joins("JOIN hongguo_works w ON w.id = b.work_id").
			Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").
			Joins("LEFT JOIN (?) st ON st.source_id = w.source_id AND st.episode_number = COALESCE(ep.number,1)", PlaybackStates(ctx, r.db, source, userID, filter)).
			Where("m.catalog_source = 'hongguo' AND (w.kind = 'movie' OR (w.kind = 'series' AND ep.id IS NOT NULL))")
		projection = `CASE WHEN w.kind = 'movie' THEN 'hg-work-' || w.id ELSE 'hg-episode-' || ep.id END AS item_id,
 CASE WHEN w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0 THEN 'album:' || w.related_album_id ELSE 'work:' || w.source_id END AS group_id,
 CASE WHEN w.kind = 'movie' THEN '' WHEN w.related_album_id <> '' AND w.season_index > 0 THEN 'hg-group-' || w.related_album_id ELSE 'hg-work-' || w.id END AS series_id,
 CASE WHEN w.kind = 'movie' THEN 'movie' ELSE 'episode' END AS kind,
 CASE WHEN w.related_album_id <> '' AND w.season_index > 0 THEN w.season_index ELSE 1 END AS season_num,
 w.source_id AS work_order, CASE WHEN w.related_album_id <> '' AND w.season_index > 0 THEN w.related_album_id ELSE '' END AS album_id,
 COALESCE(ep.number,1) AS episode_num, 'hg-state:' || w.source_id || ':' || COALESCE(ep.number,1) AS history_id`
		scope = "((a.album_id <> '' AND w.related_album_id = a.album_id AND w.season_index > 0) OR (a.album_id = '' AND w.source_id = a.work_order)) AND w.kind = 'series'"
		threshold = 1
	default:
		q = q.Joins("JOIN metadata_items i ON i.id = m.metadata_id").
			Joins("LEFT JOIN metadata_items season ON season.id = i.parent_id AND i.kind = 'episode' AND season.kind = 'season'").
			Joins("LEFT JOIN metadata_items series ON series.id = season.parent_id").
			Joins("LEFT JOIN (?) st ON st.metadata_id = i.id", PlaybackStates(ctx, r.db, source, userID, filter)).
			Where("i.kind IN ('movie','episode')")
		if !filter.IncludeNSFW {
			q = q.Where("NOT COALESCE(i.nsfw,FALSE)")
		}
		projection = `i.id AS item_id, COALESCE(series.id,i.id) AS group_id, COALESCE(series.id,'') AS series_id,
 i.kind, COALESCE(season.season_num,0) AS season_num, '' AS work_order, '' AS album_id, i.episode_num, COALESCE(st.id,'') AS history_id`
		scope = "series.id = a.group_id"
	}
	q = q.Select(projection + `, m.id AS media_id, COALESCE(m.id = st.media_id,FALSE) AS preferred,
 COALESCE(st.position_ms,0) AS position_ms, COALESCE(st.duration_ms,0) AS duration_ms,
 COALESCE(st.completed,FALSE) AS completed, st.watched_at`)
	if mode != ContinuationWeb {
		// Emby 混合目录的 Resume 接受任意正进度；NextUp 不能重复推荐它。
		threshold = 1
	}
	watched := db.Table("(?) AS visible", q).
		Where("watched_at IS NOT NULL AND (completed OR position_ms >= ?)", threshold)
	if seriesID != "" {
		watched = watched.Where("series_id = ?", seriesID)
	}
	next := db.Table("(?) AS episode", q.Session(&gorm.Session{}).Where(scope)).
		Where("kind = 'episode' AND NOT completed AND (season_num,work_order,episode_num) > (a.season_num,a.work_order,a.episode_num)").
		Order("season_num, work_order, episode_num, preferred DESC, media_id").Limit(1)
	anchorFilter := "TRUE"
	if mode == ContinuationNextUp {
		anchorFilter = "NOT a.resumable AND a.completed AND a.kind = 'episode'"
	}
	// 红果/NFO 批量标记逐集写时间；完成边界取最远集序，不能由写入顺序决定。
	return db.Raw(`WITH watched AS MATERIALIZED (SELECT visible.*, position_ms >= ? AS resumable FROM (?) visible), anchors AS (
	 SELECT DISTINCT ON (group_id) *, MAX(watched_at) OVER (PARTITION BY group_id) AS group_watched_at FROM watched
	 ORDER BY group_id, resumable DESC, CASE WHEN resumable THEN watched_at END DESC,
	 season_num DESC, work_order DESC, episode_num DESC, item_id DESC, preferred DESC, media_id
)
SELECT picked.item_id, picked.media_id, a.group_id, picked.history_id, picked.position_ms, picked.duration_ms, a.group_watched_at AS watched_at, NOT a.resumable AS is_next, picked.completed
FROM anchors a CROSS JOIN LATERAL (
 SELECT a.item_id, a.media_id, a.history_id, a.position_ms, a.duration_ms, a.completed WHERE a.resumable
 UNION ALL
 SELECT n.item_id, n.media_id, 'next:' || n.item_id, n.position_ms, n.duration_ms, n.completed FROM (?) n WHERE NOT a.resumable AND a.completed
) picked WHERE `+anchorFilter, threshold, watched, next)
}
