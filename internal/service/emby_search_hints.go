package service

import (
	"context"
	"strings"
)

// SearchHints 返回 Emby 客户端搜索框使用的轻量条目投影。
func (e *EmbyService) SearchHints(ctx context.Context, p ItemsParams) (map[string]any, error) {
	p.SearchTerm = strings.TrimSpace(p.SearchTerm)
	if p.SearchTerm == "" {
		return map[string]any{"SearchHints": []map[string]any{}, "TotalRecordCount": 0}, nil
	}
	if p.Limit <= 0 || p.Limit > 50 {
		p.Limit = 20
	}
	result, err := e.Items(ctx, p)
	if err != nil {
		return nil, err
	}
	rawItems, _ := result["Items"].([]map[string]any)
	hints := make([]map[string]any, 0, len(rawItems))
	for _, item := range rawItems {
		id, _ := item["Id"].(string)
		name, _ := item["Name"].(string)
		if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
			continue
		}
		hint := map[string]any{
			"ItemId":            id,
			"Id":                id,
			"Name":              name,
			"Type":              item["Type"],
			"MediaType":         item["MediaType"],
			"ProductionYear":    item["ProductionYear"],
			"IndexNumber":       item["IndexNumber"],
			"ParentIndexNumber": item["ParentIndexNumber"],
			"RunTimeTicks":      item["RunTimeTicks"],
			"MatchedTerm":       p.SearchTerm,
		}
		if tags, ok := item["ImageTags"].(map[string]string); ok {
			hint["PrimaryImageTag"] = tags["Primary"]
		}
		hints = append(hints, hint)
	}
	var total int64
	switch value := result["TotalRecordCount"].(type) {
	case int64:
		total = value
	case int:
		total = int64(value)
	case int32:
		total = int64(value)
	}
	return map[string]any{
		"SearchHints":      hints,
		"TotalRecordCount": total,
		"StartIndex":       p.StartIndex,
	}, nil
}
