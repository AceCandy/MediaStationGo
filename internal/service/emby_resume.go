package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// globalResumeItems 各来源先筛用户状态、按作品归组，再合并有界候选。
// 最终排序仍由数据库执行，保留文本排序规则及 NULL 顺序。
func (e *EmbyService) globalResumeItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	sources := []*gorm.DB{e.legacyResumeCandidates(ctx, p), e.hongGuoResumeCandidates(ctx, p)}
	if has, err := e.repo.NFO.HasMedia(ctx); err != nil {
		return nil, err
	} else if has {
		sources = append(sources, e.nfoResumeCandidates(ctx, p))
	}
	var total int64
	var pages []*gorm.DB
	for _, source := range sources {
		filtered := filterGlobalItems(e.repo.DB.WithContext(ctx).Table("(?) AS candidates", source), p)
		var count int64
		keys := filtered.Session(&gorm.Session{}).Select("resume_key").Group("resume_key")
		if err := e.repo.DB.WithContext(ctx).Table("(?) AS resume_keys", keys).Count(&count).Error; err != nil {
			return nil, err
		}
		total += count
		if count == 0 {
			continue
		}
		grouped := filtered.Session(&gorm.Session{}).Select("DISTINCT ON (resume_key) *").Order("resume_key, played_at DESC, id DESC")
		page := e.repo.DB.WithContext(ctx).Table("(?) AS resume_source", grouped).Order(globalItemsOrder(p))
		// 避免极大 StartIndex 相加溢出；越界页在总数确定后直接返回。
		if p.Limit > 0 && p.StartIndex <= int(^uint(0)>>1)-p.Limit {
			page = page.Limit(p.StartIndex + p.Limit)
		}
		pages = append(pages, page)
	}
	if total <= int64(p.StartIndex) {
		result := emptyItemsEnvelope(p.StartIndex)
		result["TotalRecordCount"] = total
		return result, nil
	}
	combined := pages[0]
	for _, page := range pages[1:] {
		combined = e.repo.DB.Raw("(?) UNION ALL (?)", combined, page)
	}
	var ids []string
	if err := e.repo.DB.WithContext(ctx).Table("(?) AS resume_page", combined).
		Order(globalItemsOrder(p)).Offset(p.StartIndex).Limit(p.Limit).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	items, err := e.globalItemPayloads(ctx, ids, p)
	if err != nil {
		return nil, err
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

// legacyResumeCandidates 活跃状态按用户和资料唯一；只关联有进度的电影和单集。
func (e *EmbyService) legacyResumeCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	q := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), p.UserID).
		Joins("JOIN playback_histories h ON h.metadata_id = media.metadata_id AND h.user_id = ? AND h.deleted_at IS NULL", p.UserID).
		Joins("LEFT JOIN metadata_items parent ON parent.id = emby_metadata.parent_id").
		Joins("LEFT JOIN metadata_items grandparent ON grandparent.id = parent.parent_id").
		Where("NOT COALESCE(h.completed,FALSE) AND h.position_ms > 0 AND emby_metadata.kind IN ('movie','episode')")
	if len(p.PersonIDs) > 0 {
		q = q.Where(`EXISTS (SELECT 1 FROM metadata_credits c WHERE c.person_id IN ?
 AND c.metadata_id = CASE WHEN emby_metadata.kind = 'episode' THEN emby_metadata.parent_id ELSE emby_metadata.id END)`, p.PersonIDs)
	}
	favorite := "FALSE"
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		q = q.Where("EXISTS (SELECT 1 FROM favorites f WHERE f.metadata_id = emby_metadata.id AND f.user_id = ? AND f.deleted_at IS NULL)", p.UserID)
		favorite = "TRUE"
	}
	return q.Select(`emby_metadata.id,
 CASE WHEN emby_metadata.kind = 'episode' THEN 'legacy:' || COALESCE(grandparent.id,emby_metadata.id) ELSE 'legacy:' || emby_metadata.id END AS resume_key,
 emby_metadata.kind, emby_metadata.title, MAX(media.created_at) AS created_at,
 COALESCE(MAX(h.watched_at),MAX(media.created_at)) AS played_at, FALSE AS played, ` + favorite + ` AS favorite,
 emby_metadata.rating, COALESCE(emby_metadata.release_date,'') AS release_date, emby_metadata.year`).
		Group("emby_metadata.id, grandparent.id")
}

// resumeSourceFiles 保持独立来源的文件权限，具体条目 NSFW 由 NFO 分支补充。
func (e *EmbyService) resumeSourceFiles(ctx context.Context, userID, source string) *gorm.DB {
	filter := e.mediaQueryFilter(ctx, userID)
	q := e.repo.DB.WithContext(ctx).Table("media AS m").Where("m.catalog_source = ?", source)
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	return q
}

func (e *EmbyService) nfoResumeCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	q := e.resumeSourceFiles(ctx, p.UserID, model.CatalogSourceNFO).
		Joins("JOIN nfo_media_bindings b ON b.media_id = m.id").
		Joins("JOIN nfo_items ni ON ni.id = b.item_id").
		Joins("LEFT JOIN nfo_items ns ON ns.id = ni.parent_id AND ni.kind = 'episode'").
		Joins("LEFT JOIN nfo_items nw ON nw.id = ns.parent_id").
		Joins("JOIN nfo_user_states s ON s.item_id = ni.id AND s.user_id = ?", p.UserID).
		Where("NOT COALESCE(s.completed,FALSE) AND s.position_ms > 0 AND ni.kind IN ('movie','episode')")
	if !e.mediaVisibility(ctx, p.UserID).IncludeNSFW {
		q = q.Where("NOT COALESCE(b.nsfw,FALSE) AND NOT COALESCE(ni.nsfw,FALSE) AND NOT COALESCE(ns.nsfw,FALSE) AND NOT COALESCE(nw.nsfw,FALSE)")
	}
	if len(p.PersonIDs) > 0 {
		q = q.Where("FALSE")
	}
	return q.Select(`'nfo-' || ni.id AS id, 'nfo-' || COALESCE(nw.id,ni.id) AS resume_key,
 ni.kind, ni.title, MAX(m.created_at) AS created_at, COALESCE(MAX(s.watched_at),MAX(m.created_at)) AS played_at,
 FALSE AS played, BOOL_OR(COALESCE(s.favorite,FALSE)) AS favorite, ni.rating, ni.release_date, ni.year`).Group("ni.id, nw.id")
}

func (e *EmbyService) hongGuoResumeCandidates(ctx context.Context, p ItemsParams) *gorm.DB {
	q := e.resumeSourceFiles(ctx, p.UserID, model.TaskSystemHongGuo).
		Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").
		Joins("JOIN hongguo_works w ON w.id = b.work_id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id").
		Joins("JOIN hongguo_user_states s ON s.user_id = ? AND s.source_id = w.source_id AND s.episode_number = COALESCE(ep.number,1)", p.UserID).
		Where("NOT s.completed AND s.position_ms > 0 AND (w.kind = 'movie' OR (w.kind = 'series' AND ep.id IS NOT NULL))")
	if len(p.PersonIDs) > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM hongguo_credits c WHERE c.work_id = w.id AND 'hg-person-' || c.person_id IN ?)", p.PersonIDs)
	}
	favorite := "FALSE"
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		q = q.Where("EXISTS (SELECT 1 FROM hongguo_user_states f WHERE f.user_id = ? AND f.source_id = w.source_id AND f.episode_number = 0 AND f.favorite)", p.UserID)
		favorite = "TRUE"
	}
	// 合法合集成员自身已满足原 lateral 查询，归组键无需再次查找合集标题。
	return q.Select(`CASE WHEN w.kind = 'movie' THEN 'hg-work-' || w.id ELSE 'hg-episode-' || ep.id END AS id,
 CASE WHEN w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0 THEN 'hongguo:group:' || w.related_album_id ELSE 'hongguo:work:' || w.source_id END AS resume_key,
 CASE WHEN w.kind = 'movie' THEN 'movie' ELSE 'episode' END AS kind,
 CASE WHEN w.kind = 'movie' THEN w.title ELSE '第' || ep.number || '集' END AS title,
 MAX(m.created_at) AS created_at, COALESCE(MAX(s.watched_at),MAX(m.created_at)) AS played_at,
 FALSE AS played, ` + favorite + ` AS favorite, w.rating, '' AS release_date, 0 AS year`).Group("1, 2, 3, 4, w.rating")
}
