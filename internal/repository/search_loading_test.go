package repository

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestNFOSearchBatchPreservesPresentationAndVisibility(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	create := func(value any) {
		t.Helper()
		if err := repos.DB.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	lib := model.Library{Name: "Local", Path: "/test/local", Type: "mixed"}
	create(&lib)
	seriesID, seasonID := "series", "season"
	for _, item := range []model.NFOItem{
		{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: "series", NFOFields: model.NFOFields{Title: "航海王", Year: 1999, PosterAssetID: "poster"}},
		{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: "season", ParentID: &seriesID, SeasonNum: 1},
		{PermanentBase: model.PermanentBase{ID: "episode"}, Kind: "episode", ParentID: &seasonID, EpisodeNum: 2},
		{PermanentBase: model.PermanentBase{ID: "movie"}, Kind: "movie", NFOFields: model.NFOFields{Title: "航海王电影", OriginalName: "One Piece", Overview: "test", Rating: 8}},
		{PermanentBase: model.PermanentBase{ID: "adult"}, Kind: "movie", NFOFields: model.NFOFields{Title: "航海王", NSFW: true}},
	} {
		item.LibraryID, item.LocalKey = lib.ID, item.ID
		create(&item)
	}
	for _, id := range []string{"episode", "movie", "adult"} {
		for _, suffix := range []string{"z", "a"} {
			fileID := id + suffix
			create(&model.Media{PermanentBase: model.PermanentBase{ID: fileID}, LibraryID: lib.ID, CatalogSource: "nfo", Path: "/test/local/" + fileID})
			create(&model.NFOMediaBinding{MediaID: fileID, ItemID: id, NFOFields: model.NFOFields{Title: "file title"}})
		}
	}
	ids := []string{"nfo-movie", "nfo-series", "nfo-season", "nfo-episode", "nfo-adult", "nfo-missing"}
	queries := 0
	count := func(db *gorm.DB) {
		if !db.DryRun {
			queries++
		}
	}
	if err := repos.DB.Callback().Query().After("gorm:query").Register("test:batch-query", count); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Callback().Row().After("gorm:row").Register("test:batch", count); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []MediaQueryFilter{
		{}, {IncludeNSFW: true}, {HiddenLibraryIDs: []string{lib.ID}}, {AllowedLibraryIDs: []string{"other"}},
		{MissingPoster: true}, {MissingChineseTitle: true},
	} {
		var want []model.MediaView
		for _, id := range ids {
			files, err := repos.MediaView.NFOItemViews(t.Context(), id, filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) == 0 {
				continue
			}
			view, err := repos.MediaView.NFOPresentation(t.Context(), id, filter.IncludeNSFW)
			if err != nil {
				t.Fatal(err)
			}
			if view == nil {
				continue
			}
			view.ID, view.LookupCatalogID = files[0].ID, strings.TrimPrefix(id, "nfo-")
			want = append(want, *view)
		}
		queries = 0
		got, err := repos.MediaView.FindMetadataSearchRepresentatives(t.Context(), ids, filter)
		if err != nil {
			t.Fatal(err)
		}
		if queries != 1 {
			t.Fatalf("batch queries=%d, want 1", queries)
		}
		if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
			t.Fatalf("batch differs from single-item presentation: filter=%+v", filter)
		}
		searchFilter := MetadataSearchFilter{MediaQueryFilter: filter, Kinds: []string{"movie", "series"}}
		for _, query := range []string{"", "航海王", "One Piece", "not-found"} {
			groups := buildMetadataSearchTermGroups(MediaSearchTerms(query))
			var before, after []string
			oldFiles := repos.MediaView.nfoViewQuery(t.Context(), filter).Select("COALESCE(nw.id,ni.id)")
			oldQuery := repos.DB.Table("nfo_items AS search_metadata").Where("id IN (?)", oldFiles).Where("kind IN ?", searchFilter.Kinds)
			if err := applyMetadataSearchLIKEFilter(oldQuery, groups, searchFilter.Fields).Order("id").Pluck("id", &before).Error; err != nil {
				t.Fatal(err)
			}
			if err := repos.MediaView.nfoSearchQuery(t.Context(), searchFilter, groups).Order("id").Pluck("id", &after).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("candidate visibility changed: before=%v after=%v", before, after)
			}
		}
	}
}
