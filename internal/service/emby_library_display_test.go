package service

import (
	"reflect"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyLibraryDisplay(t *testing.T) {
	libs := make([]model.Library, 3)
	for i, id := range []string{"a", "b", "new"} {
		libs[i].ID = id
	}
	for _, tt := range []struct {
		name, value string
		want        []string
	}{
		{"default", "", []string{"a", "b", "new"}},
		{"empty", "[]", []string{"a", "b", "new"}},
		{"ordered", `[{"id":"b","hidden":false},{"id":"a","hidden":false}]`, []string{"b", "a", "new"}},
		{"hidden and deleted", `[{"id":"deleted","hidden":false},{"id":"a","hidden":true},{"id":"b","hidden":false}]`, []string{"b", "new"}},
		{"all hidden", `[{"id":"a","hidden":true},{"id":"b","hidden":true},{"id":"new","hidden":true}]`, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := DecodeEmbyLibraryDisplay(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0)
			for _, lib := range applyEmbyLibraryDisplay(libs, entries) {
				got = append(got, lib.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if libs[0].ID != "a" {
				t.Fatal("changed source library order")
			}
		})
	}
	for _, value := range []string{`null`, `{}`, `[null]`, `[{"id":"a"}]`, `[{"id":"a","hidden":null}]`, `[{"id":"","hidden":false}]`, `[{"id":"a","hidden":"false"}]`, `[{"id":"a","hidden":false},{"id":"a","hidden":true}]`} {
		if _, err := DecodeEmbyLibraryDisplay(value); err == nil {
			t.Fatalf("accepted invalid input %s", value)
		}
	}
}
