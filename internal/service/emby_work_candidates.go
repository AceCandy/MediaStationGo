package service

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// globalWorkDateAggregate 保留全局 DateCreated/MAX 及默认日期并列排序，不借用库内 MIN 日期。
func globalWorkDateAggregate(p ItemsParams) string {
	order := globalItemsOrder(p)
	if strings.HasPrefix(order, "created_at ") || strings.HasPrefix(order, "release_date ") {
		return "MAX"
	}
	return ""
}

// metadataWorkFiles 按当前作品及两级后代限定文件，保留原全局层级的直接绑定资格。
func metadataWorkFiles(db *gorm.DB, files *gorm.DB) *gorm.DB {
	ids := db.Raw(`SELECT item.id UNION ALL SELECT id FROM metadata_items WHERE parent_id=item.id
UNION ALL SELECT leaf.id FROM metadata_items parent JOIN metadata_items leaf ON leaf.parent_id=parent.id
WHERE parent.parent_id=item.id`)
	return files.Where("media.metadata_id IN (?)", ids)
}

// hongGuoGlobalWorkCandidates 与库内浏览共用作品资格，合集只合并可见成员的状态。
func (e *EmbyService) hongGuoGlobalWorkCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	scoped, albums, states := e.hongGuoWorkScope(ctx, p, globalWorkDateAggregate(p))
	with, args := "WITH albums AS MATERIALIZED (?)", []any{albums}
	if states != nil {
		with += ", playback_states AS MATERIALIZED (?)"
		args = append(args, states)
	}
	q := db.Table("(?) s", scoped).
		Joins("LEFT JOIN hongguo_favorites fav ON fav.item_id=CASE WHEN s.id LIKE 'hg-group-%' THEN s.id ELSE s.source_id END AND fav.user_id=?", p.UserID).
		Select(`s.id, CASE WHEN s.id LIKE 'hg-group-%' THEN 'hongguo:group:'||SUBSTRING(s.id FROM 10)
ELSE 'hongguo:work:'||MIN(s.source_id) END AS resume_key, s.kind, s.title,
MAX(s.created_at) AS created_at, MAX(s.latest_at) AS latest_at, NULL::timestamp AS played_at,
BOOL_AND(s.played) AS played, BOOL_OR(COALESCE(fav.favorite,FALSE)) AS favorite, 0::bigint AS position_ms,
CASE WHEN s.id LIKE 'hg-group-%' THEN 0 ELSE MAX(s.rating) END AS rating, '' AS release_date, 0 AS year`).Group("s.id,s.kind,s.title")
	return db.Table("(?) candidates", db.Raw(with+" ?", append(args, q)...))
}
