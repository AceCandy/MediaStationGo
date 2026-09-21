package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

const (
	defaultMetadataSearchAlias = "mediastation_metadata"
	metadataSearchSchema       = 1
)

type OpenSearchMediaBackend struct {
	baseURL      string
	alias        string
	username     string
	password     string
	client       *http.Client
	documentType string

	readyMu sync.RWMutex
	ready   bool
}

func NewOpenSearchMediaBackend(cfg config.SearchConfig) *OpenSearchMediaBackend {
	if strings.TrimSpace(cfg.Backend) != "opensearch" || strings.TrimSpace(cfg.OpenSearchURL) == "" {
		return nil
	}
	alias := strings.TrimSpace(cfg.Index)
	if alias == "" || alias == "mediastation_media" {
		alias = defaultMetadataSearchAlias
	}
	return &OpenSearchMediaBackend{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.OpenSearchURL), "/"),
		alias:        alias,
		username:     strings.TrimSpace(cfg.Username),
		password:     cfg.Password,
		client:       &http.Client{Timeout: 4 * time.Second},
		documentType: "metadata",
	}
}

// NewOpenSearchHongGuoBackend 共用连接配置，红果文档与普通资料使用独立 alias。
func NewOpenSearchHongGuoBackend(cfg config.SearchConfig) *OpenSearchMediaBackend {
	b := NewOpenSearchMediaBackend(cfg)
	if b != nil {
		b.alias += "_hongguo"
		b.documentType = "hongguo"
	}
	return b
}

func (b *OpenSearchMediaBackend) SearchMetadataIDs(ctx context.Context, query string, _, _ int, filter MetadataSearchFilter) ([]string, int64, error) {
	if err := b.ensureReady(ctx); err != nil {
		return nil, 0, err
	}
	groups := buildMetadataSearchTermGroups(MediaSearchTerms(query))
	if len(groups) == 0 || len(filter.Kinds) == 0 || (filter.LibraryRestricted && len(filter.VisibleLibraryIDs) == 0) {
		return []string{}, 0, nil
	}
	fields := []string{"title^4", "original_name^3"}
	if filter.Fields != MetadataSearchFieldsTitle {
		fields = append(fields, "overview^2", "genres^2")
	}
	must := make([]any, 0, len(groups))
	for _, group := range groups {
		should := make([]any, 0, len(group.variants))
		for _, variant := range group.variants {
			multiMatch := map[string]any{"query": variant.value, "fields": fields, "type": "best_fields", "operator": "and"}
			if group.numeric {
				multiMatch = map[string]any{"query": variant.value, "fields": fields, "type": "phrase"}
			}
			should = append(should, map[string]any{
				"multi_match": multiMatch,
			})
		}
		must = append(must, map[string]any{
			"bool": map[string]any{
				"should": should, "minimum_should_match": 1,
			},
		})
	}
	filters := []any{map[string]any{"terms": map[string]any{"kind": filter.Kinds}}}
	if filter.CandidateIDs != nil {
		if len(filter.CandidateIDs) == 0 {
			return []string{}, 0, nil
		}
		filters = append(filters, map[string]any{"terms": map[string]any{"id": filter.CandidateIDs}})
	}
	if !filter.IncludeNSFW {
		filters = append(filters, map[string]any{"term": map[string]any{"nsfw": false}})
	}
	if filter.LibraryRestricted {
		filters = append(filters, map[string]any{"terms": map[string]any{"library_ids": filter.VisibleLibraryIDs}})
	}
	body := map[string]any{
		"from":    0,
		"size":    maxMetadataSearchCandidates,
		"_source": []string{"id"},
		"query":   map[string]any{"bool": map[string]any{"must": must, "filter": filters}},
		"sort":    []any{map[string]any{"_score": "desc"}, map[string]any{"id": "asc"}},
	}
	var resp struct {
		Hits struct {
			Total any `json:"total"`
			Hits  []struct {
				ID     string `json:"_id"`
				Source struct {
					ID string `json:"id"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := b.doJSON(ctx, http.MethodPost, "/"+url.PathEscape(b.alias)+"/_search", body, &resp); err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		id := strings.TrimSpace(hit.Source.ID)
		if id == "" {
			id = strings.TrimSpace(hit.ID)
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, openSearchTotal(resp.Hits.Total), nil
}

func (b *OpenSearchMediaBackend) PrepareMetadataIndex(ctx context.Context) (string, error) {
	if b == nil || b.client == nil || b.baseURL == "" || b.alias == "" {
		return "", errors.New("opensearch backend not configured")
	}
	b.discardOrphanedMetadataIndices(ctx)
	index := fmt.Sprintf("%s_v%d_%d", b.alias, metadataSearchSchema, time.Now().UTC().UnixNano())
	mapping := map[string]any{
		"mappings": map[string]any{
			"_meta": map[string]any{
				"schema_version": metadataSearchSchema,
				"document_type":  b.documentType,
			},
			"properties": map[string]any{
				"id":            map[string]any{"type": "keyword"},
				"kind":          map[string]any{"type": "keyword"},
				"title":         map[string]any{"type": "text"},
				"original_name": map[string]any{"type": "text"},
				"overview":      map[string]any{"type": "text"},
				"genres":        map[string]any{"type": "text"},
				"nsfw":          map[string]any{"type": "boolean"},
				"library_ids":   map[string]any{"type": "keyword"},
			},
		},
	}
	if err := b.doJSON(ctx, http.MethodPut, "/"+url.PathEscape(index), mapping, nil); err != nil {
		return "", err
	}
	return index, nil
}

func (b *OpenSearchMediaBackend) IndexMetadata(ctx context.Context, index string, rows []MetadataSearchDocument) error {
	if len(rows) == 0 {
		return nil
	}
	index = strings.TrimSpace(index)
	if index == "" {
		return errors.New("opensearch target index required")
	}
	var bulk bytes.Buffer
	enc := json.NewEncoder(&bulk)
	for _, row := range rows {
		if err := enc.Encode(map[string]any{"index": map[string]any{"_index": index, "_id": row.ID}}); err != nil {
			return err
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	var response struct {
		Errors bool `json:"errors"`
	}
	if err := b.do(ctx, http.MethodPost, "/_bulk", &bulk, "application/x-ndjson", &response); err != nil {
		return err
	}
	if response.Errors {
		return errors.New("opensearch bulk metadata indexing failed")
	}
	return nil
}

func (b *OpenSearchMediaBackend) ActivateMetadataIndex(ctx context.Context, index string) error {
	if err := b.do(ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_refresh", nil, "", nil); err != nil {
		return err
	}
	indices, err := b.aliasIndices(ctx)
	if err != nil {
		return err
	}
	actions := make([]any, 0, len(indices)+1)
	for _, old := range indices {
		if old != index {
			actions = append(actions, map[string]any{"remove": map[string]any{"index": old, "alias": b.alias}})
		}
	}
	actions = append(actions, map[string]any{"add": map[string]any{"index": index, "alias": b.alias, "is_write_index": true}})
	if err := b.doJSON(ctx, http.MethodPost, "/_aliases", map[string]any{"actions": actions}, nil); err != nil {
		return err
	}
	b.readyMu.Lock()
	b.ready = true
	b.readyMu.Unlock()
	return nil
}

func (b *OpenSearchMediaBackend) DiscardMetadataIndex(ctx context.Context, index string) error {
	if strings.TrimSpace(index) == "" {
		return nil
	}
	err := b.do(ctx, http.MethodDelete, "/"+url.PathEscape(index), nil, "", nil)
	if isOpenSearchStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (b *OpenSearchMediaBackend) UpsertMetadata(ctx context.Context, row MetadataSearchDocument) error {
	if err := b.ensureReady(ctx); err != nil {
		return err
	}
	return b.doJSON(ctx, http.MethodPut, "/"+url.PathEscape(b.alias)+"/_doc/"+url.PathEscape(row.ID), row, nil)
}

func (b *OpenSearchMediaBackend) DeleteMetadata(ctx context.Context, id string) error {
	if err := b.ensureReady(ctx); err != nil {
		return err
	}
	return b.DeleteMetadataFromIndex(ctx, b.alias, id)
}

func (b *OpenSearchMediaBackend) DeleteMetadataFromIndex(ctx context.Context, index, id string) error {
	if strings.TrimSpace(index) == "" || strings.TrimSpace(id) == "" {
		return nil
	}
	err := b.do(ctx, http.MethodDelete, "/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id), nil, "", nil)
	if isOpenSearchStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (b *OpenSearchMediaBackend) ensureReady(ctx context.Context) error {
	if b == nil || b.client == nil || b.baseURL == "" || b.alias == "" {
		return errors.New("opensearch backend not configured")
	}
	b.readyMu.RLock()
	ready := b.ready
	b.readyMu.RUnlock()
	if ready {
		return nil
	}
	var mappings map[string]struct {
		Mappings struct {
			Meta map[string]any `json:"_meta"`
		} `json:"mappings"`
	}
	if err := b.doJSON(ctx, http.MethodGet, "/"+url.PathEscape(b.alias)+"/_mapping", nil, &mappings); err != nil {
		return err
	}
	for _, mapping := range mappings {
		if schemaVersion(mapping.Mappings.Meta["schema_version"]) == metadataSearchSchema && mapping.Mappings.Meta["document_type"] == b.documentType {
			b.readyMu.Lock()
			b.ready = true
			b.readyMu.Unlock()
			return nil
		}
	}
	return errors.New("opensearch metadata alias is not ready")
}

func schemaVersion(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func (b *OpenSearchMediaBackend) aliasIndices(ctx context.Context) ([]string, error) {
	var aliases map[string]json.RawMessage
	err := b.doJSON(ctx, http.MethodGet, "/_alias/"+url.PathEscape(b.alias), nil, &aliases)
	if isOpenSearchStatus(err, http.StatusNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	indices := make([]string, 0, len(aliases))
	for index := range aliases {
		indices = append(indices, index)
	}
	return indices, nil
}

func (b *OpenSearchMediaBackend) discardOrphanedMetadataIndices(ctx context.Context) {
	prefix := fmt.Sprintf("%s_v%d_", b.alias, metadataSearchSchema)
	var indices map[string]struct {
		Aliases map[string]any `json:"aliases"`
	}
	err := b.doJSON(ctx, http.MethodGet, "/"+url.PathEscape(prefix)+"*/_alias", nil, &indices)
	if err != nil {
		return
	}
	for index, state := range indices {
		if _, active := state.Aliases[b.alias]; active {
			continue
		}
		_ = b.DiscardMetadataIndex(ctx, index)
	}
}

func (b *OpenSearchMediaBackend) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	return b.do(ctx, method, path, reader, "application/json", out)
}

type openSearchHTTPError struct {
	method string
	path   string
	status int
}

func (e *openSearchHTTPError) Error() string {
	return fmt.Sprintf("opensearch %s %s returned %d", e.method, e.path, e.status)
}

func isOpenSearchStatus(err error, status int) bool {
	var httpErr *openSearchHTTPError
	return errors.As(err, &httpErr) && httpErr.status == status
}

func (b *OpenSearchMediaBackend) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, b.baseURL+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if b.username != "" {
		req.SetBasicAuth(b.username, b.password)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return &openSearchHTTPError{method: method, path: path, status: resp.StatusCode}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func openSearchTotal(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case map[string]any:
		if n, ok := v["value"].(float64); ok {
			return int64(n)
		}
	}
	return 0
}
