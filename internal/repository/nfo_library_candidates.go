package repository

import (
	"context"

	"gorm.io/gorm"
)

// NFOWorkCandidates 供库内与全局作品列表共用；作品日期取首次创建时间，不聚合文件日期。
func (r *MediaViewRepository) NFOWorkCandidates(ctx context.Context, userID, libraryID string, filter MediaQueryFilter, played bool) *gorm.DB {
	db := r.db.WithContext(ctx)
	// 候选只需要绑定和文件事实，不加载图片、探测或展示层祖先资料。
	files := db.Table("nfo_media_bindings b").Joins("JOIN media m ON m.id=b.media_id").Where("m.catalog_source='nfo'")
	if len(filter.AllowedLibraryIDs) > 0 {
		files = files.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		files = files.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if filter.MissingPoster || filter.MissingChineseTitle {
		files = r.nfoViewQuery(ctx, filter)
	}
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
	fields := `root.id, root.title, root.kind, root.rating, root.release_date, root.year, root.created_at,
root.season_num AS season_number, root.episode_num AS episode_number, root.latest_media_added_at AS latest_at,
EXISTS (SELECT 1 FROM nfo_user_states fav WHERE fav.item_id=root.id AND fav.user_id=? AND fav.favorite) AS favorite`
	// 沿季定位分集，避免相关子查询被改成逐作品扫描整个分集目录。
	files = files.Joins(`JOIN (SELECT root.id UNION ALL
SELECT ep.id FROM nfo_items season JOIN LATERAL (
SELECT id FROM nfo_items WHERE parent_id = season.id AND kind = 'episode' OFFSET 0
) ep ON TRUE WHERE season.parent_id = root.id) leaf ON leaf.id = b.item_id`)
	// 正常入库在同一事务内建立同库条目与绑定；已维护时间证明有现存文件。
	// 只有缺失资料筛选或尚未初始化汇总的全局条目需要文件资格检查。
	if filter.MissingPoster || filter.MissingChineseTitle {
		q = q.Where("EXISTS (? OFFSET 0)", files.Session(&gorm.Session{}).Select("1"))
	} else if libraryID == "" {
		q = q.Where("root.latest_media_added_at IS NOT NULL OR EXISTS (? OFFSET 0)", files.Session(&gorm.Session{}).Select("1"))
	}
	if played {
		// 按分集定位状态，保留 Latest 的索引探测；集合型已看 UNION 会使短页读取全部用户状态。
		completed := db.Table("(?) state", PlaybackStates(ctx, r.db, "nfo", userID, filter)).
			Select("1").Where("state.item_id = b.item_id AND state.completed")
		unplayed := files.Session(&gorm.Session{}).Select("1").Where("NOT EXISTS (?)", completed)
		return q.Select(fields+", NOT EXISTS (?) AS played", userID, unplayed)
	}
	return q.Select(fields+", FALSE AS played", userID)
}
