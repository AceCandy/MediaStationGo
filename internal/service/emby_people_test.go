package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyPeopleFromCreditsLocalizesCommonCrewRoles(t *testing.T) {
	rows := []model.MetadataCredit{
		{Type: model.CreditTypeActor, Role: "Director", Person: model.Person{Name: "actor"}},
		{Type: model.CreditTypeGuestStar, Role: "Writer", Person: model.Person{Name: "guest-star"}},
		{Type: model.CreditTypeDirector, Role: "Director", Person: model.Person{Name: "director"}},
		{Type: model.CreditTypeWriter, Role: "Writer", Person: model.Person{Name: "writer"}},
		{Type: model.CreditTypeWriter, Role: "Screenplay", Person: model.Person{Name: "screenplay"}},
		{Type: model.CreditTypeWriter, Role: "Story", Person: model.Person{Name: "story"}},
		{Type: model.CreditTypeWriter, Role: "Teleplay", Person: model.Person{Name: "teleplay"}},
		{Type: model.CreditTypeWriter, Role: "编剧", Person: model.Person{Name: "localized"}},
		{Type: model.CreditTypeWriter, Role: "Creator", Person: model.Person{Name: "unknown"}},
	}
	want := map[string]model.EmbyPerson{
		"actor":      {Name: "actor", Role: "Director", Type: model.CreditTypeActor},
		"guest-star": {Name: "guest-star", Role: "Writer", Type: model.CreditTypeGuestStar},
		"director":   {Name: "director", Role: "导演", Type: model.CreditTypeDirector},
		"writer":     {Name: "writer", Role: "编剧", Type: model.CreditTypeWriter},
		"screenplay": {Name: "screenplay", Role: "编剧", Type: model.CreditTypeWriter},
		"story":      {Name: "story", Role: "故事创作", Type: model.CreditTypeWriter},
		"teleplay":   {Name: "teleplay", Role: "电视剧编剧", Type: model.CreditTypeWriter},
		"localized":  {Name: "localized", Role: "编剧", Type: model.CreditTypeWriter},
		"unknown":    {Name: "unknown", Role: "Creator", Type: model.CreditTypeWriter},
	}

	got := embyPeopleFromCredits(rows)
	if len(got) != len(want) {
		t.Fatalf("people count = %d, want %d", len(got), len(want))
	}
	for _, person := range got {
		if person != want[person.Name] {
			t.Errorf("person %q = %#v, want %#v", person.Name, person, want[person.Name])
		}
	}
}
