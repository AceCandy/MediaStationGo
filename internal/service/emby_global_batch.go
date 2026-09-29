package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

// globalBatchCandidates 先合并轻量身份和原排序字段，状态只在 work_batch 内检查。
// origin_id 保留普通资料旧祖父分组；相同 ID 的多行不可按 ID 去重。
func (e *EmbyService) globalBatchCandidates(ctx context.Context, p ItemsParams, hasNFO bool) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	base := p
	base.Filters = nil
	for _, filter := range p.Filters {
		if !strings.EqualFold(filter, "IsPlayed") && !strings.EqualFold(filter, "IsUnplayed") {
			base.Filters = append(base.Filters, filter)
		}
	}
	legacy := e.legacyGlobalBatchCandidates(ctx, base)
	var source *gorm.DB
	if containsOnlyFavoriteItemTypes(globalItemKinds(p)) && !strings.HasPrefix(globalItemsOrder(p), "played_at ") {
		scoped, albums, _ := e.hongGuoWorkScope(ctx, base, globalWorkDateAggregate(p))
		works := db.Table("(?) s", scoped).
			Joins("LEFT JOIN hongguo_favorites fav ON fav.item_id=CASE WHEN s.id LIKE 'hg-group-%' THEN s.id ELSE s.source_id END AND fav.user_id=?", p.UserID).
			Select(`s.id, s.kind, s.title, MAX(s.created_at) AS created_at, MAX(s.latest_at) AS latest_at,
NULL::timestamp AS played_at, BOOL_OR(COALESCE(fav.favorite,FALSE)) AS favorite,
CASE WHEN s.id LIKE 'hg-group-%' THEN 0 ELSE MAX(s.rating) END AS rating, '' AS release_date, 0 AS year,
NULL::text AS origin_id, ARRAY_AGG(s.work_id::text) AS work_ids, 'hongguo' AS source`).Group("s.id,s.kind,s.title")
		source = db.Table("(?) works", db.Raw("WITH albums AS MATERIALIZED (?) ?", albums, works))
	} else {
		source = e.hongGuoGlobalCandidates(ctx, base).
			Select("id,kind,title,created_at,latest_at,played_at,favorite,rating,release_date,year,NULL::text AS origin_id,work_ids,'hongguo' AS source")
	}
	source = e.hongGuoPersonFilter(ctx, source, p.UserID, "", p.PersonIDs)
	combined := db.Raw("? UNION ALL ?", legacy, source)
	if hasNFO {
		filter := e.mediaQueryFilter(ctx, p.UserID)
		local := db.Table("nfo_items item").
			Select(`'nfo-'||item.id AS id,item.kind,item.title,item.created_at,item.latest_media_added_at AS latest_at,
NULL::timestamp AS played_at,EXISTS (SELECT 1 FROM nfo_user_states fav WHERE fav.item_id=item.id AND fav.user_id=? AND fav.favorite) AS favorite,
item.rating,item.release_date,item.year,NULL::text AS origin_id,NULL::text[] AS work_ids,'nfo' AS source`, p.UserID)
		if len(filter.AllowedLibraryIDs) > 0 {
			local = local.Where("item.library_id = ANY(?)", &filter.AllowedLibraryIDs)
		}
		if len(filter.HiddenLibraryIDs) > 0 {
			local = local.Where("item.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
		}
		if len(p.PersonIDs) > 0 {
			local = local.Where("FALSE")
		}
		if strings.HasPrefix(globalItemsOrder(p), "played_at ") {
			// 播放时间属于排序输入，不能到取批后才计算。
			local = e.nfoNodes(ctx, p.UserID, "").Select(`id,LOWER(kind) AS kind,title,created_at,latest_at,
COALESCE(played_at,file_latest_at) AS played_at,favorite,rating,release_date,year,
NULL::text AS origin_id,NULL::text[] AS work_ids,'nfo' AS source`)
			if len(p.PersonIDs) > 0 {
				local = local.Where("FALSE")
			}
		}
		combined = db.Raw("? UNION ALL ?", combined, local)
	}
	q := filterGlobalItems(db.Table("(?) combined", combined), base)
	order := globalItemsOrder(p) + ", source, origin_id NULLS FIRST"
	return q.Select("*,ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal").Order(order)
}

// legacyGlobalBatchCandidates 从资料关系生成旧分组，仅为文件依赖排序读取文件。
func (e *EmbyService) legacyGlobalBatchCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	q := e.workLibraryScope(ctx, db.Table("metadata_items item"), "item.library_ids", p).
		Where("item.kind IN ?", globalItemKinds(p)).
		Joins(`CROSS JOIN LATERAL (
SELECT (SELECT g.id FROM metadata_items p JOIN metadata_items g ON g.id=p.parent_id WHERE p.id=item.parent_id) AS id
UNION SELECT item.parent_id WHERE EXISTS (SELECT 1 FROM metadata_items WHERE parent_id=item.id)
UNION SELECT item.id WHERE EXISTS (SELECT 1 FROM metadata_items p JOIN metadata_items leaf ON leaf.parent_id=p.id WHERE p.parent_id=item.id)
) origin`)
	if len(p.PersonIDs) > 0 {
		q = q.Where(`EXISTS (SELECT 1 FROM metadata_credits c WHERE c.person_id IN ?
AND c.metadata_id=CASE WHEN item.kind='episode' THEN item.parent_id ELSE item.id END)`, p.PersonIDs)
	}
	files := e.globalLegacyFiles(ctx, p, "origin.id")
	created, playedAt := "NULL::timestamp", "NULL::timestamp"
	if globalWorkDateAggregate(p) != "" {
		q = q.Joins("LEFT JOIN LATERAL (?) dates ON TRUE", files.Select("MAX(media.created_at) AS created_at"))
		created = "dates.created_at"
	}
	if strings.HasPrefix(globalItemsOrder(p), "played_at ") {
		states := repository.PlaybackStates(ctx, db, "legacy", p.UserID, e.mediaQueryFilter(ctx, p.UserID))
		q = q.Joins("LEFT JOIN LATERAL (?) dates ON TRUE", files.Joins("LEFT JOIN (?) h ON h.metadata_id=media.metadata_id", states).
			Select("COALESCE(MAX(h.watched_at),MAX(media.created_at)) AS played_at"))
		playedAt = "dates.played_at"
	}
	return q.Select(`item.id,item.kind,item.title,`+created+` AS created_at,item.latest_media_added_at AS latest_at,`+playedAt+` AS played_at,
EXISTS (SELECT 1 FROM favorites f WHERE f.metadata_id=item.id AND f.user_id=? AND f.deleted_at IS NULL) AS favorite,
item.rating,COALESCE(item.release_date,'') AS release_date,item.year,
origin.id AS origin_id,NULL::text[] AS work_ids,'legacy' AS source`, p.UserID)
}

// globalLegacyFiles 保留直接绑定与后代文件的原祖父分组，origin 只接受固定别名。
func (e *EmbyService) globalLegacyFiles(ctx context.Context, p ItemsParams, origin string) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	return metadataWorkFiles(db, e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID)).
		Joins("LEFT JOIN metadata_items parent ON parent.id=emby_metadata.parent_id").
		Joins("LEFT JOIN metadata_items grandparent ON grandparent.id=parent.parent_id").
		Where("grandparent.id IS NOT DISTINCT FROM " + origin)
}

// globalBatchEligibility 的三个文件范围均关联当前候选，不枚举整库展示节点。
func (e *EmbyService) globalBatchEligibility(ctx context.Context, p ItemsParams) *gorm.DB {
	db := e.repo.DB.WithContext(ctx)
	filter := e.mediaQueryFilter(ctx, p.UserID)
	legacy := e.globalLegacyFiles(ctx, p, "item.origin_id")
	localID := "SUBSTRING(item.id FROM 5)"
	local := e.repo.MediaView.NFOCandidateFiles(ctx, filter, localID).
		Where("ni.id=" + localID + " OR ns.id=" + localID + " OR nw.id=" + localID)
	source := e.hongGuoVisibleFiles(ctx, p.UserID, "").
		Joins("JOIN hongguo_media_bindings b ON b.media_id=m.id").
		// 从本批绑定定位源作品，避免为每个候选重扫全部作品资料。
		Joins("JOIN LATERAL (SELECT id,source_id FROM hongguo_works WHERE id=b.work_id OFFSET 0) w ON TRUE").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id").
		Where("b.work_id = ANY(item.work_ids)").
		Where("item.kind<>'episode' OR b.episode_id=SUBSTRING(item.id FROM 12)")
	return db.Table("work_batch item").Select("item.ordinal").Where(`CASE WHEN item.source='legacy' THEN EXISTS (?)
WHEN item.source='nfo' THEN EXISTS (?) ELSE EXISTS (?) END`,
		e.workBatchFileEligibility(ctx, p, legacy, "legacy", "metadata_id=media.metadata_id"),
		e.workBatchFileEligibility(ctx, p, local, "nfo", "item_id=b.item_id"),
		e.workBatchFileEligibility(ctx, p, source, "hongguo", "source_id=w.source_id AND episode_number=COALESCE(ep.number,1)"))
}
