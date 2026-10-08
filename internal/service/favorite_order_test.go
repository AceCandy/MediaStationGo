package service

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestFavoriteAddedOrderAcrossSources(t *testing.T) {
	e := nfoBrowseFixture(t, 2, 1)
	db, ctx := e.repo.DB, t.Context()
	old := time.Now().UTC().Add(-time.Hour)
	create := func(row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, lib := range []model.Library{
		{Base: model.Base{ID: "movies"}, Name: "Movies", Path: "/movies", Type: "movie"},
		{Base: model.Base{ID: "hongguo"}, Name: "HongGuo", Path: "/hongguo", Type: model.LibraryTypeHongGuo},
		{Base: model.Base{ID: "huangguoai"}, Name: "HuangGuoAI", Path: "/huangguoai", Type: model.LibraryTypeHuangGuoAI},
	} {
		create(&lib)
	}
	for i := 1; i <= 2; i++ {
		n := fmt.Sprint(i)
		at := old.Add(time.Duration(i) * time.Minute)
		create(&model.NFOUserState{UserID: "viewer", ItemID: "show-" + n, Favorite: true, FavoriteAddedAt: &at, UpdatedAt: old.Add(-time.Duration(i) * time.Minute)})
		metadata := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "movie-" + n}, Kind: "movie", Title: "Movie " + n})
		create(&model.Media{PermanentBase: model.PermanentBase{ID: "movie-file-" + n}, LibraryID: "movies", MetadataID: metadata.ID, Path: "/movies/" + n})
		create(&model.Favorite{Base: model.Base{CreatedAt: at.Add(time.Minute * 10)}, UserID: "viewer", MetadataID: metadata.ID, MediaID: "movie-file-" + n})
		work := model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "hg-" + n}, SourceID: "70000" + n, Kind: "series", RelatedAlbumID: "90000" + n, SeasonIndex: 1, Title: "HongGuo " + n}
		create(&work)
		create(&model.Media{PermanentBase: model.PermanentBase{ID: "hg-file-" + n}, LibraryID: "hongguo", Path: "/hongguo/" + n, CatalogSource: "hongguo", LookupCatalogID: work.SourceID})
		hgEpisode := model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "hg-ep-" + n}, WorkID: work.ID, Number: 1}
		create(&hgEpisode)
		create(&model.HongGuoMediaBinding{MediaID: "hg-file-" + n, WorkID: work.ID, EpisodeID: &hgEpisode.ID})
		create(&model.HongGuoFavorite{UserID: "viewer", ItemID: "hg-group-" + work.RelatedAlbumID, Favorite: true, UpdatedAt: at.Add(time.Minute * 20)})
		hga := model.HuangGuoAIWork{PermanentBase: model.PermanentBase{ID: "hga-" + n}, SourceID: "80000" + n, SourceCategory: "ai-huanlian", Kind: "movie", Title: "HuangGuoAI " + n}
		create(&hga)
		ep := model.HuangGuoAIEpisode{PermanentBase: model.PermanentBase{ID: "hga-ep-" + n}, WorkID: hga.ID, Number: 1}
		create(&ep)
		create(&model.Media{PermanentBase: model.PermanentBase{ID: "hga-file-" + n}, LibraryID: "huangguoai", Path: "/huangguoai/" + n, CatalogSource: "huangguoai", LookupCatalogID: hga.SourceID})
		create(&model.HuangGuoAIMediaBinding{MediaID: "hga-file-" + n, WorkID: hga.ID, EpisodeID: ep.ID})
		create(&model.HuangGuoAIFavorite{UserID: "viewer", SourceID: hga.SourceID, Favorite: true, UpdatedAt: at.Add(time.Minute * 30)})
	}
	check := func(parent string, want []string) {
		t.Helper()
		for _, skipCount := range []bool{false, true} {
			for start := 0; start < len(want); start++ {
				page, err := e.Items(ctx, ItemsParams{UserID: "viewer", ParentID: parent, Recursive: true, Filters: []string{"IsFavorite"}, IncludeItemTypes: []string{"Movie", "Series"}, StartIndex: start, Limit: 1, SkipTotalRecordCount: skipCount})
				if err != nil {
					t.Fatal(err)
				}
				items := page["Items"].([]map[string]any)
				if !skipCount && page["TotalRecordCount"] != int64(len(want)) {
					t.Fatalf("favorite count: %v, want %d", page["TotalRecordCount"], len(want))
				}
				if len(items) != 1 || items[0]["Id"] != want[start] {
					t.Fatalf("parent=%s countOff=%v offset=%d: %v, want %s", parent, skipCount, start, page, want[start])
				}
			}
		}
	}
	check("", []string{"hga-work-hga-2", "hga-work-hga-1", "hg-group-900002", "hg-group-900001", "movie-2", "movie-1", "nfo-show-2", "nfo-show-1"})
	check("movies", []string{"movie-2", "movie-1"})
	check("hongguo", []string{"hg-group-900002", "hg-group-900001"})
	check("huangguoai", []string{"hga-work-hga-2", "hga-work-hga-1"})
	check("library-nfo", []string{"nfo-show-2", "nfo-show-1"})
	playback := NewPlaybackService(zap.NewNop(), e.repo)
	items, err := playback.ListFavourites(ctx, "viewer", MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, item := range items {
		titles = append(titles, item.Title)
	}
	if !reflect.DeepEqual(titles, []string{"Movie 2", "Movie 1", "Title 2", "Title 1"}) {
		t.Fatalf("web favorites: %v", titles)
	}
	// 显式名称排序仍由客户端决定。
	page, err := e.Items(ctx, ItemsParams{UserID: "viewer", ParentID: "movies", Filters: []string{"IsFavorite"}, IncludeItemTypes: []string{"Movie"}, SortBy: "SortName", SortOrder: "Ascending", Limit: 10})
	if err != nil || page["Items"].([]map[string]any)[0]["Id"] != "movie-1" {
		t.Fatalf("explicit sort: %v %v", page, err)
	}
	// 重复收藏保留时间；取消后重加按新的添加时间排序。
	if _, err := e.repo.Favorite.SetByIdentity(ctx, "viewer", "movie-1", "movie-file-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.Favorite.SetByIdentity(ctx, "viewer", "movie-1", "movie-file-1", true); err != nil {
		t.Fatal(err)
	}
	check("movies", []string{"movie-1", "movie-2"})
	if err := e.repo.NFO.SetFavorite(ctx, "viewer", "nfo-show-1", "file-ep-1-1-1", true); err != nil {
		t.Fatal(err)
	}
	check("library-nfo", []string{"nfo-show-2", "nfo-show-1"})
	if err := e.repo.HongGuo.SetFavorite(ctx, "viewer", "700001", true); err != nil {
		t.Fatal(err)
	}
	check("hongguo", []string{"hg-group-900002", "hg-group-900001"})
	value := true
	if _, err := e.repo.HuangGuoAI.Favorite(ctx, "viewer", "800001", &value); err != nil {
		t.Fatal(err)
	}
	check("huangguoai", []string{"hga-work-hga-2", "hga-work-hga-1"})
	for _, favorite := range []bool{false, true} {
		if err := e.repo.NFO.SetFavorite(ctx, "viewer", "nfo-show-1", "file-ep-1-1-1", favorite); err != nil {
			t.Fatal(err)
		}
	}
	check("library-nfo", []string{"nfo-show-1", "nfo-show-2"})
}

func TestNFOFavoriteAddedTimeSurvivesPlayback(t *testing.T) {
	e := nfoBrowseFixture(t, 0, 0)
	db, ctx := e.repo.DB, t.Context()
	for _, row := range []any{
		&model.NFOItem{PermanentBase: model.PermanentBase{ID: "local-movie"}, LibraryID: "library-nfo", LocalKey: "local-movie", Kind: "movie", NFOFields: model.NFOFields{Title: "Local movie"}},
		&model.Media{PermanentBase: model.PermanentBase{ID: "local-file"}, LibraryID: "library-nfo", Path: "/local-movie.mkv", CatalogSource: "nfo"},
		&model.NFOMediaBinding{MediaID: "local-file", ItemID: "local-movie", Fingerprint: "fixture"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	view, err := e.repo.MediaView.FindByID(ctx, "local-file")
	if err != nil || view == nil {
		t.Fatalf("view: %v %v", view, err)
	}
	if err := e.repo.NFO.SetFavorite(ctx, "viewer", "nfo-local-movie", view.ID, true); err != nil {
		t.Fatal(err)
	}
	read := func() model.NFOUserState {
		t.Helper()
		var state model.NFOUserState
		if err := db.First(&state, "user_id=? AND item_id=?", "viewer", "local-movie").Error; err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := read()
	if before.FavoriteAddedAt == nil {
		t.Fatal("favorite time is missing")
	}
	if err := e.repo.NFO.RecordProgress(ctx, "viewer", "", *view, 30000, 120000, false); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.NFO.MarkPlayed(ctx, "viewer", *view, true); err != nil {
		t.Fatal(err)
	}
	after := read()
	if !after.Favorite || after.FavoriteAddedAt == nil || !after.FavoriteAddedAt.Equal(*before.FavoriteAddedAt) || !after.Completed {
		t.Fatalf("playback changed favorite: before=%+v after=%+v", before, after)
	}
}

func TestOrdinarySeriesFavoriteAddedOrder(t *testing.T) {
	e := nfoBrowseFixture(t, 0, 0)
	db, ctx := e.repo.DB, t.Context()
	lib := model.Library{Base: model.Base{ID: "shows"}, Name: "Shows", Path: "/shows", Type: "tv"}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for i := 1; i <= 2; i++ {
		n := fmt.Sprint(i)
		series := createServiceTestMetadata(t, db, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "series-" + n}, Kind: "series", Title: "Series " + n})
		season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", Title: "Season", ParentID: &series.ID, SeasonNum: 1})
		episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", Title: "Episode", ParentID: &season.ID, EpisodeNum: 1})
		media := model.Media{LibraryID: lib.ID, MetadataID: episode.ID, Path: "/shows/" + n, SeasonNum: 1, EpisodeNum: 1}
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Favorite{Base: model.Base{CreatedAt: old.Add(time.Duration(i) * time.Minute)}, UserID: "viewer", MetadataID: series.ID, MediaID: media.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 历史或并发产生的重复收藏行不能使一个作品重复占据分页位置。
	if err := db.Create(&model.Favorite{Base: model.Base{CreatedAt: old}, UserID: "viewer", MetadataID: "series-2"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{"", "shows"} {
		for start, want := range []string{"series-2", "series-1"} {
			page, err := e.Items(ctx, ItemsParams{UserID: "viewer", ParentID: parent, IncludeItemTypes: []string{"Series"}, Filters: []string{"IsFavorite"}, Limit: 1, StartIndex: start})
			if err != nil {
				t.Fatal(err)
			}
			items := page["Items"].([]map[string]any)
			if len(items) != 1 || items[0]["Id"] != want || page["TotalRecordCount"] != 2 {
				t.Fatalf("parent=%s offset=%d: %v, want %s", parent, start, page, want)
			}
		}
	}
}
