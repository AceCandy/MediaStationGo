package service

import (
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newTestEmbyService(t *testing.T) *EmbyService {
	t.Helper()
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.Favorite{}, &model.PlaybackHistory{}, &model.User{}, &model.Setting{}, &model.Person{}, &model.MetadataCredit{})
	// 内存库 + 异步探测协程：限制为单连接，避免连接池新建连接时
	// 拿到一个空白的 :memory: 实例（no such table）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repos := repository.New(db)
	return NewEmbyService(&config.Config{}, zap.NewNop(), repos)
}

func TestEmbyItemsPayloadQueriesDoNotScaleWithPageSize(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	person := model.Person{
		Base: model.Base{ID: "person-query-count"}, Name: "Actor", OriginalName: "Actor",
		NormalizedName: "actor", Source: "tmdb",
	}
	if err := svc.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	mediaIDs := []string{"media-query-1", "media-query-2", "media-query-3"}
	for i, mediaID := range mediaIDs {
		suffix := strconv.Itoa(i + 1)
		metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "metadata-query-" + suffix},
			Kind:          model.MetadataKindMovie, Title: "Movie " + suffix, Source: "tmdb",
		},
			model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: suffix},
			model.MetadataIdentifier{Provider: "imdb", EntityKind: model.MetadataKindMovie, ExternalID: "tt-query-" + suffix},
		)
		if err := svc.repo.DB.Create(&model.MetadataCredit{
			MetadataID: metadata.ID, PersonID: person.ID, Type: model.CreditTypeActor, Role: "Lead",
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.repo.DB.Create(&model.Media{
			PermanentBase: model.PermanentBase{ID: mediaID}, MetadataID: metadata.ID, LibraryID: lib.ID,
			Title: metadata.Title, Path: "/media/movies/" + mediaID + ".mkv",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	views, err := svc.repo.MediaView.FindByIDs(t.Context(), mediaIDs, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}

	queries := 0
	if err := svc.repo.DB.Callback().Query().Before("gorm:query").Register("test:count-emby-items-payload", func(*gorm.DB) {
		queries++
	}); err != nil {
		t.Fatal(err)
	}
	items := svc.payloadsForViews(t.Context(), views[:1], "")
	oneItemQueries := queries
	queries = 0
	allItems := svc.payloadsForViews(t.Context(), views, "")
	if queries != oneItemQueries {
		t.Fatalf("payload queries grew with page size: one=%d three=%d", oneItemQueries, queries)
	}
	if len(allItems) != len(views) {
		t.Fatalf("items = %d, want %d", len(allItems), len(views))
	}
	people := items[0]["People"].([]model.EmbyPerson)
	providers := items[0]["ProviderIds"].(map[string]string)
	sources := items[0]["MediaSources"].([]map[string]any)
	if len(people) != 1 || people[0].Id != person.ID || providers["Tmdb"] != "1" || providers["Imdb"] != "tt-query-1" || len(sources) != 1 || sources[0]["Id"] != mediaIDs[0] {
		t.Fatalf("list relations changed: people=%#v providers=%#v sources=%#v", people, providers, sources)
	}
	queries = 0
	minimalItems := svc.payloadsForViewsWithFields(t.Context(), views, "", []string{"PrimaryImageAspectRatio"})
	if queries >= oneItemQueries {
		t.Fatalf("minimal fields queries = %d, want fewer than full payload %d", queries, oneItemQueries)
	}
	for _, key := range []string{"People", "ProviderIds", "MediaSources"} {
		if _, ok := minimalItems[0][key]; ok {
			t.Fatalf("minimal list unexpectedly contains %s: %#v", key, minimalItems[0])
		}
	}
}

func TestEmbyFavoritePeopleItemsAreEmpty(t *testing.T) {
	svc := newTestEmbyService(t)
	person := model.Person{
		Base:           model.Base{ID: "favorite-person"},
		Name:           "Actor",
		OriginalName:   "Actor",
		NormalizedName: "actor",
		Source:         "tmdb",
	}
	if err := svc.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}

	out, err := svc.Persons(t.Context(), ItemsParams{
		UserID:     "user",
		Filters:    []string{"IsFavorite"},
		StartIndex: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if items := out["Items"].([]map[string]any); len(items) != 0 || out["TotalRecordCount"] != int64(0) || out["StartIndex"] != 3 {
		t.Fatalf("favorite people items = %#v, want empty page", out)
	}
}

func TestEmbyUnsupportedFavoriteItemTypesAreEmpty(t *testing.T) {
	svc := newTestEmbyService(t)
	for _, itemTypes := range [][]string{{"Season"}, {"Episode"}, {"Folder"}, {"Movie", "Season"}} {
		out, err := svc.Items(t.Context(), ItemsParams{
			UserID:           "user",
			IncludeItemTypes: itemTypes,
			Filters:          []string{"IsFavorite"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if items := out["Items"].([]map[string]any); len(items) != 0 || out["TotalRecordCount"] != int64(0) {
			t.Fatalf("favorite item types %v = %#v, want empty page", itemTypes, out)
		}
	}
}

func TestEmbySearchCombinesPersonAndMediaItemTypes(t *testing.T) {
	svc := newTestEmbyService(t)
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	person := model.Person{
		Base: model.Base{ID: "person-stephen-chow"}, Name: "周星驰", OriginalName: "Stephen Chow",
		NormalizedName: "周星驰", Source: "tmdb",
	}
	if err := svc.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: "metadata-chow-story"}, Kind: model.MetadataKindMovie, Title: "周星驰传", Year: 2024, Source: "tmdb"},
		{PermanentBase: model.PermanentBase{ID: "metadata-unrelated-credit"}, Kind: model.MetadataKindMovie, Title: "喜剧之王", Year: 1999, Source: "tmdb"},
	} {
		metadata := createServiceTestMetadata(t, svc.repo.DB, item)
		if err := svc.repo.DB.Create(&model.Media{
			PermanentBase: model.PermanentBase{ID: "media-" + metadata.ID}, MetadataID: metadata.ID, LibraryID: library.ID,
			Title: metadata.Title, Path: "/media/movies/" + metadata.ID + ".mkv",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.repo.DB.Create(&model.MetadataCredit{
		MetadataID: "metadata-unrelated-credit", PersonID: person.ID, Type: model.CreditTypeActor,
	}).Error; err != nil {
		t.Fatal(err)
	}

	out, err := svc.Items(t.Context(), ItemsParams{
		SearchTerm: "周星驰", IncludeItemTypes: []string{"Person", "Movie"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 2 || out["TotalRecordCount"] != int64(2) {
		t.Fatalf("mixed search = %#v, want Person and matching Movie", out)
	}
	if items[0]["Type"] != "Person" || items[0]["Id"] != person.ID || items[1]["Id"] != "metadata-chow-story" {
		t.Fatalf("mixed search order = %#v, want exact Person before containing Movie", items)
	}
	for _, item := range items {
		if item["Id"] == "metadata-unrelated-credit" {
			t.Fatalf("person credit expanded into unrelated movie: %#v", items)
		}
	}
	hints, err := svc.SearchHints(t.Context(), ItemsParams{
		SearchTerm: "周星驰", IncludeItemTypes: []string{"Person", "Movie"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	searchHints := hints["SearchHints"].([]map[string]any)
	if len(searchHints) != 2 || searchHints[0]["Type"] != "Person" || searchHints[0]["ItemId"] != person.ID {
		t.Fatalf("mixed search hints = %#v, want exact Person first", hints)
	}

	peopleOnly, err := svc.Items(t.Context(), ItemsParams{
		SearchTerm: "周星驰", IncludeItemTypes: []string{"Person", "MusicAlbum"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := peopleOnly["Items"].([]map[string]any); len(got) != 1 || got[0]["Id"] != person.ID {
		t.Fatalf("Person + unsupported type = %#v, want Person", peopleOnly)
	}

	unsupported, err := svc.Items(t.Context(), ItemsParams{
		SearchTerm: "周星驰", IncludeItemTypes: []string{"MusicAlbum"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := unsupported["Items"].([]map[string]any); len(got) != 0 {
		t.Fatalf("unsupported-only search = %#v, want empty", unsupported)
	}
	mediaWithUnsupported, err := svc.Items(t.Context(), ItemsParams{
		SearchTerm: "周星驰", IncludeItemTypes: []string{"Movie", "MusicAlbum"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mediaWithUnsupported["Items"].([]map[string]any); len(got) != 1 || got[0]["Id"] != "metadata-chow-story" {
		t.Fatalf("Movie + unsupported type = %#v, want matching Movie", mediaWithUnsupported)
	}

	browse, err := svc.Items(t.Context(), ItemsParams{IncludeItemTypes: []string{"Person", "Movie"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range browse["Items"].([]map[string]any) {
		if item["Type"] == "Person" {
			t.Fatalf("no-term browse unexpectedly merged people: %#v", browse)
		}
	}
}

func TestEmbyItemsFilterByPerson(t *testing.T) {
	svc := newTestEmbyService(t)
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	person := model.Person{Base: model.Base{ID: "person-1"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor", Source: "tmdb"}
	if err := svc.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"related", "unrelated"} {
		metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "metadata-" + id}, Kind: model.MetadataKindMovie, Title: id, Source: "tmdb"})
		if err := svc.repo.DB.Create(&model.Media{PermanentBase: model.PermanentBase{ID: "media-" + id}, MetadataID: metadata.ID, LibraryID: library.ID, Title: id, Path: "/media/movies/" + id + ".mkv"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.repo.DB.Create(&model.MetadataCredit{MetadataID: "metadata-related", PersonID: person.ID, Type: model.CreditTypeActor}).Error; err != nil {
		t.Fatal(err)
	}

	out, err := svc.Items(t.Context(), ItemsParams{PersonIDs: []string{person.ID}, IncludeItemTypes: []string{"Movie"}})
	if err != nil {
		t.Fatal(err)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Id"] != "metadata-related" {
		t.Fatalf("person items = %#v, want related work", items)
	}

	for _, id := range []string{"related-series", "unrelated-series"} {
		series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindSeries, Title: id, Source: "tmdb"})
		season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id + "-season"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"})
		episode := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id + "-episode"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Source: "tmdb"})
		if err := svc.repo.DB.Create(&model.Media{PermanentBase: model.PermanentBase{ID: "media-" + id}, MetadataID: episode.ID, LibraryID: library.ID, Title: id, Path: "/media/shows/" + id + "/S01E01.mkv", SeasonNum: 1, EpisodeNum: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.repo.DB.Create(&model.MetadataCredit{MetadataID: "related-series", PersonID: person.ID, Type: model.CreditTypeActor}).Error; err != nil {
		t.Fatal(err)
	}
	out, err = svc.Items(t.Context(), ItemsParams{PersonIDs: []string{person.ID}, IncludeItemTypes: []string{"Series"}})
	if err != nil {
		t.Fatal(err)
	}
	items = out["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Id"] != "related-series" {
		t.Fatalf("person series items = %#v, want related series", items)
	}
}

func TestEmbyFolderPayloadQueriesDoNotScaleWithPageSize(t *testing.T) {
	svc := newTestEmbyService(t)
	seriesGroups := make([]embySeriesGroup, 0, 3)
	seasonGroups := make([]embySeasonGroup, 0, 3)
	for i := 1; i <= 3; i++ {
		suffix := strconv.Itoa(i)
		series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "series-query-" + suffix}, Kind: model.MetadataKindSeries,
			Title: "Series " + suffix, Source: "tmdb",
		})
		season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "season-query-" + suffix}, Kind: model.MetadataKindSeason,
			ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb",
		})
		group := embySeriesGroup{ID: series.ID, Name: series.Title}
		seriesGroups = append(seriesGroups, group)
		seasonGroups = append(seasonGroups, embySeasonGroup{ID: season.ID, SeriesID: series.ID, Name: season.Title, Series: group})
	}

	queries := 0
	if err := svc.repo.DB.Callback().Query().Before("gorm:query").Register("test:count-emby-folder-payload", func(*gorm.DB) {
		queries++
	}); err != nil {
		t.Fatal(err)
	}
	svc.seriesPayloadsWithFields(t.Context(), seriesGroups[:1], "", nil)
	oneSeriesQueries := queries
	queries = 0
	svc.seriesPayloadsWithFields(t.Context(), seriesGroups, "", nil)
	if queries != oneSeriesQueries {
		t.Fatalf("series payload queries grew with page size: one=%d three=%d", oneSeriesQueries, queries)
	}

	queries = 0
	svc.seasonPayloadsWithFields(t.Context(), seasonGroups[:1], "", nil)
	oneSeasonQueries := queries
	queries = 0
	svc.seasonPayloadsWithFields(t.Context(), seasonGroups, "", nil)
	if queries != oneSeasonQueries {
		t.Fatalf("season payload queries grew with page size: one=%d three=%d", oneSeasonQueries, queries)
	}
}

func TestEmbyLatestItemsOrderByImportDate(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	base := time.Now()
	testRows := []struct {
		id, title, path, releaseDate string
		createdAt                    time.Time
	}{
		{"older-release-newer-scan", "旧上映新入库", `/media/movies/old.mkv`, "2026-01-10", base.Add(2 * time.Hour)},
		{"newer-release-older-scan", "新上映", `/media/movies/new.mkv`, "2026-06-23", base},
	}
	for _, row := range testRows {
		metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: "metadata-" + row.id}, Kind: model.MetadataKindMovie,
			Title: row.title, Year: 2026, ReleaseDate: row.releaseDate, Source: "tmdb",
		})
		media := model.Media{
			PermanentBase: model.PermanentBase{ID: row.id, CreatedAt: row.createdAt}, LibraryID: lib.ID, MetadataID: metadata.ID,
			Title: row.title, Path: row.path, Year: 2026, ScrapeStatus: "matched",
		}
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	items, err := svc.LatestItems(t.Context(), "", lib.ID, 10, false)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(items) != 2 || items[0]["Id"] != "metadata-older-release-newer-scan" {
		t.Fatalf("latest items should prefer created_at over release date, got %#v", items)
	}
	if items[0]["PremiereDate"] != "2026-01-10T00:00:00.0000000Z" {
		t.Fatalf("latest item should expose Emby-compatible PremiereDate: %#v", items[0])
	}
}
