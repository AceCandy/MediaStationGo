package service

import "testing"

func TestDedupeExternalMedia(t *testing.T) {
	in := []ExternalMediaResult{
		{Source: "tmdb", MediaType: "movie", TMDbID: 1, Title: "A"},
		{Source: "tmdb", MediaType: "movie", TMDbID: 1, Title: "A duplicate"},
		{Source: "douban", DoubanID: "2", Title: "B"},
	}
	got := dedupeExternalMedia(in)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
}
