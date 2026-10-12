package service

import "testing"

func TestLibrarySeriesHashIdentityDoesNotReadDatabase(t *testing.T) {
	svc := &MediaService{}
	for _, key := range []string{"", "series:12345678", "invalid", compactSeriesKey("metadata:work-id")} {
		id, err := svc.resolveLibrarySeriesKey(t.Context(), "library-id", key, MediaVisibility{})
		if err != nil || id != "" {
			t.Fatalf("retired identity %q resolved to %q: %v", key, id, err)
		}
	}
	if id, err := svc.resolveLibrarySeriesKey(t.Context(), "library-id", "metadata:work-id", MediaVisibility{}); err != nil || id != "work-id" {
		t.Fatalf("current identity resolved to %q: %v", id, err)
	}
}
