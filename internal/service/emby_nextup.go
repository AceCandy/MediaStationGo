package service

import "context"

// NextUpItems 返回已开始剧集的后续未看单集；已有断点的剧继续由 Resume 提供。
func (e *EmbyService) NextUpItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	if p.Limit <= 0 || p.Limit > 100 {
		p.Limit = 20
	}
	if p.StartIndex < 0 || p.StartIndex > int(^uint(0)>>1)-p.Limit {
		p.StartIndex = 0
	}
	candidates, total, err := e.repo.History.Continuations(ctx, p.UserID, e.mediaQueryFilter(ctx, p.UserID), true, p.ParentID, p.StartIndex, p.Limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ItemID)
	}
	items, err := e.globalItemPayloads(ctx, ids, p)
	if err != nil {
		return nil, err
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}
