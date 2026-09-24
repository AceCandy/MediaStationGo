package repository

import "testing"

func TestMetadataSearchNumericAliasesAndTitleNumbers(t *testing.T) {
	for input, want := range map[string]string{
		"0": "零", "1": "一", "9": "九", "10": "十", "11": "十一",
		"20": "二十", "44": "四十四", "99": "九十九", "100": "一百",
		"零": "0", "十": "10", "四十四": "44", "一百": "100",
	} {
		got, ok := metadataSearchNumericAlias(input)
		if !ok || got != want {
			t.Fatalf("metadataSearchNumericAlias(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	for _, input := range []string{"101", "007", "一十", "一百零一", "〇", "两"} {
		if got, ok := metadataSearchNumericAlias(input); ok {
			t.Fatalf("metadataSearchNumericAlias(%q) = %q, true; want no alias", input, got)
		}
	}
	for title, want := range map[string]int{
		"死神10": 10, "死神十": 10, "第44号": 44, "第四十四号": 44,
		"死神100": 100, "死神1000": 0, "一百零一": 0, "死神10 1000": 10,
	} {
		if got := metadataSearchTitleNumber(title); got != want {
			t.Fatalf("metadataSearchTitleNumber(%q) = %d, want %d", title, got, want)
		}
	}
}

func TestRankMetadataSearchCandidates(t *testing.T) {
	groups := buildMetadataSearchTermGroups(MediaSearchTerms("死神"))
	ranked := rankMetadataSearchCandidates("死神", groups, []metadataSearchCandidate{
		{ID: "exact-old", Title: "死神", Year: 2004},
		{ID: "exact-a", Title: "死神", Year: 2022},
		{ID: "exact-new", Title: "死神", Year: 2022},
		{ID: "contains-10", Title: "新死神10", Year: 2030},
		{ID: "contains-2", Title: "死神2", Year: 2010},
		{ID: "other", Title: "死而复生的神100", Year: 2099},
		{ID: "irrelevant-death", Title: "出生死线", Year: 2099},
		{ID: "irrelevant-god", Title: "神墓", Year: 2099},
	}, MetadataSearchFieldsTitle)
	want := []string{"exact-a", "exact-new", "exact-old", "contains-2", "contains-10", "other"}
	if len(ranked) != len(want) {
		t.Fatalf("ranked IDs = %#v, want %#v", metadataSearchCandidateIDs(ranked), want)
	}
	for index, id := range want {
		if ranked[index].ID != id {
			t.Fatalf("ranked IDs = %#v, want %#v", metadataSearchCandidateIDs(ranked), want)
		}
	}
}

func TestRankMetadataSearchCandidatesUsesNumberAfterRelevance(t *testing.T) {
	groups := buildMetadataSearchTermGroups(MediaSearchTerms("死神"))
	ranked := rankMetadataSearchCandidates("死神", groups, []metadataSearchCandidate{
		{ID: "number-2", Title: "死神2", Year: 2020},
		{ID: "number-9", Title: "死神9", Year: 2020},
		{ID: "number-10-zh", Title: "死神十", Year: 2020},
		{ID: "number-10", Title: "死神10", Year: 2020},
		{ID: "number-10-old", Title: "死神10", Year: 2010},
	}, MetadataSearchFieldsTitle)
	wantIDs := []string{"number-10", "number-10-zh", "number-10-old", "number-9", "number-2"}
	wantNumbers := []int{10, 10, 10, 9, 2}
	for index, want := range wantNumbers {
		if got := metadataSearchTitleNumber(ranked[index].Title); got != want {
			t.Fatalf("ranked titles = %#v, number at %d = %d, want %d", metadataSearchCandidateIDs(ranked), index, got, want)
		}
		if ranked[index].ID != wantIDs[index] {
			t.Fatalf("ranked IDs = %#v, want %#v", metadataSearchCandidateIDs(ranked), wantIDs)
		}
	}
}

func TestRankMetadataSearchCandidatesUsesNumericEquivalence(t *testing.T) {
	groups := buildMetadataSearchTermGroups(MediaSearchTerms("44"))
	ranked := rankMetadataSearchCandidates("44", groups, []metadataSearchCandidate{
		{ID: "arabic-exact", Title: "44", Year: 2020},
		{ID: "chinese-exact", Title: "四十四", Year: 2024},
		{ID: "arabic-contains", Title: "第44号", Year: 2024},
		{ID: "chinese-contains", Title: "第四十四号", Year: 2023},
		{ID: "arabic-later", Title: "144 44", Year: 2022},
		{ID: "arabic-part", Title: "144", Year: 2030},
		{ID: "chinese-part", Title: "一百四十四", Year: 2030},
		{ID: "different-number", Title: "四十", Year: 2030},
	}, MetadataSearchFieldsTitle)
	want := []string{"chinese-exact", "arabic-exact", "arabic-contains", "chinese-contains", "arabic-later"}
	if len(ranked) != len(want) {
		t.Fatalf("ranked IDs = %#v, want %#v", metadataSearchCandidateIDs(ranked), want)
	}
	for index, id := range want {
		if ranked[index].ID != id {
			t.Fatalf("ranked IDs = %#v, want %#v", metadataSearchCandidateIDs(ranked), want)
		}
	}
	ordered := buildMetadataSearchTermGroups(MediaSearchTerms("44 死神"))
	if metadataSearchContainsGroups("144 死神 44", ordered) {
		t.Fatal("numeric substring must not satisfy ordered full containment")
	}
}

func TestRankMetadataSearchCandidatesRespectsFieldScope(t *testing.T) {
	groups := buildMetadataSearchTermGroups(MediaSearchTerms("死神 千年"))
	candidates := []metadataSearchCandidate{
		{ID: "contains", Title: "死神千年"},
		{ID: "cross-field", Title: "死神外传", Overview: "千年之战"},
	}
	web := rankMetadataSearchCandidates("死神 千年", groups, candidates, MetadataSearchFieldsWeb)
	if got := metadataSearchCandidateIDs(web); len(got) != 2 || got[0] != "contains" || got[1] != "cross-field" {
		t.Fatalf("Web ranked IDs = %#v", got)
	}
	emby := rankMetadataSearchCandidates("死神 千年", groups, candidates, MetadataSearchFieldsTitle)
	if got := metadataSearchCandidateIDs(emby); len(got) != 1 || got[0] != "contains" {
		t.Fatalf("Emby ranked IDs = %#v", got)
	}
}

func TestPageMetadataSearchCandidates(t *testing.T) {
	ranked := make([]metadataSearchCandidate, 100)
	for index := range ranked {
		ranked[index].ID = string(rune('a' + index%26))
	}
	ids, total := pageMetadataSearchCandidates(ranked, 98, 10)
	if total != 100 || len(ids) != 2 {
		t.Fatalf("page len=%d total=%d, want 2/100", len(ids), total)
	}
	ids, total = pageMetadataSearchCandidates(ranked[:2], -1, 0)
	if total != 2 || len(ids) != 2 {
		t.Fatalf("default page len=%d total=%d, want 2/2", len(ids), total)
	}
	ids, total = pageMetadataSearchCandidates(ranked, 100, 10)
	if total != 100 || len(ids) != 0 {
		t.Fatalf("past-end page len=%d total=%d, want 0/100", len(ids), total)
	}
}

func TestRankMetadataSearchCandidatePageKeepsKindsAndPagesAfterRanking(t *testing.T) {
	candidates := []MetadataSearchCandidate{
		{Kind: "movie", ID: "same-id", Title: "周星驰传", Year: 2024},
		{Kind: "person", ID: "same-id", Title: "周星驰"},
		{Kind: "person", ID: "same-id", Title: "重复候选"},
	}
	ranked, total := RankMetadataSearchCandidatePage("周星驰", candidates, 0, 1)
	if total != 2 || len(ranked) != 1 {
		t.Fatalf("ranked len=%d total=%d, want 1/2", len(ranked), total)
	}
	if ranked[0].Kind != "person" || ranked[0].ID != "same-id" || ranked[0].Title != "周星驰" {
		t.Fatalf("first candidate = %#v, want exact Person", ranked[0])
	}
}

func TestRankMetadataSearchCandidatePageCapsBeforePaging(t *testing.T) {
	candidates := make([]MetadataSearchCandidate, 101)
	for index := range candidates {
		candidates[index] = MetadataSearchCandidate{
			Kind: "movie", ID: string(rune(index + 1)), Title: "周星驰外传",
		}
	}
	ranked, total := RankMetadataSearchCandidatePage("周星驰", candidates, 98, 10)
	if total != 100 || len(ranked) != 2 {
		t.Fatalf("ranked len=%d total=%d, want 2/100", len(ranked), total)
	}
}

func TestRankWebMetadataSearchCandidatePagePreservesFieldsAndTieOrder(t *testing.T) {
	candidates := []MetadataSearchCandidate{
		{Kind: "movie", ID: "b", Title: "航海王"},
		{Kind: "series", ID: "a", Title: "航海王"},
		{Kind: "movie", ID: "overview", Title: "其他电影", Overview: "航海王"},
		{Kind: "movie", ID: "nfo-genres", Title: "本地电影", Genres: "航海王"},
		{Kind: "movie", ID: "b", Title: "重复候选"},
	}
	want := []string{"a", "b", "nfo-genres", "overview"}
	for offset, id := range want {
		rows, total := RankWebMetadataSearchCandidatePage("航海王", candidates, offset, 1)
		if total != 4 || len(rows) != 1 || rows[0].ID != id {
			t.Fatalf("offset=%d rows=%v total=%d", offset, rows, total)
		}
	}
	rows, total := RankMetadataSearchCandidatePage("航海王", candidates, 0, 10)
	if total != 2 || rows[0].ID != "b" || rows[1].ID != "a" || candidates[0].Kind != "movie" {
		t.Fatalf("title search or caller candidates changed: %v", rows)
	}
}

func metadataSearchCandidateIDs(candidates []metadataSearchCandidate) []string {
	ids := make([]string, len(candidates))
	for index := range candidates {
		ids[index] = candidates[index].ID
	}
	return ids
}
