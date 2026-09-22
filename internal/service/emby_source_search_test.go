package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type sourceSearchBackend struct {
	ids     []string
	filters []repository.MetadataSearchFilter
	err     error
}

func (b *sourceSearchBackend) SearchMetadataIDs(_ context.Context, _ string, _, _ int, filter repository.MetadataSearchFilter) ([]string, int64, error) {
	b.filters = append(b.filters, filter)
	return b.ids, int64(len(b.ids)), b.err
}

func TestEmbySourceSearchUsesSeparateBackendsWithNFO(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repo)
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	lib := model.Library{Base: model.Base{ID: "search-library"}, Name: "Search", Type: "mixed", Path: "/test/search"}
	create(&lib)
	create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: "ordinary"}, Kind: "movie", Title: "航海王", Source: "test"})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "ordinary-file"}, LibraryID: lib.ID, MetadataID: "ordinary", Path: "/test/search/movie.mkv"})
	create(&model.MediaProbeMetadata{MediaID: "ordinary-file", DurationMS: 90000, ProbeJSON: "{}"})
	create(&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "source"}, SourceID: "12345678901", Kind: "series", Title: "航海王续篇"})
	create(&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "source-episode"}, WorkID: "source", Number: 1})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "source-file"}, LibraryID: lib.ID, CatalogSource: "hongguo", Path: "/test/search/source.mkv"})
	episodeID := "source-episode"
	create(&model.HongGuoMediaBinding{MediaID: "source-file", WorkID: "source", EpisodeID: &episodeID})
	create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: "local"}, LibraryID: lib.ID, LocalKey: "local", Kind: "movie", NFOFields: model.NFOFields{Title: "寻找航海王"}})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "local-file"}, LibraryID: lib.ID, CatalogSource: "nfo", Path: "/test/search/local.mkv"})
	create(&model.NFOMediaBinding{MediaID: "local-file", ItemID: "local", NFOFields: model.NFOFields{Title: "寻找航海王"}})
	ordinary := &sourceSearchBackend{ids: []string{"ordinary"}}
	source := &sourceSearchBackend{ids: []string{"hg-work-source"}}
	repo.MediaView.SetSearchBackend(ordinary)
	repo.HongGuo.SetSearchBackend(source)
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	p := ItemsParams{UserID: "viewer", SearchTerm: "航海王", Limit: 1, IncludeItemTypes: []string{"Movie", "Series"}, Fields: []string{"BasicSyncInfo"}}
	for offset, want := range []string{"ordinary", "hg-work-source", "nfo-local"} {
		p.StartIndex = offset
		page, err := e.Items(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		items := page["Items"].([]map[string]any)
		if page["TotalRecordCount"] != int64(3) || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("page %d: %+v", offset, page)
		}
	}
	if len(ordinary.filters) != 3 || len(source.filters) != 3 {
		t.Fatal("NFO bypassed source search backends")
	}
	if !reflect.DeepEqual(source.filters[0].CandidateIDs, []string{"hg-work-source"}) {
		t.Fatal("source visibility missing")
	}
	p.StartIndex, p.Limit = 0, 20
	source.err, ordinary.err = errors.New("unavailable"), errors.New("unavailable")
	hints, err := e.SearchHints(t.Context(), p)
	if err != nil || hints["TotalRecordCount"] != int64(3) {
		t.Fatalf("fallback hints=%v err=%v", hints, err)
	}
	assertSearchHintsProjection(t, e, p)
	e.visibilityCache["viewer"] = embyVisibilityCacheEntry{visibility: MediaVisibility{IncludeNSFW: true, HiddenLibraryIDs: []string{lib.ID}}, expiresAt: time.Now().Add(time.Hour)}
	page, err := e.Items(t.Context(), p)
	if err != nil || page["TotalRecordCount"] != int64(0) {
		t.Fatalf("hidden page=%v err=%v", page, err)
	}
	e.visibilityCache["viewer"] = embyVisibilityCacheEntry{visibility: MediaVisibility{LibraryRestricted: true}, expiresAt: time.Now().Add(time.Hour)}
	page, err = e.Items(t.Context(), p)
	if err != nil || page["TotalRecordCount"] != int64(0) {
		t.Fatalf("locked page=%v err=%v", page, err)
	}
}

func assertSearchHintsProjection(t *testing.T, e *EmbyService, p ItemsParams) {
	t.Helper()
	queries := 0
	count := func(db *gorm.DB) {
		if !db.DryRun {
			queries++
		}
	}
	if err := e.repo.DB.Callback().Query().After("gorm:query").Register("test:hints-query", count); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DB.Callback().Row().After("gorm:row").Register("test:hints-row", count); err != nil {
		t.Fatal(err)
	}
	p.Fields = nil
	full, err := e.Items(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	fullCount := queries
	queries = 0
	// 即使客户端要求完整关系，提示仍只加载自己的字段。
	p.Fields = []string{"People", "ProviderIds", "MediaSources", "MediaStreams"}
	hints, err := e.SearchHints(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if queries >= fullCount {
		t.Fatalf("hint queries=%d, full=%d", queries, fullCount)
	}
	if hints["TotalRecordCount"] != full["TotalRecordCount"] {
		t.Fatal("hint total changed")
	}
	items := full["Items"].([]map[string]any)
	rows := hints["SearchHints"].([]map[string]any)
	if len(rows) != len(items) {
		t.Fatal("hint count changed")
	}
	for i, item := range items {
		for _, field := range []string{"Id", "Name", "Type", "MediaType", "ProductionYear", "IndexNumber", "ParentIndexNumber", "RunTimeTicks"} {
			if !reflect.DeepEqual(rows[i][field], item[field]) {
				t.Errorf("hint %d field %s changed", i, field)
			}
		}
		if tags, ok := item["ImageTags"].(map[string]string); ok && rows[i]["PrimaryImageTag"] != tags["Primary"] {
			t.Error("hint image changed")
		}
	}
	t.Logf("search hints queries=%d, full items=%d", queries, fullCount)
}

func TestEmbyHongGuoSearchKeepsPlayedFilterWithoutNFO(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repo)
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	lib := model.Library{Name: "Source", Type: model.LibraryTypeHongGuo, Path: "/test/source"}
	create(&lib)
	for _, id := range []string{"played", "unplayed"} {
		create(&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: id}, SourceID: id, Kind: "movie", Title: "航海王"})
		create(&model.Media{PermanentBase: model.PermanentBase{ID: id}, LibraryID: lib.ID, CatalogSource: "hongguo", Path: "/test/source/" + id + ".mkv"})
		create(&model.HongGuoMediaBinding{MediaID: id, WorkID: id})
	}
	create(&model.HongGuoUserState{UserID: "viewer", SourceID: "played", EpisodeNumber: 1, Completed: true})
	e.visibilityCache = map[string]embyVisibilityCacheEntry{"viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
	for filter, want := range map[string]string{"IsPlayed": "hg-work-played", "IsUnplayed": "hg-work-unplayed"} {
		page, err := e.Items(t.Context(), ItemsParams{UserID: "viewer", SearchTerm: "航海王", Filters: []string{filter}, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		items := page["Items"].([]map[string]any)
		if page["TotalRecordCount"] != int64(1) || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("filter=%s page=%v", filter, page)
		}
	}
}
