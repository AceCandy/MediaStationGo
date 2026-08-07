package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestBackfillLibraryPeopleOnlyFillsMetadataWithoutCredits(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	library := model.Library{Name: "TV", Type: "tv", Path: "/tv"}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{LibraryID: library.ID, MetadataID: series.ID, Title: "Show", Path: "/tv/show.mkv", ScrapeStatus: "matched"}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := scraper.BackfillLibraryPeople(t.Context(), library.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Completed != 1 || result.Failed != 0 {
		t.Fatalf("result = %+v", result)
	}
	credits, err := repos.Person.ListCreditsWithPeople(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	hasActor := false
	for _, credit := range credits {
		hasActor = hasActor || credit.Type == model.CreditTypeActor && credit.Person.Name == "Test Actor"
	}
	if len(credits) != 2 || !hasActor {
		t.Fatalf("credits = %+v", credits)
	}
	second, err := scraper.BackfillLibraryPeople(t.Context(), library.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 0 {
		t.Fatalf("second result = %+v, want no candidates", second)
	}
}
