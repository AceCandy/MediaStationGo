package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestBackfillPeopleFillsGlobalMetadataWithoutCredits(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	scraper.SetTaskTracker(NewTaskTrackerService(nil, nil))
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.runPeopleBackfillPass(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	snapshot := scraper.tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Name != "人物信息补齐" || snapshot.Recent[0].Metrics["completed"] != 1 {
		t.Fatalf("task snapshot = %+v", snapshot)
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
	var hydrated model.MetadataItem
	if err := repos.DB.First(&hydrated, "id = ?", series.ID).Error; err != nil || hydrated.PeopleHydratedAt == nil {
		t.Fatalf("people hydrated at = %v, err=%v", hydrated.PeopleHydratedAt, err)
	}
	second, err := scraper.BackfillLibraryPeople(t.Context(), "another-library", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 0 {
		t.Fatalf("second result = %+v, want no candidates", second)
	}
}

func TestBackfillPeopleDoesNotRetrySuccessfulEmptyCredits(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Empty Credits", Source: "tmdb"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "12345"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.persistCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor, model.CreditTypeDirector, model.CreditTypeWriter}, nil); err != nil {
		t.Fatal(err)
	}

	candidates, err := scraper.pendingPeopleBackfillCandidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", candidates)
	}
}
