package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResumePosition 保留重播断点；完成只清续播位置，不抹除真实播放快照。
func ResumePosition(position int64, completed bool) *int64 {
	if completed {
		position = 0
	}
	return &position
}

// progressUpdates 只用于自动进度；显式取消已看仍由各来源的手动入口处理。
func progressUpdates(table string) clause.Set {
	updates := clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "watched_at", "updated_at"})
	// 首次切换到替代版本时保留旧断点已达到新片长的事实，再写入新的重播位置。
	completed := table + `.completed OR EXCLUDED.completed OR (
 EXCLUDED.duration_ms > 0 AND ` + table + `.position_ms > 0 AND NOT EXISTS (SELECT 1 FROM media old WHERE old.id = ` + table + `.media_id)
 AND EXISTS (SELECT 1 FROM media current JOIN media_probe_metadata probe ON probe.media_id = current.id
 WHERE current.id = EXCLUDED.media_id AND COALESCE(current.part_group_key,'') = '' AND probe.duration_ms = EXCLUDED.duration_ms)
 AND ` + table + `.position_ms >= CASE WHEN EXCLUDED.duration_ms < 600000
 THEN GREATEST(EXCLUDED.duration_ms - 30000,0) ELSE EXCLUDED.duration_ms * 9 / 10 END)`
	return append(updates, clause.Assignment{Column: clause.Column{Name: "completed"}, Value: gorm.Expr(completed)})
}

// PlaybackStates 投影用户的有效播放状态；只读且可在分页前参与过滤与季汇总。
// 仅当原文件不再可用且存在断点时定位替代版本，避免常规列表重复探测所有版本。
func PlaybackStates(ctx context.Context, db *gorm.DB, source, userID string, filter MediaQueryFilter) *gorm.DB {
	db = db.WithContext(ctx)
	files := db.Table("media AS m")
	if len(filter.AllowedLibraryIDs) > 0 {
		files = files.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		files = files.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	table, fields := "playback_histories", "h.id,h.created_at,h.updated_at,h.deleted_at,h.user_id,h.metadata_id"
	switch source {
	case "nfo":
		table, fields = "nfo_user_states", "h.user_id,h.item_id,h.favorite,h.updated_at"
		files = files.Joins("JOIN nfo_media_bindings b ON b.media_id = m.id").
			Joins("JOIN nfo_items i ON i.id = b.item_id").
			Joins("LEFT JOIN nfo_items season ON season.id = i.parent_id AND i.kind = 'episode'").
			Joins("LEFT JOIN nfo_items series ON series.id = season.parent_id").
			Where("m.catalog_source = 'nfo' AND b.item_id = h.item_id")
		if !filter.IncludeNSFW {
			files = files.Where("NOT COALESCE(b.nsfw,FALSE) AND NOT COALESCE(i.nsfw,FALSE) AND NOT COALESCE(season.nsfw,FALSE) AND NOT COALESCE(series.nsfw,FALSE)")
		}
	case "hongguo":
		table, fields = "hongguo_user_states", "h.user_id,h.source_id,h.episode_number,h.favorite,h.updated_at"
		files = files.Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
			Joins("JOIN hongguo_works w ON w.id = b.work_id").
			Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").
			Where("m.catalog_source = 'hongguo' AND w.source_id = h.source_id AND COALESCE(ep.number,1) = h.episode_number")
	default:
		files = files.Joins("JOIN metadata_items i ON i.id = m.metadata_id").Where("m.metadata_id = h.metadata_id")
		if !filter.IncludeNSFW {
			files = files.Where("NOT COALESCE(i.nsfw,FALSE)")
		}
	}
	position := "GREATEST(COALESCE(h.resume_position_ms,CASE WHEN h.completed THEN 0 ELSE h.position_ms END),0)"
	current := files.Session(&gorm.Session{}).Where("m.id = h.media_id").Select("1")
	replacement := files.Session(&gorm.Session{}).
		Joins("LEFT JOIN media_probe_metadata probe ON probe.media_id = m.id").
		Where(position+" > 0").Where("NOT EXISTS (?)", current).
		// multipart 的位置是整组时间线，不能拿一个分段的时长推断看完。
		Select("m.id, CASE WHEN COALESCE(m.part_group_key,'') = '' THEN COALESCE(probe.duration_ms,0) ELSE 0 END AS duration_ms").
		Order("(BTRIM(COALESCE(m.strm_url,'')) <> ''), COALESCE(probe.width,0) DESC, COALESCE(probe.size_bytes,0) DESC, m.created_at DESC, m.id DESC").Limit(1)
	reached := "(replacement.duration_ms > 0 AND " + position + " >= CASE WHEN replacement.duration_ms < 600000 THEN GREATEST(replacement.duration_ms - 30000,0) ELSE replacement.duration_ms * 9 / 10 END)"
	q := db.Table(table+" AS h").Where("h.user_id = ?", userID).
		Joins("LEFT JOIN LATERAL (?) AS replacement ON TRUE", replacement).
		Select(fields + ", h.watched_at, h.resume_position_ms, COALESCE(replacement.id,h.media_id) AS media_id," +
			" CASE WHEN " + reached + " THEN 0 ELSE " + position + " END AS position_ms," +
			" COALESCE(replacement.duration_ms,h.duration_ms) AS duration_ms," +
			" (COALESCE(h.completed,FALSE) OR COALESCE(" + reached + ",FALSE)) AS completed")
	if source != "nfo" && source != "hongguo" {
		q = q.Where("h.deleted_at IS NULL")
	}
	return q
}
