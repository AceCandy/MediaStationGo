package repository

import (
	"context"

	"gorm.io/gorm"
)

// NFOWorkCandidates 供库内与全局作品列表共用；dateAggregate 仅接受内部固定的 MIN/MAX，空串不读取文件日期。
func (r *MediaViewRepository) NFOWorkCandidates(ctx context.Context, userID, libraryID string, filter MediaQueryFilter, dateAggregate string, played, latest bool) *gorm.DB {
	db := r.db.WithContext(ctx)
	files := r.nfoViewQuery(ctx, filter)
	q := db.Table("nfo_items root").Where("root.kind IN ('movie','series')")
	if libraryID != "" {
		files = files.Where("m.library_id = ?", libraryID)
		q = q.Where("root.library_id = ? AND root.parent_id IS NULL AND root.latest_media_added_at IS NOT NULL", libraryID)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("root.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("root.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if latest {
		// Latest 先排序轻量条目，之后再检查可见文件与状态；不能在过滤前 LIMIT。
		q = db.Table("(? ORDER BY season_num, episode_num, latest_media_added_at DESC NULLS LAST, id OFFSET 0) root", q.Select("*"))
	}
	fields := `root.id, root.title, root.kind, root.rating, root.release_date, root.year,
root.season_num AS season_number, root.episode_num AS episode_number, root.latest_media_added_at AS latest_at,
EXISTS (SELECT 1 FROM nfo_user_states fav WHERE fav.item_id=root.id AND fav.user_id=? AND fav.favorite) AS favorite`
	// 沿季定位分集，避免相关子查询被改成逐作品扫描整个分集目录。
	files = files.Joins(`JOIN (SELECT root.id UNION ALL
SELECT ep.id FROM nfo_items season JOIN LATERAL (
SELECT id FROM nfo_items WHERE parent_id = season.id AND kind = 'episode' OFFSET 0
) ep ON TRUE WHERE season.parent_id = root.id) leaf ON leaf.id = b.item_id`)
	if dateAggregate == "" {
		q = q.Where("EXISTS (? OFFSET 0)", files.Session(&gorm.Session{}).Select("1"))
		fields += ", NULL::timestamp AS created_at"
	} else {
		q = q.Joins("JOIN LATERAL (?) stats ON stats.created_at IS NOT NULL", files.Session(&gorm.Session{}).Select(dateAggregate+"(m.created_at) AS created_at"))
		fields += ", stats.created_at"
	}
	if played {
		// 按分集定位状态，保留 Latest 的索引探测；集合型已看 UNION 会使短页读取全部用户状态。
		completed := db.Table("(?) state", PlaybackStates(ctx, r.db, "nfo", userID, filter)).
			Select("1").Where("state.item_id = ni.id AND state.completed")
		unplayed := files.Session(&gorm.Session{}).Select("1").Where("NOT EXISTS (?)", completed)
		return q.Select(fields+", NOT EXISTS (?) AS played", userID, unplayed)
	}
	return q.Select(fields+", FALSE AS played", userID)
}
