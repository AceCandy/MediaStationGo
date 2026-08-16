package service

import "testing"

func TestEmbyItemsCacheKeyIncludesFields(t *testing.T) {
	svc := &EmbyService{}
	base := ItemsParams{ParentID: "library-1", Limit: 50}
	withPeople := base
	withPeople.Fields = []string{"People"}
	withSources := base
	withSources.Fields = []string{"MediaSources"}
	if svc.embyItemsCacheKey("items", withPeople) == svc.embyItemsCacheKey("items", withSources) {
		t.Fatal("different Fields share an Items cache key")
	}
	personOne := base
	personOne.PersonIDs = []string{"person-1"}
	personTwo := base
	personTwo.PersonIDs = []string{"person-2"}
	if svc.embyItemsCacheKey("items", personOne) == svc.embyItemsCacheKey("items", personTwo) {
		t.Fatal("different PersonIDs share an Items cache key")
	}
}
