package repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestOpenSearchPersonWorkCursorAndLiteralName(t *testing.T) {
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_mapping") {
			fmt.Fprintf(w, `{"index":{"mappings":{"_meta":{"schema_version":%d,"document_type":"metadata"}}}}`, metadataSearchSchema)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		bodies = append(bodies, body)
		start, end := 0, 500
		if len(bodies) == 2 {
			start, end = 500, 507
		}
		hits := []any{}
		for i := start; i < end; i++ {
			hits = append(hits, map[string]any{"_id": fmt.Sprintf("work-%04d", i)})
		}
		json.NewEncoder(w).Encode(map[string]any{"hits": map[string]any{"hits": hits}})
	}))
	defer upstream.Close()
	b := NewOpenSearchMediaBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL})
	ids, err := b.SearchPersonWorkIDs(t.Context(), `A*B?C\%_`, []string{"person-id"}, MetadataSearchFilter{Kinds: []string{"movie", "series"}, LibraryRestricted: true, VisibleLibraryIDs: []string{"visible-library"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 507 || ids[0] != "work-0000" || ids[506] != "work-0506" || len(bodies) != 2 {
		t.Fatalf("ids=%d bodies=%d", len(ids), len(bodies))
	}
	first := mustJSON(t, bodies[0])
	for _, want := range []string{`"person_ids":["person-id"]`, `"library_ids":["visible-library"]`, `"person_names":"*a\\*b\\?c\\\\%_*"`, `"id":"asc"`} {
		if !strings.Contains(first, want) {
			t.Fatalf("missing %s in %s", want, first)
		}
	}
	if strings.Contains(first, `"from"`) || strings.Contains(first, `"range"`) {
		t.Fatalf("first page=%s", first)
	}
	if next := mustJSON(t, bodies[1]); !strings.Contains(next, `"gt":"work-0499"`) {
		t.Fatalf("next page=%s", next)
	}
	ids, err = b.SearchPersonWorkIDs(t.Context(), "name", nil, MetadataSearchFilter{Kinds: []string{"movie"}, LibraryRestricted: true})
	if err != nil || len(ids) != 0 || len(bodies) != 2 {
		t.Fatalf("restricted empty=%v %v", ids, err)
	}
}

func TestOpenSearchPersonWorkRejectsIncompleteResults(t *testing.T) {
	for _, response := range []string{`{"timed_out":true,"hits":{"hits":[]}}`, `{"_shards":{"failed":1},"hits":{"hits":[]}}`} {
		t.Run(response, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
			defer upstream.Close()
			b := NewOpenSearchMediaBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL})
			b.ready = true
			if ids, err := b.SearchPersonWorkIDs(t.Context(), "", []string{"person"}, MetadataSearchFilter{Kinds: []string{"movie"}}); err == nil || ids != nil {
				t.Fatalf("partial result=%v %v", ids, err)
			}
		})
	}
}
