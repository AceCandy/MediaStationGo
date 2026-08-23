package repository

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestOpenSearchMetadataSearchQuery(t *testing.T) {
	var searchBodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/metadata-test/_mapping":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata-test-v1": map[string]any{"mappings": map[string]any{"_meta": map[string]any{
					"schema_version": metadataSearchSchema,
					"document_type":  "metadata",
				}}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/metadata-test/_search":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			searchBodies = append(searchBodies, body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": map[string]any{
					"total": map[string]any{"value": 2},
					"hits": []any{
						map[string]any{"_id": "metadata-1", "_source": map[string]any{"id": "metadata-1"}},
						map[string]any{"_id": "metadata-2", "_source": map[string]any{}},
					},
				},
			})
		default:
			t.Fatalf("unexpected OpenSearch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()

	backend := NewOpenSearchMediaBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL, Index: "metadata-test"})
	ids, total, err := backend.SearchMetadataIDs(t.Context(), "流浪 地球", 5, 10, MetadataSearchFilter{
		MediaQueryFilter:  MediaQueryFilter{IncludeNSFW: false},
		Fields:            MetadataSearchFieldsTitle,
		Kinds:             []string{"movie", "series"},
		LibraryRestricted: true,
		VisibleLibraryIDs: []string{"library-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(ids) != 2 || ids[0] != "metadata-1" || ids[1] != "metadata-2" {
		t.Fatalf("ids=%#v total=%d", ids, total)
	}
	if len(searchBodies) != 1 || searchBodies[0]["from"] != float64(0) || searchBodies[0]["size"] != float64(maxMetadataSearchCandidates) {
		t.Fatalf("search bodies = %#v", searchBodies)
	}
	titleQuery, _ := json.Marshal(searchBodies[0])
	titleJSON := string(titleQuery)
	for _, want := range []string{"title^4", "original_name^3", "library_ids", "library-1", "movie", "series"} {
		if !strings.Contains(titleJSON, want) {
			t.Fatalf("title search query missing %q: %s", want, titleJSON)
		}
	}
	if strings.Contains(titleJSON, "overview") || strings.Contains(titleJSON, "genres") || strings.Contains(titleJSON, "path") || strings.Contains(titleJSON, "scan_title") {
		t.Fatalf("Emby title search contains unsupported fields: %s", titleJSON)
	}
	if strings.Count(titleJSON, `"multi_match"`) != 2 {
		t.Fatalf("multi-word search must require every term: %s", titleJSON)
	}
	if !strings.Contains(titleJSON, `"operator":"and"`) || strings.Contains(titleJSON, "fuzziness") {
		t.Fatalf("search must require all analyzed tokens without fuzzy matching: %s", titleJSON)
	}

	_, _, err = backend.SearchMetadataIDs(t.Context(), "44", 8, 1, MetadataSearchFilter{
		Fields: MetadataSearchFieldsTitle,
		Kinds:  []string{"movie", "series"},
	})
	if err != nil {
		t.Fatal(err)
	}
	numericJSON := mustJSON(t, searchBodies[1])
	for _, want := range []string{`"query":"44"`, `"query":"四十四"`, `"minimum_should_match":1`, `"type":"phrase"`} {
		if !strings.Contains(numericJSON, want) {
			t.Fatalf("numeric-equivalent search missing %q: %s", want, numericJSON)
		}
	}
	if strings.Count(numericJSON, `"multi_match"`) != 2 {
		t.Fatalf("numeric equivalents must be alternatives in one term group: %s", numericJSON)
	}
	if !strings.Contains(numericJSON, `"_score":"desc"`) || !strings.Contains(numericJSON, `"id":"asc"`) {
		t.Fatalf("candidate selection must be stable: %s", numericJSON)
	}

	_, _, err = backend.SearchMetadataIDs(t.Context(), "宇宙", 0, 20, MetadataSearchFilter{
		Fields: MetadataSearchFieldsWeb,
		Kinds:  []string{"movie", "series"},
	})
	if err != nil {
		t.Fatal(err)
	}
	webQuery, _ := json.Marshal(searchBodies[2])
	for _, want := range []string{"title^4", "original_name^3", "overview^2", "genres^2"} {
		if !strings.Contains(string(webQuery), want) {
			t.Fatalf("Web search query missing %q: %s", want, webQuery)
		}
	}

	requests := len(searchBodies)
	ids, total, err = backend.SearchMetadataIDs(t.Context(), "宇宙", 0, 20, MetadataSearchFilter{
		Kinds:             []string{"movie"},
		LibraryRestricted: true,
	})
	if err != nil || len(ids) != 0 || total != 0 || len(searchBodies) != requests {
		t.Fatalf("restricted-empty search ids=%#v total=%d err=%v requests=%d", ids, total, err, len(searchBodies))
	}
}

func TestOpenSearchMetadataIndexLifecycle(t *testing.T) {
	var (
		indexName string
		mapping   map[string]any
		aliasBody map[string]any
		upsert    map[string]any
		bulk      string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "*/_alias"):
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/metadata-test_v1_"):
			indexName = strings.TrimPrefix(r.URL.Path, "/")
			if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
				t.Fatal(err)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/_bulk":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			bulk = string(raw)
			_, _ = io.WriteString(w, `{"errors":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/"+indexName+"/_refresh":
		case r.Method == http.MethodGet && r.URL.Path == "/_alias/metadata-test":
			_, _ = io.WriteString(w, `{"metadata-test_v1_old":{}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/_aliases":
			if err := json.NewDecoder(r.Body).Decode(&aliasBody); err != nil {
				t.Fatal(err)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/metadata-test/_doc/metadata-movie":
			if err := json.NewDecoder(r.Body).Decode(&upsert); err != nil {
				t.Fatal(err)
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/metadata-test/_doc/metadata-movie":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/"+indexName:
		default:
			t.Fatalf("unexpected OpenSearch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()

	backend := NewOpenSearchMediaBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL, Index: "metadata-test"})
	index, err := backend.PrepareMetadataIndex(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	row := MetadataSearchDocument{
		ID: "metadata-movie", Kind: "movie", Title: "Movie", OriginalName: "Original",
		Overview: "Overview", Genres: "Drama", LibraryIDs: []string{"library-1"},
	}
	if err := backend.IndexMetadata(t.Context(), index, []MetadataSearchDocument{row}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ActivateMetadataIndex(t.Context(), index); err != nil {
		t.Fatal(err)
	}
	if err := backend.UpsertMetadata(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if err := backend.DeleteMetadata(t.Context(), row.ID); err != nil {
		t.Fatal(err)
	}
	if err := backend.DiscardMetadataIndex(t.Context(), index); err != nil {
		t.Fatal(err)
	}

	mappings := mapping["mappings"].(map[string]any)
	properties := mappings["properties"].(map[string]any)
	if len(properties) != 8 {
		t.Fatalf("metadata mapping properties = %#v", properties)
	}
	for _, key := range []string{"id", "kind", "title", "original_name", "overview", "genres", "nsfw", "library_ids"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("metadata mapping missing %q: %#v", key, properties)
		}
	}
	for _, raw := range []string{bulk, mustJSON(t, upsert), mustJSON(t, aliasBody)} {
		if strings.Contains(raw, "media_id") || strings.Contains(raw, "path") || strings.Contains(raw, "scan_title") {
			t.Fatalf("OpenSearch payload exposes Media fields: %s", raw)
		}
	}
	if !strings.Contains(bulk, `"_id":"metadata-movie"`) || !strings.Contains(bulk, `"library_ids":["library-1"]`) {
		t.Fatalf("bulk metadata document = %s", bulk)
	}
	aliasJSON := mustJSON(t, aliasBody)
	if !strings.Contains(aliasJSON, "metadata-test_v1_old") || !strings.Contains(aliasJSON, indexName) {
		t.Fatalf("alias switch = %s", aliasJSON)
	}
}

func TestOpenSearchMetadataAliasMustBeReady(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"legacy":{"mappings":{"_meta":{"schema_version":0,"document_type":"media"}}}}`)
	}))
	defer upstream.Close()

	backend := NewOpenSearchMediaBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL, Index: "metadata-test"})
	if _, _, err := backend.SearchMetadataIDs(t.Context(), "movie", 0, 10, MetadataSearchFilter{Kinds: []string{"movie"}}); err == nil {
		t.Fatal("incompatible alias must not serve metadata search")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
