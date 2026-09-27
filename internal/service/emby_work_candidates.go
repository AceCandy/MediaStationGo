package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
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

// legacyGlobalWorkCandidates 不展开全库文件；仅为所需排序计算当前作品的文件日期。
func (e *EmbyService) legacyGlobalWorkCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	q := repository.FilterWorkLibraries(db.Table("metadata_items item").Where("item.kind IN ('movie','series')"), "item.library_ids", e.mediaVisibility(ctx, p.UserID).AllowedLibraryIDs)
	if !e.mediaVisibility(ctx, p.UserID).IncludeNSFW {
		q = q.Where("NOT COALESCE(item.nsfw,FALSE)")
	}
	if len(p.PersonIDs) > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM metadata_credits c WHERE c.metadata_id=item.id AND c.person_id IN ?)", p.PersonIDs)
	}
	// 旧全局查询按文件的祖父节点分组；直接绑定整剧/季与分集绑定不能在优化中悄然合并。
	// 只从作品关系产生至多三个分组键，各组的可见性仍由受限文件范围确认。
	q = q.Joins(`CROSS JOIN LATERAL (
SELECT (SELECT g.id FROM metadata_items p JOIN metadata_items g ON g.id=p.parent_id WHERE p.id=item.parent_id) AS id
UNION SELECT item.parent_id WHERE EXISTS (SELECT 1 FROM metadata_items WHERE parent_id=item.id)
UNION SELECT item.id WHERE EXISTS (SELECT 1 FROM metadata_items p JOIN metadata_items leaf ON leaf.parent_id=p.id WHERE p.parent_id=item.id)
) origin`)
	files := metadataWorkFiles(db, e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID))
	files = files.Joins("LEFT JOIN metadata_items parent ON parent.id=emby_metadata.parent_id").
		Joins("LEFT JOIN metadata_items grandparent ON grandparent.id=parent.parent_id").
		Where("grandparent.id IS NOT DISTINCT FROM origin.id")
	created := "NULL::timestamp"
	if aggregate := globalWorkDateAggregate(p); aggregate != "" {
		q = q.Joins("JOIN LATERAL (?) dates ON dates.created_at IS NOT NULL", files.Session(&gorm.Session{}).Select(aggregate+"(media.created_at) AS created_at"))
		created = "dates.created_at"
	} else {
		q = q.Where("EXISTS (? OFFSET 0)", files.Session(&gorm.Session{}).Select("1"))
	}
	played := db.Raw("FALSE")
	if containsEmbyFilter(p.Filters, "IsPlayed") || containsEmbyFilter(p.Filters, "IsUnplayed") {
		completed := repository.CompletedPlaybackStates(ctx, db, "legacy", p.UserID, e.mediaQueryFilter(ctx, p.UserID))
		completed = db.Table("(?) h", completed).Select("1").Where("h.metadata_id=media.metadata_id")
		unplayed := files.Session(&gorm.Session{}).Select("1").Where("NOT EXISTS (?)", completed)
		played = db.Raw("NOT EXISTS (?)", unplayed)
	}
	return q.Select(`item.id, 'legacy:'||item.id AS resume_key, item.kind, item.title, `+created+` AS created_at,
item.latest_media_added_at AS latest_at, NULL::timestamp AS played_at, (?) AS played,
EXISTS (SELECT 1 FROM favorites f WHERE f.metadata_id=item.id AND f.user_id=? AND f.deleted_at IS NULL) AS favorite,
0::bigint AS position_ms, item.rating, COALESCE(item.release_date,'') AS release_date, item.year`, played, p.UserID)
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
		Joins("LEFT JOIN hongguo_user_states fav ON fav.source_id=s.source_id AND fav.user_id=? AND fav.episode_number=0", p.UserID).
		Select(`s.id, CASE WHEN s.id LIKE 'hg-group-%' THEN 'hongguo:group:'||SUBSTRING(s.id FROM 10)
ELSE 'hongguo:work:'||MIN(s.source_id) END AS resume_key, LOWER(s.kind) AS kind, s.title,
MAX(s.created_at) AS created_at, MAX(s.latest_at) AS latest_at, NULL::timestamp AS played_at,
BOOL_AND(s.played) AS played, BOOL_OR(COALESCE(fav.favorite,FALSE)) AS favorite, 0::bigint AS position_ms,
CASE WHEN s.id LIKE 'hg-group-%' THEN 0 ELSE MAX(s.rating) END AS rating, '' AS release_date, 0 AS year`).Group("s.id,s.kind,s.title")
	return db.Table("(?) candidates", db.Raw(with+" ?", append(args, q)...))
}

func (e *EmbyService) nfoGlobalWorkCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	played := containsEmbyFilter(p.Filters, "IsPlayed") || containsEmbyFilter(p.Filters, "IsUnplayed")
	q := e.repo.MediaView.NFOWorkCandidates(ctx, p.UserID, "", e.mediaQueryFilter(ctx, p.UserID), globalWorkDateAggregate(p), played, false)
	return e.repo.DB.WithContext(ctx).Table("(?) works", q).
		Select(`'nfo-'||id AS id, 'nfo-'||id AS resume_key, kind, title, created_at, latest_at,
NULL::timestamp AS played_at, played, favorite, 0::bigint AS position_ms, rating, release_date, year`)
}
