package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type personWorkTestBackend struct {
	ids       []string
	err       error
	calls     int
	personIDs []string
}

func TestOrdinaryPersonWorksLeavesSourcePeopleToExistingRoute(t *testing.T) {
	svc := newTestEmbyService(t)
	_, handled, err := svc.ordinaryPersonWorkItems(t.Context(), ItemsParams{PersonIDs: []string{"hg-person-existing"}, IncludeItemTypes: []string{"Series"}})
	if handled || err != nil {
		t.Fatalf("source person handled=%v err=%v", handled, err)
	}
}

func (b *personWorkTestBackend) SearchMetadataIDs(context.Context, string, int, int, repository.MetadataSearchFilter) ([]string, int64, error) {
	return []string{}, 0, nil
}
func (b *personWorkTestBackend) SearchPersonWorkIDs(_ context.Context, _ string, ids []string, _ repository.MetadataSearchFilter) ([]string, error) {
	b.calls++
	b.personIDs = ids
	return b.ids, b.err
}

func TestEmbyOrdinaryPersonWorksCompletePagesAndRevalidation(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "Movies", Path: "/media/person-search", Type: "mixed", Enabled: true}
	otherLib := model.Library{Name: "Other", Path: "/media/other", Type: "movie", Enabled: true}
	for _, library := range []*model.Library{&lib, &otherLib} {
		if err := svc.repo.Library.Create(t.Context(), library); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"actor-1", "actor-2"} {
		if err := svc.repo.DB.Create(&model.Person{Base: model.Base{ID: id}, Name: "测试演员", OriginalName: "Actor Name", NormalizedName: id, Source: "tmdb"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	b := &personWorkTestBackend{}
	addMovie := func(id, libraryID, personID string, playable bool) {
		t.Helper()
		createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: id, Source: "tmdb"})
		if playable {
			if err := svc.repo.DB.Create(&model.Media{MetadataID: id, LibraryID: libraryID, Path: "/media/person-search/" + id + ".mkv"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if personID != "" {
			if err := svc.repo.DB.Create(&model.MetadataCredit{MetadataID: id, PersonID: personID, Type: model.CreditTypeActor}).Error; err != nil {
				t.Fatal(err)
			}
		}
		b.ids = append(b.ids, id)
	}
	for i := 0; i < 121; i++ {
		addMovie(fmt.Sprintf("work-%03d", i), lib.ID, "actor-1", true)
	}
	addMovie("other-actor-work", lib.ID, "actor-2", true)
	addMovie("other-library-work", otherLib.ID, "actor-1", true)
	addMovie("no-files", lib.ID, "actor-1", false)
	addMovie("stale-relation", lib.ID, "", true)
	if err := svc.repo.DB.Create(&model.Media{MetadataID: "work-000", LibraryID: lib.ID, Path: "/media/person-search/work-000-4k.mkv"}).Error; err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "series-work"}, Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"})
	season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "series-season"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, Title: "Season", SeasonNum: 1, Source: "tmdb"})
	ep := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "series-episode"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, Title: "Episode", EpisodeNum: 1, Source: "tmdb"})
	if err := svc.repo.DB.Create(&model.Media{MetadataID: ep.ID, LibraryID: lib.ID, Path: "/media/person-search/series/S01E01.mkv", SeasonNum: 1, EpisodeNum: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DB.Create(&model.MetadataCredit{MetadataID: season.ID, PersonID: "actor-1", Type: model.CreditTypeActor}).Error; err != nil {
		t.Fatal(err)
	}
	b.ids = append(b.ids, series.ID, "missing-index-id")
	svc.repo.MediaView.SetSearchBackend(b)
	pageIDs := func(p ItemsParams) ([]string, int64) {
		t.Helper()
		out, err := svc.Items(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range out["Items"].([]map[string]any) {
			ids = append(ids, item["Id"].(string))
		}
		return ids, out["TotalRecordCount"].(int64)
	}
	p := ItemsParams{ParentID: lib.ID, PersonIDs: []string{"actor-1"}, IncludeItemTypes: []string{"Movie"}, SortBy: "Name", Limit: 20, StartIndex: 100, Fields: []string{"BasicSyncInfo"}}
	ids, total := pageIDs(p)
	if total != 121 || len(ids) != 20 || ids[0] != "work-100" || ids[19] != "work-119" {
		t.Fatalf("avatar tail=%v total=%d", ids, total)
	}
	if !reflect.DeepEqual(b.personIDs, []string{"actor-1"}) {
		t.Fatalf("backend person filter=%v", b.personIDs)
	}
	for _, sortBy := range []string{"Name", "ProductionYear", "CommunityRating", "DateCreated", "DatePlayed"} {
		t.Run("avatar-sort-"+sortBy, func(t *testing.T) {
			ordered := p
			ordered.StartIndex, ordered.SortBy, ordered.SortOrder = 0, sortBy, "Descending"
			if ids, total := pageIDs(ordered); total != 121 || len(ids) != 20 {
				t.Fatalf("sorted page=%v total=%d", ids, total)
			}
		})
	}
	uncounted := p
	uncounted.StartIndex, uncounted.SkipTotalRecordCount = 0, true
	if ids, total := pageIDs(uncounted); len(ids) != 20 || total < 20 || total > 121 {
		t.Fatalf("optional count page=%v total=%d", ids, total)
	}
	p.StartIndex = 120
	if ids, total := pageIDs(p); total != 121 || !reflect.DeepEqual(ids, []string{"work-120"}) {
		t.Fatalf("last page=%v total=%d", ids, total)
	}
	p.StartIndex = 140
	if ids, total := pageIDs(p); total != 121 || len(ids) != 0 {
		t.Fatalf("empty page=%v total=%d", ids, total)
	}
	p.StartIndex = 0
	p.IncludeItemTypes = []string{"Series"}
	if ids, total := pageIDs(p); total != 1 || !reflect.DeepEqual(ids, []string{series.ID}) {
		t.Fatalf("season actor=%v total=%d", ids, total)
	}
	p.IncludeItemTypes = []string{"Movie", "Series"}
	p.PersonIDs = []string{"actor-2"}
	if ids, total := pageIDs(p); total != 1 || !reflect.DeepEqual(ids, []string{"other-actor-work"}) {
		t.Fatalf("same-name ID=%v total=%d", ids, total)
	}
	p.PersonIDs = nil
	p.SearchTerm = "Actor Name"
	p.StartIndex = 100
	p.Limit = 50
	indexed, indexedTotal := pageIDs(p)
	if indexedTotal != 123 || len(indexed) != 23 {
		t.Fatalf("name tail=%v total=%d", indexed, indexedTotal)
	}
	// 后端出错必须回退全量关联查询，而不是保留不完整索引结果。
	b.err = errors.New("unavailable")
	fallback, fallbackTotal := pageIDs(p)
	if indexedTotal != fallbackTotal || !reflect.DeepEqual(indexed, fallback) {
		t.Fatalf("fallback=%v/%d indexed=%v/%d", fallback, fallbackTotal, indexed, indexedTotal)
	}
	p.SearchTerm = "测试演员"
	p.StartIndex = 0
	if _, total := pageIDs(p); total != 123 {
		t.Fatalf("localized name total=%d", total)
	}
	p.SearchTerm = "%"
	if ids, total := pageIDs(p); total != 0 || len(ids) != 0 {
		t.Fatalf("literal wildcard=%v/%d", ids, total)
	}
	if b.calls == 0 {
		t.Fatal("person search did not use backend")
	}
}

func TestEmbyPersonWorksPreserveDirectSeriesAndSeasonFiles(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "Direct files", Path: "/media/direct", Type: "tv", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	person := model.Person{Base: model.Base{ID: "direct-person"}, Name: "Direct Actor", OriginalName: "Direct Actor", Source: "tmdb"}
	if err := svc.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"direct-series", "direct-season-series"} {
		series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindSeries, Title: id, Source: "tmdb"})
		owner := series
		if id == "direct-season-series" {
			owner = createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, Title: "Season", SeasonNum: 1, Source: "tmdb"})
		}
		if err := svc.repo.DB.Create(&model.MetadataCredit{MetadataID: owner.ID, PersonID: person.ID, Type: model.CreditTypeActor}).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.repo.DB.Create(&model.Media{MetadataID: owner.ID, LibraryID: lib.ID, Path: "/media/direct/" + id + ".mkv"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &personWorkTestBackend{ids: []string{"direct-series", "direct-season-series"}}
	svc.repo.MediaView.SetSearchBackend(backend)
	for _, p := range []ItemsParams{
		{PersonIDs: []string{person.ID}, IncludeItemTypes: []string{"Series"}, SortBy: "Name"},
		{SearchTerm: person.Name, IncludeItemTypes: []string{"Series"}},
	} {
		out, err := svc.Items(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		items := out["Items"].([]map[string]any)
		if len(items) != 2 || out["TotalRecordCount"] != int64(2) {
			t.Fatalf("direct files omitted: %#v", out)
		}
	}
}
