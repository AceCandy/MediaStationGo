package repository

import (
	"reflect"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestPersonSearchDocumentsFollowCreditsAndNames(t *testing.T) {
	repos := searchPlayableWorksFixture(t)
	b := &recordingMetadataSearchBackend{}
	repos.MediaView.SetSearchBackend(b)
	input := CreditInput{Provider: "tmdb", ExternalID: "actor", Name: "Actor Name", Type: model.CreditTypeActor}
	for _, id := range []string{"search-movie", "search-season"} {
		if err := repos.Person.ReplaceCredits(t.Context(), id, []string{model.CreditTypeActor}, []CreditInput{input}); err != nil {
			t.Fatal(err)
		}
	}
	var person model.Person
	if err := repos.DB.Where("name=?", input.Name).First(&person).Error; err != nil {
		t.Fatal(err)
	}
	assertLatest := func(id string, names []string) {
		t.Helper()
		for i := len(b.upserts) - 1; i >= 0; i-- {
			row := b.upserts[i]
			if row.ID == id {
				if !reflect.DeepEqual(row.PersonNames, names) {
					t.Fatalf("%s names=%v want=%v", id, row.PersonNames, names)
				}
				if len(names) > 0 && !reflect.DeepEqual(row.PersonIDs, []string{person.ID}) {
					t.Fatalf("%s people=%v", id, row.PersonIDs)
				}
				return
			}
		}
		t.Fatalf("no refresh for %s", id)
	}
	assertLatest("search-movie", []string{"actor name"})
	assertLatest("search-series", []string{"actor name"})
	targets := []TranslationTarget{{Kind: "person_name", ID: person.ID, OriginalText: input.Name}}
	if err := repos.Person.ApplyCachedTranslation(t.Context(), targets, "演员姓名"); err != nil {
		t.Fatal(err)
	}
	assertLatest("search-movie", []string{"actor name", "演员姓名"})
	assertLatest("search-series", []string{"actor name", "演员姓名"})
	input.Name = "New Actor"
	if err := repos.Person.ReplaceCredits(t.Context(), "search-movie", []string{model.CreditTypeActor}, []CreditInput{input}); err != nil {
		t.Fatal(err)
	}
	assertLatest("search-movie", []string{"new actor"})
	assertLatest("search-series", []string{"new actor"})
	if err := repos.Person.ReplaceCredits(t.Context(), "search-season", []string{model.CreditTypeActor}, nil); err != nil {
		t.Fatal(err)
	}
	assertLatest("search-series", []string{})

	// 重建已读过旧文档时，后续改名仍需追补到新索引。
	b.indexed = nil
	b.onIndex = func() {
		b.onIndex = nil
		if err := repos.Person.ApplyCachedTranslation(t.Context(), []TranslationTarget{{Kind: "person_name", ID: person.ID, OriginalText: "New Actor"}}, "新演员"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repos.MediaView.BackfillSearchIndex(t.Context(), 100, 0); err != nil {
		t.Fatal(err)
	}
	if !b.activated {
		t.Fatal("rebuild not activated")
	}
	for i := len(b.indexed) - 1; i >= 0; i-- {
		if row := b.indexed[i]; row.ID == "search-movie" {
			if !reflect.DeepEqual(row.PersonNames, []string{"new actor", "新演员"}) {
				t.Fatalf("replayed names=%v", row.PersonNames)
			}
			return
		}
	}
	t.Fatal("rebuilt movie missing")
}

func TestPersonSearchDocumentsIncludeDirectSeriesFiles(t *testing.T) {
	repos := searchPlayableWorksFixture(t)
	for _, id := range []string{"direct-series", "direct-season-series"} {
		series := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindSeries, Title: id, Source: "local"})
		owner := series
		if id == "direct-season-series" {
			owner = createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, Title: "Season", Source: "local"})
		}
		var library model.Library
		if err := repos.DB.First(&library).Error; err != nil {
			t.Fatal(err)
		}
		if err := repos.DB.Create(&model.Media{MetadataID: owner.ID, LibraryID: library.ID, Path: "/media/direct/" + id + ".mkv"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	docs, err := repos.MediaView.metadataSearchDocuments(t.Context(), []string{"direct-series", "direct-season-series"})
	if err != nil || len(docs) != 2 {
		t.Fatalf("direct docs=%v err=%v", docs, err)
	}
}
