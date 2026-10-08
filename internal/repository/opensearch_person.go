package repository

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// SearchPersonWorkIDs 按唯一作品 ID 游标召回，不受 100 条候选或深分页窗口限制。
func (b *OpenSearchMediaBackend) SearchPersonWorkIDs(ctx context.Context, name string, personIDs []string, filter MetadataSearchFilter) ([]string, error) {
	if b.documentType != "metadata" {
		return nil, errors.New("person search requires ordinary metadata index")
	}
	if err := b.ensureReady(ctx); err != nil {
		return nil, err
	}
	if len(filter.Kinds) == 0 || filter.LibraryRestricted && len(filter.VisibleLibraryIDs) == 0 {
		return []string{}, nil
	}
	filters := []any{map[string]any{"terms": map[string]any{"kind": filter.Kinds}}}
	if len(personIDs) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"person_ids": personIDs}})
	}
	if name = strings.TrimSpace(name); name != "" {
		// keyword 存储小写姓名；通配符只用于子串边界，输入中的符号按字面匹配。
		pattern := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?").Replace(strings.ToLower(name))
		filters = append(filters, map[string]any{"wildcard": map[string]any{"person_names": "*" + pattern + "*"}})
	}
	if name == "" && len(personIDs) == 0 {
		return []string{}, nil
	}
	if filter.LibraryRestricted {
		filters = append(filters, map[string]any{"terms": map[string]any{"library_ids": filter.VisibleLibraryIDs}})
	}
	const batchSize = 500
	ids := []string{}
	lastID := ""
	for {
		pageFilters := append([]any{}, filters...)
		if lastID != "" {
			pageFilters = append(pageFilters, map[string]any{"range": map[string]any{"id": map[string]any{"gt": lastID}}})
		}
		body := map[string]any{
			"size": batchSize, "_source": []string{"id"}, "track_total_hits": false,
			"query": map[string]any{"bool": map[string]any{"filter": pageFilters}},
			"sort":  []any{map[string]any{"id": "asc"}},
		}
		var resp struct {
			TimedOut bool `json:"timed_out"`
			Shards   struct {
				Failed int `json:"failed"`
			} `json:"_shards"`
			Hits struct {
				Hits []struct {
					ID     string `json:"_id"`
					Source struct {
						ID string `json:"id"`
					} `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		if err := b.doJSON(ctx, http.MethodPost, "/"+url.PathEscape(b.alias)+"/_search", body, &resp); err != nil {
			return nil, err
		}
		if resp.TimedOut || resp.Shards.Failed > 0 {
			return nil, errors.New("opensearch person search returned incomplete results")
		}
		for _, hit := range resp.Hits.Hits {
			id := hit.Source.ID
			if id == "" {
				id = hit.ID
			}
			if id <= lastID {
				return nil, errors.New("opensearch person search cursor did not advance")
			}
			ids = append(ids, id)
			lastID = id
		}
		if len(resp.Hits.Hits) < batchSize {
			return ids, nil
		}
	}
}
