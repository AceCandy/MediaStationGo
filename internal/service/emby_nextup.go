package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// NextUpItems 返回已开始剧集的后续未看单集；已有断点的剧继续由 Resume 提供。
func (e *EmbyService) NextUpItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	return e.continuationItems(ctx, p, repository.ContinuationNextUp)
}

// ResumeItemsPage 与 NextUp 共用分组、分页和响应；每部剧只返回断点或下一集。
func (e *EmbyService) ResumeItemsPage(ctx context.Context, p ItemsParams) (map[string]any, error) {
	return e.continuationItems(ctx, p, repository.ContinuationResume)
}

// continuationItems 只补全最终页，普通来源沿用续播版本和分段选择规则。
func (e *EmbyService) continuationItems(ctx context.Context, p ItemsParams, mode repository.ContinuationMode) (map[string]any, error) {
	if p.Limit <= 0 || p.Limit > 100 {
		p.Limit = 20
	}
	if p.StartIndex < 0 || p.StartIndex > int(^uint(0)>>1)-p.Limit {
		p.StartIndex = 0
	}
	candidates, total, err := e.repo.History.Continuations(ctx, p.UserID, e.mediaQueryFilter(ctx, p.UserID), mode, p.ParentID, p.StartIndex, p.Limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(candidates))
	preferredMedia := make(map[string]string, len(candidates))
	var legacy []repository.Continuation
	for _, candidate := range candidates {
		if !candidate.IsNext {
			preferredMedia[candidate.ItemID] = candidate.MediaID
		}
		if strings.HasPrefix(candidate.ItemID, "nfo-") || strings.HasPrefix(candidate.ItemID, "hg-") {
			ids = append(ids, candidate.ItemID)
		} else {
			legacy = append(legacy, candidate)
		}
	}
	items, err := e.globalItemPayloadsWithPreferredMedia(ctx, ids, p, preferredMedia)
	if err != nil {
		return nil, err
	}
	ordinary, err := e.legacyContinuationPayloads(ctx, p.UserID, legacy, p.Fields)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]map[string]any, len(items)+len(ordinary))
	for _, item := range append(items, ordinary...) {
		byID[item["Id"].(string)] = item
	}
	items = make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		if item := byID[candidate.ItemID]; item != nil {
			if !candidate.IsNext {
				item["UserData"].(map[string]any)["LastPlayedDate"] = formatEmbyDateTime(candidate.WatchedAt)
			}
			items = append(items, item)
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}
