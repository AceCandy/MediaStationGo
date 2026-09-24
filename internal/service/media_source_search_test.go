package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestWebSourceSearch(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repo)
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"ordinary", "hongguo", "nfo", "hidden"} {
		create(&model.Library{Base: model.Base{ID: id}, Name: id, Type: id, Path: "/test/" + id})
	}
	for _, row := range []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: "ordinary"}, Kind: "movie", Title: "航海王", Source: "test"},
		{PermanentBase: model.PermanentBase{ID: "overview"}, Kind: "movie", Title: "其他电影", Overview: "航海王的故事", Source: "test"},
	} {
		create(&row)
		create(&model.Media{PermanentBase: model.PermanentBase{ID: row.ID + "-file"}, LibraryID: "ordinary", MetadataID: row.ID, Path: "/test/ordinary/" + row.ID})
	}
	create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: "local"}, LibraryID: "nfo", LocalKey: "local", Kind: "movie", NFOFields: model.NFOFields{Title: "寻找航海王", Genres: "冒险"}})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "local-file"}, LibraryID: "nfo", CatalogSource: "nfo", Path: "/test/nfo/local"})
	create(&model.NFOMediaBinding{MediaID: "local-file", ItemID: "local", NFOFields: model.NFOFields{Title: "寻找航海王", Genres: "冒险"}})
	create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: "genre"}, LibraryID: "nfo", LocalKey: "genre", Kind: "movie", NFOFields: model.NFOFields{Title: "本地类型命中", Genres: "航海王"}})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "genre-file"}, LibraryID: "nfo", CatalogSource: "nfo", Path: "/test/nfo/genre"})
	create(&model.NFOMediaBinding{MediaID: "genre-file", ItemID: "genre"})
	for _, row := range []model.HongGuoWork{
		{PermanentBase: model.PermanentBase{ID: "first"}, SourceID: "101", Kind: "series", Title: "航海王续篇", Overview: "首季资料", Tags: `["冒险"]`, RelatedAlbumID: "1000", SeasonIndex: 1},
		{PermanentBase: model.PermanentBase{ID: "second"}, SourceID: "102", Kind: "series", Title: "第二季名称", RelatedAlbumID: "1000", SeasonIndex: 2},
		{PermanentBase: model.PermanentBase{ID: "standalone"}, SourceID: "103", Kind: "series", Title: "航海王独立剧集"},
		{PermanentBase: model.PermanentBase{ID: "movie"}, SourceID: "104", Kind: "movie", Title: "航海王电影", Tags: `["冒险"]`},
		{PermanentBase: model.PermanentBase{ID: "hidden"}, SourceID: "105", Kind: "movie", Title: "航海王隐藏电影"},
		{PermanentBase: model.PermanentBase{ID: "unavailable"}, SourceID: "106", Kind: "movie", Title: "航海王无文件"},
	} {
		create(&row)
		if row.ID == "first" || row.ID == "unavailable" {
			continue
		}
		libraryID := "hongguo"
		if row.ID == "hidden" {
			libraryID = "hidden"
		}
		var episodeID *string
		if row.Kind == "series" {
			episode := model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: row.ID + "-episode"}, WorkID: row.ID, Number: 1}
			create(&episode)
			episodeID = &episode.ID
		}
		for _, suffix := range []string{"-file", "-version"} {
			fileID := row.ID + suffix
			create(&model.Media{PermanentBase: model.PermanentBase{ID: fileID}, LibraryID: libraryID, CatalogSource: "hongguo", LookupCatalogID: row.SourceID, Path: "/test/" + libraryID + "/" + fileID})
			create(&model.HongGuoMediaBinding{MediaID: fileID, WorkID: row.ID, EpisodeID: episodeID})
		}
	}
	firstWorkID, firstSourceID := "first", "101"
	create(&model.HongGuoArtwork{WorkID: &firstWorkID, SourceID: &firstSourceID, LocalKey: "poster", PermanentBase: model.PermanentBase{ID: "first-poster"}})
	ordinary := &sourceSearchBackend{ids: []string{"overview", "ordinary"}}
	source := &sourceSearchBackend{ids: []string{"hg-work-unavailable", "hg-work-hidden", "hg-work-movie", "hg-work-standalone", "hg-group-1000"}}
	repo.MediaView.SetSearchBackend(ordinary)
	repo.HongGuo.SetSearchBackend(source)
	visibility := MediaVisibility{IncludeNSFW: true, HiddenLibraryIDs: []string{"hidden"}}
	wantIDs := []string{"ordinary-file", "second-file", "movie-file", "standalone-file", "local-file", "genre-file", "overview-file"}
	for page, want := range wantIDs {
		items, total, err := svc.SearchMediaVisiblePageGrouped(t.Context(), "航海王", page+1, 1, visibility)
		if err != nil || total != int64(len(wantIDs)) || len(items) != 1 || items[0].ID != want {
			t.Fatalf("page %d: items=%+v total=%d err=%v", page+1, items, total, err)
		}
		if want == "second-file" {
			item := items[0]
			if item.SeriesID != "hg-group-1000" || item.Title != "航海王续篇" || item.Overview != "首季资料" || item.PosterURL != "/api/catalogs/hongguo/artwork/first-poster" || item.LookupCatalogID != "102" || item.MetadataID != "" || item.DisplayLibraryID != "hongguo" {
				t.Fatalf("album lost presentation or playable identity: %+v", item)
			}
		}
	}
	if len(ordinary.filters) != len(wantIDs) || len(source.filters) != len(wantIDs) {
		t.Fatal("a catalog bypassed its search backend")
	}
	suggestions, err := svc.SearchMediaVisibleGrouped(t.Context(), "航海王", 2, visibility)
	if err != nil || len(suggestions) != 2 || suggestions[1].SeriesID != "hg-group-1000" {
		t.Fatalf("top-bar suggestions: items=%v err=%v", suggestions, err)
	}
	items, total, err := svc.SearchMediaVisiblePage(t.Context(), "航海王", 10, 1, visibility)
	if err != nil || total != int64(len(wantIDs)) || len(items) != 0 {
		t.Fatalf("out-of-range page: items=%v total=%d err=%v", items, total, err)
	}
	for _, fallback := range []bool{false, true} {
		if fallback {
			ordinary.err, source.err = errors.New("unavailable"), errors.New("unavailable")
		}
		items, err := svc.SearchMediaVisible(t.Context(), "航海王", 100, visibility)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		if !reflect.DeepEqual(ids, wantIDs) {
			t.Fatalf("suggestions fallback=%v: ids=%v", fallback, ids)
		}
	}
	for _, scope := range []MediaVisibility{
		{LibraryRestricted: true},
		{IncludeNSFW: true, HiddenLibraryIDs: []string{"ordinary", "hongguo", "nfo", "hidden"}},
		{IncludeNSFW: true, AllowedLibraryIDs: []string{"hongguo"}, HiddenLibraryIDs: []string{"hongguo"}},
	} {
		items, total, err := svc.SearchMediaVisiblePage(t.Context(), "航海王", 1, 30, scope)
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("invisible results: items=%v total=%d err=%v", items, total, err)
		}
	}
	items, total, err = svc.SearchMediaVisiblePage(t.Context(), "航海王", 1, 30, MediaVisibility{AllowedLibraryIDs: []string{"hongguo"}})
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("source-only scope: items=%v total=%d err=%v", items, total, err)
	}
}
