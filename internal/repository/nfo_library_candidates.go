package repository

import (
	"context"

	"gorm.io/gorm"
)

// NFOLibraryCandidates 只投影顶层作品及分页所需字段；展示状态由页内节点补充。
func (r *MediaViewRepository) NFOLibraryCandidates(ctx context.Context, userID, libraryID string, filter MediaQueryFilter, dates, played bool) *gorm.DB {
	db := r.db.WithContext(ctx)
	files := r.nfoViewQuery(ctx, filter).Where("m.library_id = ?", libraryID)
	q := db.Table("nfo_items root").Where("root.library_id = ? AND root.kind IN ('movie','series') AND root.parent_id IS NULL", libraryID)
	fields := "root.id, root.title, root.kind, root.season_num AS season_number, root.episode_num AS episode_number"
	if !dates && !played {
		files = files.Joins("JOIN (SELECT root.id UNION ALL SELECT ep.id FROM nfo_items season JOIN nfo_items ep ON ep.parent_id = season.id WHERE season.parent_id = root.id AND ep.kind = 'episode') leaf ON leaf.id = b.item_id")
		return q.Select(fields).Where("EXISTS (? OFFSET 0)", files.Select("1"))
	}
	selection := "COALESCE(nw.id,ni.id) AS id"
	if dates {
		selection += ", MIN(m.created_at) AS created_at"
		fields += ", stats.created_at"
	}
	if played {
		files = files.Joins("LEFT JOIN (?) state ON state.item_id = ni.id", PlaybackStates(ctx, r.db, "nfo", userID, filter))
		selection += ", BOOL_AND(COALESCE(state.completed,FALSE)) AS played"
		fields += ", stats.played"
	}
	return q.Joins("JOIN (?) stats ON stats.id = root.id", files.Select(selection).Group("COALESCE(nw.id,ni.id)")).Select(fields)
}
