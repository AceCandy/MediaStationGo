package service

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestFavoriteLibraryMembershipSourcesAndVisibility(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureLatestMediaAddedTriggers(db); err != nil {
		t.Fatal(err)
	}
	svc := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	libs := []model.Library{
		{Base: model.Base{ID: "library-a"}, Name: "A", Path: "/test/a", Type: "movie"},
		{Base: model.Base{ID: "library-b"}, Name: "B", Path: "/test/b", Type: model.LibraryTypeHongGuo},
		{Base: model.Base{ID: "library-hidden"}, Name: "Hidden", Path: "/test/hidden", Type: "movie"},
	}
	for i := range libs {
		if err := db.Create(&libs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	movie := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "movie", Title: "Movie", Source: "local"})
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "Series", Source: "local"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", Title: "Season", Source: "local", ParentID: &series.ID})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", Title: "Episode", Source: "local", ParentID: &season.ID, EpisodeNum: 1})
	for _, id := range []string{movie.ID, episode.ID} {
		for _, lib := range libs {
			for version := 0; version < 2; version++ {
				media := model.Media{LibraryID: lib.ID, MetadataID: id, Path: fmt.Sprintf("%s/%s-%d.mkv", lib.Path, id, version)}
				if err := db.Create(&media).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	works := []model.HongGuoWork{
		{SourceID: "101", Kind: "series", Title: "First", RelatedAlbumID: "900", SeasonIndex: 1},
		{SourceID: "102", Kind: "series", Title: "Second", RelatedAlbumID: "900", SeasonIndex: 2},
		{SourceID: "103", Kind: "movie", Title: "Source movie"},
	}
	for i := range works {
		if err := db.Create(&works[i]).Error; err != nil {
			t.Fatal(err)
		}
		for _, lib := range libs[i%2:] {
			media := model.Media{LibraryID: lib.ID, CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: works[i].SourceID, Path: lib.Path + "/" + works[i].SourceID + ".mkv"}
			if err := db.Create(&media).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.HongGuoMediaBinding{MediaID: media.ID, WorkID: works[i].ID}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	local := model.NFOItem{LibraryID: libs[0].ID, LocalKey: "series", Kind: "series", NFOFields: model.NFOFields{Title: "NFO series"}}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{
		{"Id": movie.ID, "Type": "Movie", "ParentId": "unchanged"},
		{"Id": series.ID, "Type": "Series"},
		{"Id": "hg-group-900", "Type": "Series", "ParentId": ""},
		{"Id": "hg-work-" + works[2].ID, "Type": "Movie"},
		{"Id": "nfo-" + local.ID, "Type": "Series"},
	}
	// 覆盖已维护归属与尚未初始化归属；后者必须从页内绑定回查。
	for _, unknown := range []bool{false, true} {
		if unknown {
			for _, table := range []string{"metadata_items", "hongguo_works"} {
				if err := db.Exec("UPDATE " + table + " SET library_ids=NULL").Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, tc := range []struct {
			name       string
			visibility MediaVisibility
			want       []string
		}{
			{"all", MediaVisibility{IncludeNSFW: true}, []string{"library-a", "library-b", "library-hidden"}},
			{"hidden", MediaVisibility{HiddenLibraryIDs: []string{"library-hidden"}}, []string{"library-a", "library-b"}},
			{"allowed", MediaVisibility{AllowedLibraryIDs: []string{"library-b"}}, []string{"library-b"}},
			{"locked", MediaVisibility{LibraryRestricted: true}, []string{}},
		} {
			t.Run(fmt.Sprintf("%s/unknown=%t", tc.name, unknown), func(t *testing.T) {
				svc.visibilityCache = map[string]embyVisibilityCacheEntry{svc.repo.ReadCacheKey() + "viewer": {visibility: tc.visibility, expiresAt: time.Now().Add(time.Hour)}}
				out, err := svc.favoriteLibraryMembership(t.Context(), "viewer", items)
				if err != nil {
					t.Fatal(err)
				}
				for i, item := range out {
					want := tc.want
					if i == 4 {
						want = []string{}
						for _, id := range tc.want {
							if id == "library-a" {
								want = append(want, id)
							}
						}
					}
					if !reflect.DeepEqual(item["LibraryIds"], want) {
						t.Fatalf("item %d membership=%#v want=%v", i, item["LibraryIds"], want)
					}
					if item["Id"] != items[i]["Id"] || item["ParentId"] != items[i]["ParentId"] {
						t.Fatal("identity/parent changed")
					}
					if _, changed := items[i]["LibraryIds"]; changed {
						t.Fatal("cached input mutated")
					}
				}
			})
		}
	}
	svc.visibilityCache = nil
	if err := db.Exec("UPDATE metadata_items SET library_ids='[]' WHERE id=?", movie.ID).Error; err != nil {
		t.Fatal(err)
	}
	out, err := svc.favoriteLibraryMembership(t.Context(), "", items[:1])
	if err != nil || len(out[0]["LibraryIds"].([]string)) != 0 {
		t.Fatalf("known empty must not fall back: %v %v", out, err)
	}
}

func TestEmbyViewsExposeLibraryTypeWithoutChangingCollectionType(t *testing.T) {
	svc := &EmbyService{}
	for _, kind := range []string{model.LibraryTypeHongGuo, "tv", "movie"} {
		out := svc.libraryAsView(&model.Library{Type: kind})
		collection := "tvshows"
		if kind == "movie" {
			collection = "movies"
		}
		if out["LibraryType"] != kind || out["CollectionType"] != collection || out["Type"] != "CollectionFolder" {
			t.Fatalf("view compatibility: %v", out)
		}
	}
}
