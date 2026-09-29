package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// mixedLatestItems 保留无 NFO 时普通直接绑定身份及来源内并列截断，再合并最终页。
// 不计总数，红果状态按 50 个候选补取；两来源详情只加载合并后选中的身份。
func (e *EmbyService) mixedLatestItems(ctx context.Context, userID string, limit int, isPlayed bool, fields []string) ([]map[string]any, error) {
	db := e.repo.DB.WithContext(ctx)
	files := e.applyLatestPlayedFilter(ctx, e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), userID), userID, isPlayed)
	legacyIDs, err := e.latestMetadataIDs(ctx, files, userID, nil, limit)
	if err != nil {
		return nil, err
	}
	legacy := db.Table("metadata_items").Where("id IN ?", legacyIDs).
		Select("id,kind,latest_media_added_at AS latest_at,'legacy' AS source,NULL::text AS origin_id,NULL::text[] AS work_ids")
	p := ItemsParams{UserID: userID, Limit: limit, Fields: fields, SortBy: "DateLastContentAdded", SortOrder: "Descending"}
	source := e.hongGuoGlobalCandidates(ctx, p).Where("kind IN ('movie','episode')").
		Select("id,kind,latest_at,'hongguo' AS source,NULL::text AS origin_id,work_ids")
	p.Filters = []string{"IsUnplayed"}
	if isPlayed {
		p.Filters = []string{"IsPlayed"}
	}
	order := "latest_at DESC NULLS LAST,id ASC"
	candidates := db.Table("(?) combined", db.Raw("? UNION ALL ?", legacy, source)).
		Select("*,ROW_NUMBER() OVER (ORDER BY " + order + ") AS ordinal").Order(order)
	// 普通身份已按直接绑定文件筛过；不能再用全局祖先资格扩展它。
	eligible := db.Raw("? UNION ALL ?", db.Table("work_batch").Select("ordinal").Where("source='legacy'"),
		e.globalBatchEligibility(ctx, p).Where("item.source='hongguo'"))
	ids, _, err := e.filteredWorkBatchPage(ctx, candidates, eligible, 0, limit, false)
	if err != nil {
		return nil, err
	}
	return e.loadGlobalItemPayloads(ctx, ids, p, nil, func(ctx context.Context, ids []string, p ItemsParams) ([]map[string]any, error) {
		views, err := e.metadataViewsForIDs(ctx, files.Session(&gorm.Session{Context: ctx}), userID, ids)
		if err != nil {
			return nil, err
		}
		items := e.payloadsForViewsWithFields(ctx, views, userID, p.Fields)
		for _, item := range items {
			if item["Type"] == "Movie" {
				item["ParentId"] = ""
			}
		}
		return items, nil
	})
}
