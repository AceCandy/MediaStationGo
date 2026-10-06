package repository

import (
	"context"
	"strings"
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
	switch {
	case strings.HasPrefix(seriesID, "nfo-"):
		sources = []string{"nfo"}
	case strings.HasPrefix(seriesID, "hga-"):
		sources = []string{"huangguoai"}
	case strings.HasPrefix(seriesID, "hg-"):
		sources = []string{"hongguo"}
	case seriesID == "":
		for _, source := range []string{"nfo", "hongguo", "huangguoai"} {
			var exists bool
			// 固定来源字面量让预编译计划可利用来源索引。
			if err := db.Raw("SELECT EXISTS(SELECT 1 FROM media WHERE catalog_source = '" + source + "')").Scan(&exists).Error; err != nil {
				return nil, 0, err
			}
			if exists {
				sources = append(sources, source)
			}
		}
	}
	var total int64
	var pages []*gorm.DB
	for _, source := range sources {
		q := r.continuationSource(ctx, userID, filter, source, mode, seriesID)
		if mode != ContinuationWeb {
			pages = append(pages, q)
			continue
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
	if mode != ContinuationWeb {
		// 精确总数和当前页共享一次有效候选计算，越界空页也保留总数。
		page := db.Table("continuation_candidates").Order("watched_at DESC, item_id DESC").Offset(start)
		if limit > 0 {
			page = page.Limit(limit)
		}
		var result []struct {
			Continuation
			Total int64
		}
		err := db.Raw(`WITH continuation_candidates AS MATERIALIZED (?)
SELECT totals.total, page.* FROM (SELECT COUNT(*) AS total FROM continuation_candidates) totals
LEFT JOIN (?) page ON TRUE ORDER BY page.watched_at DESC, page.item_id DESC`, combined, page).Scan(&result).Error
		for _, row := range result {
			total = row.Total
			if row.ItemID != "" {
				rows = append(rows, row.Continuation)
			}
		}
		return rows, total, err
	}
	q := db.Table("(?) AS continuation_page", combined).Order("watched_at DESC, item_id DESC").Offset(start)
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Scan(&rows).Error
	return rows, total, err
}

// continuationSource 从当前用户历史定位可见条目，再归组续播或下一集，不展开未看目录。
func (r *HistoryRepository) continuationSource(ctx context.Context, userID string, filter MediaQueryFilter, source string, mode ContinuationMode, seriesID string) *gorm.DB {
	db := r.db.WithContext(ctx)
	q := db.Table("media AS m")
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	var projection, scope, stateIdentity string
	threshold := int64(20000)
	switch source {
	case "nfo":
		q = q.Joins("JOIN nfo_media_bindings b ON b.media_id = m.id").
			Joins("JOIN nfo_items i ON i.id = b.item_id").
			Joins("LEFT JOIN nfo_items season ON season.id = i.parent_id AND i.kind = 'episode'").
			Joins("LEFT JOIN nfo_items series ON series.id = season.parent_id").
			Where("m.catalog_source = 'nfo' AND i.kind IN ('movie','episode')")
		if len(filter.AllowedLibraryIDs) > 0 {
			q = q.Where("i.library_id = ANY(?)", &filter.AllowedLibraryIDs)
		}
		if len(filter.HiddenLibraryIDs) > 0 {
			q = q.Where("i.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
		}
		projection = `'nfo-' || i.id AS item_id, COALESCE(series.id,i.id) AS group_id,
 COALESCE('nfo-' || series.id,'') AS series_id, i.kind, COALESCE(season.season_num,0) AS season_num,
 '' AS work_order, '' AS album_id, i.episode_num, 'nfo-' || i.id AS history_id`
		scope = "series.id = a.group_id"
		stateIdentity = "st.item_id = i.id"
	case "huangguoai":
		q = q.Joins("JOIN huangguoai_media_bindings b ON b.media_id=m.id").Joins("JOIN huangguoai_works w ON w.id=b.work_id").Joins("JOIN huangguoai_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id").Where("m.catalog_source='huangguoai' AND w.projection_error='' AND (w.kind='series' OR ep.number=1)")
		q = FilterVisibleWorkLibraries(db, q, "w.library_ids", nil, filter)
		projection = `CASE WHEN w.kind='movie' THEN 'hga-work-' || w.id ELSE 'hga-episode-' || ep.id END AS item_id,
 'hga-work:' || w.source_id AS group_id,CASE WHEN w.kind='series' THEN 'hga-group-' || w.source_id ELSE '' END AS series_id,
 CASE WHEN w.kind='movie' THEN 'movie' ELSE 'episode' END AS kind,1 AS season_num,
 w.source_id AS work_order,'' AS album_id,ep.number AS episode_num,'hga-state:' || w.source_id || ':' || ep.number AS history_id`
		scope = "w.source_id=a.work_order AND w.kind='series'"
		stateIdentity = "st.source_id=w.source_id AND st.episode_number=ep.number"
		threshold = 1
	case "hongguo":
		q = q.Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
			Joins("JOIN hongguo_works w ON w.id = b.work_id").
			Joins("JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").
			Where("m.catalog_source = 'hongguo'")
		q = FilterVisibleWorkLibraries(db, q, "w.library_ids", nil, filter)
		projection = `'hg-episode-' || ep.id AS item_id,
 CASE WHEN w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0 THEN 'album:' || w.related_album_id ELSE 'work:' || w.source_id END AS group_id,
 CASE WHEN w.related_album_id <> '' AND w.season_index > 0 THEN 'hg-group-' || w.related_album_id ELSE 'hg-work-' || w.id END AS series_id,
 'episode' AS kind,
 CASE WHEN w.related_album_id <> '' AND w.season_index > 0 THEN w.season_index ELSE 1 END AS season_num,
 w.source_id AS work_order, CASE WHEN w.related_album_id <> '' AND w.season_index > 0 THEN w.related_album_id ELSE '' END AS album_id,
 COALESCE(ep.number,1) AS episode_num, 'hg-state:' || w.source_id || ':' || COALESCE(ep.number,1) AS history_id`
		scope = "((a.album_id <> '' AND w.related_album_id = a.album_id AND w.season_index > 0) OR (a.album_id = '' AND w.source_id = a.work_order)) AND w.kind = 'series'"
		stateIdentity = "st.source_id = w.source_id AND st.episode_number = COALESCE(ep.number,1)"
		threshold = 1
	default:
		q = q.Joins("JOIN metadata_items i ON i.id = m.metadata_id").
			Joins("LEFT JOIN metadata_items season ON season.id = i.parent_id AND i.kind = 'episode' AND season.kind = 'season'").
			Joins("LEFT JOIN metadata_items series ON series.id = season.parent_id").
			Where("i.kind IN ('movie','episode')")
		q = FilterVisibleWorkLibraries(db, q, "i.library_ids", nil, filter)
		projection = `i.id AS item_id, COALESCE(series.id,i.id) AS group_id, COALESCE(series.id,'') AS series_id,
 i.kind, COALESCE(season.season_num,0) AS season_num, '' AS work_order, '' AS album_id, i.episode_num, COALESCE(st.id,'') AS history_id`
		scope = "series.id = a.group_id"
		stateIdentity = "st.metadata_id = i.id"
	}
	q = q.Select(projection + `, m.id AS media_id, COALESCE(m.id = st.media_id,FALSE) AS preferred,
 COALESCE(st.position_ms,0) AS position_ms, COALESCE(st.duration_ms,0) AS duration_ms,
 COALESCE(st.completed,FALSE) AS completed, st.watched_at`)
	if mode != ContinuationWeb {
		// Emby 混合目录的 Resume 接受任意正进度；NextUp 不能重复推荐它。
		threshold = 1
	}
	effectiveStates := PlaybackStates(ctx, r.db, source, userID, filter)
	if seriesID != "" {
		// 在有效状态和文件解析前收窄历史，不影响无历史的下一集查询。
		switch source {
		case "huangguoai":
			effectiveStates = effectiveStates.Where("h.source_id=?", strings.TrimPrefix(seriesID, "hga-group-"))
		case "hongguo":
			works := db.Table("hongguo_works").Select("source_id").Where("kind = 'series'")
			if strings.HasPrefix(seriesID, "hg-group-") {
				works = works.Where("related_album_id = ? AND season_index > 0", strings.TrimPrefix(seriesID, "hg-group-"))
			} else {
				works = works.Where("id = ?", strings.TrimPrefix(seriesID, "hg-work-"))
			}
			effectiveStates = effectiveStates.Where("h.source_id IN (?)", works)
		case "nfo":
			effectiveStates = effectiveStates.Where(`h.item_id IN (SELECT ep.id FROM nfo_items ep
 JOIN nfo_items season ON season.id = ep.parent_id WHERE ep.kind = 'episode' AND season.parent_id = ?)`, strings.TrimPrefix(seriesID, "nfo-"))
		default:
			effectiveStates = effectiveStates.Where(`h.metadata_id IN (SELECT ep.id FROM metadata_items ep
 JOIN metadata_items season ON season.id = ep.parent_id AND season.kind = 'season'
 WHERE ep.kind = 'episode' AND season.parent_id = ?)`, seriesID)
		}
	}
	// 历史与下一集共用一次有效状态投影，避免逐个候选重复展开替代版本检查。
	states := db.Table("continuation_states AS h")
	historyItems := q.Session(&gorm.Session{}).Where(stateIdentity)
	if source == "hongguo" {
		// 按作品与集号定位历史对应的分集版本。
		historyItems = historyItems.Where("ep.number = st.episode_number")
	}
	// 只对历史中的逻辑身份查可见版本；边界阻止重新展开整个媒体目录。
	historyItems = db.Raw("? OFFSET 0", historyItems)
	watched := db.Table("(?) AS st", states.Session(&gorm.Session{}).Where("h.watched_at IS NOT NULL")).
		Joins("JOIN LATERAL (?) AS history_item ON TRUE", historyItems).
		Select("history_item.*").Where("st.completed OR st.position_ms >= ?", threshold)
	if seriesID != "" {
		watched = watched.Where("series_id = ?", seriesID)
	}
	if source == "hongguo" {
		stateIdentity = "st.source_id = w.source_id AND st.episode_number = ep.number"
	}
	nextItems := q.Session(&gorm.Session{}).Joins("LEFT JOIN (?) st ON "+stateIdentity, states).Where(scope)
	next := db.Table("(?) AS episode", nextItems).
		Where("kind = 'episode' AND NOT completed AND (season_num,work_order,episode_num) > (a.season_num,a.work_order,a.episode_num)").
		Order("season_num, work_order, episode_num, preferred DESC, media_id").Limit(1)
	anchorFilter := "TRUE"
	if mode == ContinuationNextUp {
		anchorFilter = "NOT a.resumable AND a.completed AND a.kind = 'episode'"
	}
	// 红果/NFO 批量标记逐集写时间；完成边界取最远集序，不能由写入顺序决定。
	return db.Raw(`WITH continuation_states AS MATERIALIZED (?),
watched AS MATERIALIZED (SELECT visible.*, position_ms >= ? AS resumable FROM (?) visible), anchors AS (
	 SELECT DISTINCT ON (group_id) *, MAX(watched_at) OVER (PARTITION BY group_id) AS group_watched_at FROM watched
	 ORDER BY group_id, resumable DESC, CASE WHEN resumable THEN watched_at END DESC,
	 season_num DESC, work_order DESC, episode_num DESC, item_id DESC, preferred DESC, media_id
)
SELECT picked.item_id, picked.media_id, a.group_id, picked.history_id, picked.position_ms, picked.duration_ms, a.group_watched_at AS watched_at, NOT a.resumable AS is_next, picked.completed
FROM anchors a CROSS JOIN LATERAL (
 SELECT a.item_id, a.media_id, a.history_id, a.position_ms, a.duration_ms, a.completed WHERE a.resumable
 UNION ALL
 SELECT n.item_id, n.media_id, 'next:' || n.item_id, n.position_ms, n.duration_ms, n.completed FROM (?) n WHERE NOT a.resumable AND a.completed
) picked WHERE `+anchorFilter, effectiveStates, threshold, watched, next)
}
