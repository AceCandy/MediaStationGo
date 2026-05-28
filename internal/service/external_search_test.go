package service

import "testing"

func TestBuildSubscribeKeyword(t *testing.T) {
	if got := buildSubscribeKeyword("沙丘", 2024); got != "沙丘 2024" {
		t.Fatalf("keyword = %q", got)
	}
	if got := buildSubscribeKeyword("沙丘", 0); got != "沙丘" {
		t.Fatalf("keyword without year = %q", got)
	}
}

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
