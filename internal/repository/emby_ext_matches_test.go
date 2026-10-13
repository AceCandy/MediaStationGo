package repository

import (
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"testing"
)

func TestExtensionMatchesExactIdentityAndVisibleFiles(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Library{}, &model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	create := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	repos := New(db)
	lib := model.Library{Name: "visible", Path: "/visible", Enabled: true}
	hidden := model.Library{Name: "hidden", Path: "/hidden", Enabled: true}
	create(&lib)
	create(&hidden)
	makeWork := func(source, kind, id string) *model.MetadataItem {
		return createTestMetadata(t, repos, model.MetadataItem{Kind: kind, Title: "同名作品", Source: source}, model.MetadataIdentifier{Provider: source, EntityKind: kind, ExternalID: id})
	}
	movie := makeWork("tmdb", "movie", "101")
	series := makeWork("tmdb", "series", "101")
	makeWork("tmdb", "movie", "102")
	private := makeWork("douban", "movie", "101")
	anime := makeWork("bangumi", "series", "101")
	season := createTestMetadata(t, repos, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 0, Title: "特别篇", Source: "tmdb"})
	episode := createTestMetadata(t, repos, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 7, Title: "第七集", Source: "tmdb"})
	for _, row := range []model.Media{
		{MetadataID: movie.ID, LibraryID: lib.ID, Path: "/visible/movie"},
		{MetadataID: movie.ID, LibraryID: lib.ID, Path: "/visible/version2"},
		{MetadataID: episode.ID, LibraryID: lib.ID, Path: "/visible/episode"},
		{MetadataID: private.ID, LibraryID: hidden.ID, Path: "/hidden/movie"},
		{MetadataID: anime.ID, LibraryID: lib.ID, Path: "/visible/anime"},
	} {
		create(&row)
	}
	zero, seven, eight := 0, 7, 8
	items := []ExtensionIdentity{
		{Key: "movie", Source: "tmdb", ExternalID: "101", MediaType: "movie"},
		{Key: "show", Source: "tmdb", ExternalID: "101", MediaType: "tv"},
		{Key: "episode", Source: "tmdb", ExternalID: "101", MediaType: "series", Season: &zero, Episode: &seven},
		{Key: "absent_episode", Source: "tmdb", ExternalID: "101", MediaType: "series", Season: &zero, Episode: &eight},
		{Key: "fileless", Source: "tmdb", ExternalID: "102", MediaType: "movie"},
		{Key: "private", Source: "douban", ExternalID: "101", MediaType: "movie"},
		{Key: "anime", Source: "bangumi", ExternalID: "101", MediaType: "anime"},
	}
	filter := MediaQueryFilter{AllowedLibraryIDs: []string{lib.ID}}
	assertMatches := func(want map[string]string) {
		t.Helper()
		got, err := repos.MediaView.ExtensionLibraryMatches(t.Context(), items, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("got %+v want %+v", got, want)
		}
		for _, m := range got {
			if want[m.Key] != m.ItemID {
				t.Fatalf("unexpected match %+v", m)
			}
			if m.Key == "episode" && m.ItemType != "Episode" {
				t.Fatalf("wrong type %+v", m)
			}
		}
	}
	assertMatches(map[string]string{"movie": movie.ID, "show": series.ID, "episode": episode.ID, "anime": anime.ID})
	filter.HiddenLibraryIDs = []string{lib.ID}
	assertMatches(map[string]string{})
	filter = MediaQueryFilter{AllowedLibraryIDs: []string{"__locked__"}}
	assertMatches(map[string]string{})
	filter = MediaQueryFilter{}
	assertMatches(map[string]string{"movie": movie.ID, "show": series.ID, "episode": episode.ID, "anime": anime.ID, "private": private.ID})
	if err := db.Model(&lib).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	assertMatches(map[string]string{"private": private.ID})
	for _, bad := range [][]ExtensionIdentity{nil, make([]ExtensionIdentity, 101), {items[0], items[0]}, {{Key: "bad", Source: "tmdb", ExternalID: "-1", MediaType: "movie"}}, {{Key: "bad", Source: "tmdb", ExternalID: "1", MediaType: "movie", Season: &zero, Episode: &seven}}} {
		if _, err := repos.MediaView.ExtensionLibraryMatches(t.Context(), bad, filter); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
}
