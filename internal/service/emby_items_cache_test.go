package service

import "testing"

type embyItemsCacheValue struct {
	Items            []map[string]any `json:"items"`
	TotalRecordCount int64            `json:"total_record_count"`
	StartIndex       int              `json:"start_index"`
}

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
	withoutTotal := base
	withoutTotal.SkipTotalRecordCount = true
	if svc.embyItemsCacheKey("items", base) == svc.embyItemsCacheKey("items", withoutTotal) {
		t.Fatal("counted and uncounted pages share an Items cache key")
	}
}
