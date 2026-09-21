package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
)

type hongGuoSearchTestBackend struct {
	recordingMetadataSearchBackend
	ids      []string
	err      error
	writeErr error
	filters  []MetadataSearchFilter
}

func (b *hongGuoSearchTestBackend) SearchMetadataIDs(_ context.Context, _ string, _, _ int, filter MetadataSearchFilter) ([]string, int64, error) {
	b.filters = append(b.filters, filter)
	return b.ids, int64(len(b.ids)), b.err
}

func (b *hongGuoSearchTestBackend) UpsertMetadata(ctx context.Context, row MetadataSearchDocument) error {
	if b.writeErr != nil {
		return b.writeErr
	}
	return b.recordingMetadataSearchBackend.UpsertMetadata(ctx, row)
}

func TestHongGuoSearchIndexLifecycleAndVisibility(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	backend := &hongGuoSearchTestBackend{}
	r := repos.HongGuo
	r.SetSearchBackend(backend)
	input := hongguo.Work{SourceID: "12345678901", Title: "航海王", EpisodeCount: 2, Snapshot: json.RawMessage(`{}`)}
	work, err := r.SaveDetail(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.upserts) != 1 || backend.upserts[0].ID != "hg-work-"+work.ID {
		t.Fatalf("missing committed detail update: %+v", backend.upserts)
	}
	if err := r.SaveAlbum(ctx, input.SourceID, hongguo.Album{ID: "99999999999", Season: 1}); err != nil {
		t.Fatal(err)
	}
	groupID := "hg-group-99999999999"
	if !containsStringValue(backend.deletes, "hg-work-"+work.ID) || backend.upserts[len(backend.upserts)-1].ID != groupID {
		t.Fatalf("album transition did not replace standalone identity")
	}
	input.SourceID, input.Title = "12345678902", "航海王续篇"
	second, err := r.SaveDetail(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SaveAlbum(ctx, input.SourceID, hongguo.Album{ID: "99999999999", Season: 2}); err != nil {
		t.Fatal(err)
	}
	// 重建过程中发生标题更新，最终临时索引必须收到更新后的合集文档。
	backend.onIndex = func() {
		backend.onIndex = nil
		input.SourceID, input.Title = "12345678901", "航海王 新版"
		if _, err := r.SaveDetail(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if total, err := r.BackfillSearchIndex(ctx, 1, 0); err != nil || total != 1 || !backend.activated {
		t.Fatalf("rebuild total=%d err=%v", total, err)
	}
	if got := backend.indexed[len(backend.indexed)-1]; got.ID != groupID || got.Title != "航海王 新版" {
		t.Fatalf("dirty update not replayed: %+v", got)
	}
	lib := model.Library{Name: "source", Type: model.LibraryTypeHongGuo, Path: "/test/source"}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	file := model.Media{LibraryID: lib.ID, CatalogSource: "hongguo", Path: "/test/source/episode.mkv"}
	if err := repos.DB.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.HongGuoMediaBinding{MediaID: file.ID, WorkID: second.ID}).Error; err != nil {
		t.Fatal(err)
	}
	backend.ids = []string{groupID, "hg-work-hidden", "hg-work-deleted"}
	filter := MetadataSearchFilter{Kinds: []string{"series"}, MediaQueryFilter: MediaQueryFilter{IncludeNSFW: true}}
	assertResult := func(want int) {
		t.Helper()
		rows, err := r.SearchCandidates(ctx, "航海王", filter)
		if err != nil || len(rows) != want {
			t.Fatalf("rows=%v err=%v want=%d", rows, err, want)
		}
		if want == 1 && rows[0].ID != groupID {
			t.Fatalf("rows=%v", rows)
		}
	}
	assertResult(1)
	if !reflect.DeepEqual(backend.filters[0].CandidateIDs, []string{groupID}) {
		t.Fatalf("visibility not applied before recall: %+v", backend.filters[0])
	}
	filter.HiddenLibraryIDs = []string{lib.ID}
	assertResult(0)
	filter.HiddenLibraryIDs = nil
	filter.LibraryRestricted = true
	assertResult(0)
	filter.LibraryRestricted = false
	filter.FavoriteUserID = "viewer"
	assertResult(0)
	if err := repos.DB.Create(&model.HongGuoUserState{UserID: "viewer", SourceID: second.SourceID, Favorite: true}).Error; err != nil {
		t.Fatal(err)
	}
	assertResult(1)
	filter.FavoriteUserID = ""
	backend.err = errors.New("unavailable")
	assertResult(1)
	backend.err, backend.writeErr = nil, errors.New("write unavailable")
	input.Title = "航海王 更新失败"
	if _, err := r.SaveDetail(ctx, input); err != nil {
		t.Fatal(err)
	}
	requests := len(backend.filters)
	assertResult(1)
	if len(backend.filters) != requests {
		t.Fatal("known stale index used")
	}
	backend.writeErr = nil
	if _, err := r.BackfillSearchIndex(ctx, 10, 0); err != nil {
		t.Fatal(err)
	}
	assertResult(1)
	if len(backend.filters) != requests+1 {
		t.Fatal("rebuilt index not restored")
	}
	if err := r.SaveAlbum(ctx, work.SourceID, hongguo.Album{}); err != nil {
		t.Fatal(err)
	}
	if got := backend.upserts[len(backend.upserts)-1]; got.ID != groupID || got.Title != second.Title {
		t.Fatalf("old album title not refreshed on member departure: %+v", got)
	}
	if err := r.SaveAlbum(ctx, second.SourceID, hongguo.Album{}); err != nil {
		t.Fatal(err)
	}
	if !containsStringValue(backend.deletes, groupID) {
		t.Fatal("empty album remains indexed")
	}
	if err := repos.DB.Delete(&file).Error; err != nil {
		t.Fatal(err)
	}
	assertResult(0)
}

func TestOpenSearchHongGuoLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_OPENSEARCH_LIVE") != "1" {
		t.Skip("requires explicit live search test")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	cfg.Search.Index = "mediastation_search_test_" + uuid.NewString()
	backend := NewOpenSearchHongGuoBackend(cfg.Search)
	if backend == nil {
		t.Fatal("OpenSearch not configured")
	}
	index, err := backend.PrepareMetadataIndex(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.DiscardMetadataIndex(context.Background(), index); err != nil {
			t.Error(err)
		}
	})
	if err := backend.IndexMetadata(t.Context(), index, []MetadataSearchDocument{
		{ID: "hg-work-visible", Kind: "series", Title: "航海王"},
		{ID: "hg-work-hidden", Kind: "series", Title: "航海王"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ActivateMetadataIndex(t.Context(), index); err != nil {
		t.Fatal(err)
	}
	ids, total, err := backend.SearchMetadataIDs(t.Context(), "航海王", 0, 20, MetadataSearchFilter{Kinds: []string{"series"}, CandidateIDs: []string{"hg-work-visible"}})
	if err != nil || total != 1 || !reflect.DeepEqual(ids, []string{"hg-work-visible"}) {
		t.Fatalf("ids=%v total=%d err=%v", ids, total, err)
	}
}

func TestOpenSearchHongGuoAliasAndCandidateScope(t *testing.T) {
	var mapping, search map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/_alias"):
			http.Error(w, "not found", 404)
		case r.Method == http.MethodPut:
			if !strings.HasPrefix(r.URL.Path, "/example_hongguo_v1_") {
				t.Error("wrong target index", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&mapping)
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/example_hongguo/_mapping":
			_, _ = w.Write([]byte(`{"index":{"mappings":{"_meta":{"schema_version":1,"document_type":"hongguo"}}}}`))
		case r.URL.Path == "/example_hongguo/_search":
			_ = json.NewDecoder(r.Body).Decode(&search)
			_, _ = w.Write([]byte(`{"hits":{"hits":[{"_id":"hg-work-example"}],"total":1}}`))
		default:
			t.Error("unexpected request", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer upstream.Close()
	backend := NewOpenSearchHongGuoBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL, Index: "example"})
	if _, err := backend.PrepareMetadataIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustJSON(t, mapping), `"document_type":"hongguo"`) {
		t.Fatal(mapping)
	}
	ids, _, err := backend.SearchMetadataIDs(t.Context(), "航海王", 0, 10, MetadataSearchFilter{Kinds: []string{"series"}, CandidateIDs: []string{"hg-work-example"}})
	if err != nil || !reflect.DeepEqual(ids, []string{"hg-work-example"}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if !strings.Contains(mustJSON(t, search), `"terms":{"id":["hg-work-example"]}`) {
		t.Fatal(search)
	}
}
