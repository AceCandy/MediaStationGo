package model

import "testing"

func TestCatalogBasicsComplete(t *testing.T) {
	for _, tc := range []struct {
		kind, title, overview, image, date string
		complete                           bool
	}{
		{"movie", "中文电影", "简介", "/poster.jpg", "", true},
		{"series", "Series", "简介", "/poster.jpg", "", false},
		{"season", "第 1 季", "简介", "/poster.jpg", "", true},
		{"episode", "第 3 集", "简介", "/still.jpg", "2026-01-01", false},
		{"episode", "重逢", "简介", "/still.jpg", "", false},
		{"episode", "重逢", "简介", "/still.jpg", "2026-01-01", true},
		{"movie", "中文电影", " ", "/poster.jpg", "", false},
		{"movie", "中文电影", "简介", "", "", false},
		{"movie", "日本の映画", "简介", "/poster.jpg", "", false},
	} {
		if got := CatalogBasicsComplete(tc.kind, tc.title, tc.overview, tc.image, tc.date); got != tc.complete {
			t.Errorf("%+v: complete=%v", tc, got)
		}
	}
}
