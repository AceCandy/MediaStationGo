package service

import (
	"errors"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoEmbyPlayableIdentityAndUserState(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	ctx := t.Context()
	if err := e.repo.DB.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "红果", Path: "/test/hg", Type: model.LibraryTypeHongGuo}
	if err := e.repo.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	work, err := e.repo.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "900000000000000001", Title: "测试电影", EpisodeCount: 1, TotalEpisodes: 1, Completed: true, People: []hongguo.Person{{SourceID: "92001", Name: "测试演员", Subtitle: "演员卡片文案"}}, Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	m := model.Media{LibraryID: library.ID, Path: "/test/hg/movie.strm", CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: work.SourceID}
	if err := e.repo.Media.Upsert(ctx, &m); err != nil {
		t.Fatal(err)
	}
	if err := (&ScraperService{repo: e.repo}).enrichOneWithOptions(ctx, &m, ScrapeOptions{}); err == nil {
		t.Fatal("source file entered legacy scraper")
	}
	if _, err := (&ScraperService{repo: e.repo}).ManualSearch(ctx, &m, "测试", "tmdb", "movie"); err == nil {
		t.Fatal("source file entered legacy manual search")
	}
	if _, err := (&ScraperService{repo: e.repo}).ApplyManualMatch(ctx, m.ID, ManualScrapeRequest{Title: "不能写入旧体系", Source: "manual"}); err == nil {
		t.Fatal("source file entered legacy manual match")
	}
	id := "hg-work-" + work.ID
	title := "不能写入旧体系"
	if _, err := (&MediaService{repo: e.repo}).UpdateMetadata(ctx, m.ID, MediaMetadataUpdate{Title: &title}); err == nil {
		t.Fatal("source file entered legacy metadata editor")
	}
	var legacyCount int64
	if err := db.Model(&model.MetadataItem{}).Count(&legacyCount).Error; err != nil || legacyCount != 0 {
		t.Fatalf("source mutation created legacy metadata: count=%d err=%v", legacyCount, err)
	}
	info, err := e.PlaybackInfo(ctx, id, "user-a")
	if err != nil || info == nil {
		t.Fatalf("info=%v err=%v", info, err)
	}
	sources := info["MediaSources"].([]map[string]any)
	if len(sources) != 1 || sources[0]["Id"] != m.ID {
		t.Fatalf("wrong sources: %v", sources)
	}
	if err := e.RecordProgress(ctx, "user-a", id, m.ID, "session-a", 40000*10000, 120000*10000); err != nil {
		t.Fatal(err)
	}
	if err := e.SetFavorite(ctx, "user-a", id, true); err != nil {
		t.Fatal(err)
	}
	resume, err := e.ResumeItems(ctx, "user-a", 20)
	if err != nil {
		t.Fatal(err)
	}
	resumeItems := resume["Items"].([]map[string]any)
	if len(resumeItems) != 1 || resumeItems[0]["Id"] != id {
		t.Fatalf("source resume: %v", resume)
	}
	version := model.Media{LibraryID: library.ID, Path: "/test/hg/movie-v2.strm", CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: work.SourceID}
	if err := e.repo.Media.Upsert(ctx, &version); err != nil {
		t.Fatal(err)
	}
	resume, err = e.ResumeItems(ctx, "user-a", 20)
	if err != nil {
		t.Fatal(err)
	}
	resumeSources := resume["Items"].([]map[string]any)[0]["MediaSources"].([]map[string]any)
	if len(resumeSources) != 2 || resumeSources[0]["Id"] != m.ID {
		t.Fatalf("resume must expose versions and prefer last played: %v", resumeSources)
	}
	item, err := e.Item(ctx, id, "user-a")
	if err != nil || item == nil {
		t.Fatalf("item=%v err=%v", item, err)
	}
	data := item["UserData"].(map[string]any)
	if item["Id"] != id || item["Type"] != "Movie" || data["PlaybackPositionTicks"] != int64(40000*10000) || data["IsFavorite"] != true {
		t.Fatalf("wrong item state: %v", item)
	}
	people := item["People"].([]model.EmbyPerson)
	if len(people) != 1 || people[0].Name != "测试演员" || people[0].Role != "" {
		t.Fatalf("source people or fabricated role: %v", people)
	}
	personID := people[0].Id
	person, err := e.Item(ctx, personID, "user-a")
	if err != nil || person == nil || person["ProviderIds"].(map[string]string)["HongGuoDB"] != "92001" {
		t.Fatalf("source person detail: %v %v", person, err)
	}
	personList, err := e.Items(ctx, ItemsParams{UserID: "user-a", IncludeItemTypes: []string{"Person"}, SearchTerm: "测试演员", Limit: 1})
	if err != nil || personList["TotalRecordCount"] != int64(1) {
		t.Fatalf("source person search: %v %v", personList, err)
	}
	credits, err := e.Items(ctx, ItemsParams{UserID: "user-a", ParentID: library.ID, PersonIDs: []string{personID}, Limit: 50})
	if err != nil || credits["TotalRecordCount"] != int64(1) {
		t.Fatalf("source person credits: %v %v", credits, err)
	}
	if err := e.MarkPlayed(ctx, "user-a", id, true); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkPlayed(ctx, "user-a", id, false); err != nil {
		t.Fatal(err)
	}
	state, err := e.repo.HongGuo.UserState(ctx, "user-a", work.SourceID, 1)
	if err != nil || state.Completed || state.PositionMs != 0 {
		t.Fatal("unwatched did not clear progress")
	}
	favorite, err := e.repo.HongGuo.UserState(ctx, "user-a", work.SourceID, 0)
	if err != nil || !favorite.Favorite {
		t.Fatal("unwatched removed favorite")
	}
	var count int64
	if err := e.repo.DB.Model(&model.HongGuoPlaybackEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("unwatched removed event")
	}
	series, err := e.repo.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "900000000000000002", Title: "测试剧", EpisodeCount: 2, Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	ep := model.Media{LibraryID: library.ID, Path: "/test/hg/episode.strm", CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: series.SourceID, SeasonNum: 1, EpisodeNum: 2}
	if err := e.repo.Media.Upsert(ctx, &ep); err != nil {
		t.Fatal(err)
	}
	view, err := e.repo.MediaView.FindByID(ctx, ep.ID)
	if err != nil || view == nil {
		t.Fatal(err)
	}
	group, err := e.repo.HongGuo.SaveGroup(ctx, "", "人工整剧", []repository.HongGuoGroupInput{{SourceID: series.SourceID, SeasonNumber: 3}})
	if err != nil {
		t.Fatal(err)
	}
	listing, err := e.Items(ctx, ItemsParams{ParentID: library.ID, UserID: "user-a", Limit: 50})
	if err != nil || listing["TotalRecordCount"] != int64(2) {
		t.Fatalf("library hierarchy: %v %v", listing, err)
	}
	seasons, err := e.Items(ctx, ItemsParams{ParentID: "hg-group-" + group.ID, UserID: "user-a", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	seasonItems := seasons["Items"].([]map[string]any)
	if len(seasonItems) != 1 || seasonItems[0]["IndexNumber"] != 3 {
		t.Fatalf("seasons: %v", seasons)
	}
	episodes, err := e.Items(ctx, ItemsParams{ParentID: "hg-group-" + group.ID, UserID: "user-a", IncludeItemTypes: []string{"Episode"}, Recursive: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	episodeItems := episodes["Items"].([]map[string]any)
	if len(episodeItems) != 1 || episodeItems[0]["Id"] != view.CatalogItemID || episodeItems[0]["ParentIndexNumber"] != 3 {
		t.Fatalf("episodes: %v", episodes)
	}
	search, err := e.Items(ctx, ItemsParams{UserID: "user-a", SearchTerm: "人工整剧", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	searchItems := search["Items"].([]map[string]any)
	if len(searchItems) != 1 || searchItems[0]["Id"] != "hg-group-"+group.ID {
		t.Fatalf("global source search: %v", search)
	}
	counts, err := e.ItemCounts(ctx, "user-a")
	if err != nil || counts["MovieCount"] != int64(1) || counts["EpisodeCount"] != int64(1) || counts["SeriesCount"] != 1 {
		t.Fatalf("counts: %v %v", counts, err)
	}
	if err := e.SetFavorite(ctx, "user-a", "hg-group-"+group.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.MarkPlayed(ctx, "user-a", "hg-group-"+group.ID, true); err != nil {
		t.Fatal(err)
	}
	groupItem, err := e.Item(ctx, "hg-group-"+group.ID, "user-a")
	if err != nil || groupItem == nil {
		t.Fatalf("group item: %v %v", groupItem, err)
	}
	if data := groupItem["UserData"].(map[string]any); data["Played"] != true || data["IsFavorite"] != true {
		t.Fatalf("container state: %v", data)
	}
	if err := e.MarkPlayed(ctx, "user-a", "hg-group-"+group.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := e.SetFavorite(ctx, "user-a", view.CatalogItemID, true); !errors.Is(err, repository.ErrFavoriteUnsupportedType) {
		t.Fatalf("episode favorite accepted: %v", err)
	}
	if err := e.RecordProgress(ctx, "user-a", id, ep.ID, "mismatched-source", 30000*10000, 120000*10000); err != nil {
		t.Fatal(err)
	}
	other, err := e.repo.HongGuo.UserState(ctx, "user-a", series.SourceID, 2)
	if err != nil || other.PositionMs != 0 {
		t.Fatal("mismatched source changed another episode")
	}
	if err := NewPlaybackService(e.log, e.repo).RecordProgress(ctx, "user-a", m.ID, "locked", 30000, 120000, MediaVisibility{LibraryRestricted: true}); err == nil {
		t.Fatal("locked profile wrote progress")
	}
	legacyLibrary := model.Library{Name: "旧电影库", Path: "/test/legacy", Type: "movie"}
	if err := db.Create(&legacyLibrary).Error; err != nil {
		t.Fatal(err)
	}
	legacy := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "A legacy", Source: "tmdb"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	legacyFile := model.Media{LibraryID: legacyLibrary.ID, Path: "/test/legacy/movie.strm", MetadataID: legacy.ID}
	if err := db.Create(&legacyFile).Error; err != nil {
		t.Fatal(err)
	}
	for page := 0; page < 3; page++ {
		result, err := e.Items(ctx, ItemsParams{UserID: "user-a", IncludeItemTypes: []string{"Movie", "Series"}, Fields: []string{"Overview"}, Recursive: true, SortBy: "SortName", SortOrder: "Ascending", StartIndex: page, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		items := result["Items"].([]map[string]any)
		want := []string{legacy.ID, "hg-group-" + group.ID, id}[page]
		if result["TotalRecordCount"] != int64(3) || len(items) != 1 || items[0]["Id"] != want {
			t.Fatalf("mixed global page %d: %v", page, result)
		}
		for _, field := range []string{"People", "ProviderIds", "MediaSources"} {
			if _, ok := items[0][field]; ok {
				t.Fatalf("unrequested mixed-list field %s on %s", field, want)
			}
		}
	}
	if err := db.Model(&ep).Update("created_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	latest, err := e.LatestItems(ctx, "user-a", "", 1, false)
	if err != nil || len(latest) != 1 || latest[0]["Id"] != view.CatalogItemID {
		t.Fatalf("mixed latest: %v %v", latest, err)
	}
	latest, err = e.LatestItems(ctx, "user-a", library.ID, 1, false)
	if err != nil || len(latest) != 1 || latest[0]["Id"] != "hg-group-"+group.ID {
		t.Fatalf("source library latest grouping: %v %v", latest, err)
	}
	oldSeries := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "旧整剧", Source: "manual"}
	if err := db.Create(&oldSeries).Error; err != nil {
		t.Fatal(err)
	}
	oldSeason := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &oldSeries.ID, SeasonNum: 1, Title: "旧第一季", Source: "manual"}
	if err := db.Create(&oldSeason).Error; err != nil {
		t.Fatal(err)
	}
	oldEpisode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &oldSeason.ID, EpisodeNum: 1, Title: "旧第一集", Source: "manual"}
	if err := db.Create(&oldEpisode).Error; err != nil {
		t.Fatal(err)
	}
	oldFile := model.Media{LibraryID: legacyLibrary.ID, Path: "/test/legacy/episode.strm", MetadataID: oldEpisode.ID, SeasonNum: 1, EpisodeNum: 1}
	if err := db.Create(&oldFile).Error; err != nil {
		t.Fatal(err)
	}
	containers, err := e.hongGuoBrowseLegacyPayloads(ctx, []string{oldSeries.ID, oldSeason.ID, oldEpisode.ID}, ItemsParams{UserID: "user-a", Fields: []string{"Overview"}})
	if err != nil || len(containers) != 3 {
		t.Fatalf("mixed legacy hierarchy: %v %v", containers, err)
	}
	for _, item := range containers {
		if _, ok := item["People"]; ok {
			t.Fatal("legacy hierarchy ignored Fields")
		}
		if item["Type"] == "Season" && (item["ChildCount"] != 1 || item["SeriesName"] != oldSeries.Title) {
			t.Fatalf("season summary: %v", item)
		}
	}
}

func TestHongGuoEmbyResumableGroupsBeforePaging(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "红果", Path: "/test/hg-resume", Type: model.LibraryTypeHongGuo}
	if err := db.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	work, err := e.repo.HongGuo.SaveDetail(t.Context(), hongguo.Work{SourceID: "900000000000000099", Title: "两集短剧", EpisodeCount: 2, Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"/test/hg-resume/e1.strm", "/test/hg-resume/e2.strm"}
	ids := make([]string, len(paths))
	for i, path := range paths {
		media := model.Media{LibraryID: library.ID, Path: path, CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: work.SourceID, SeasonNum: 1, EpisodeNum: i + 1}
		if err := e.repo.Media.Upsert(t.Context(), &media); err != nil {
			t.Fatal(err)
		}
		view, err := e.repo.MediaView.FindByID(t.Context(), media.ID)
		if err != nil || view == nil {
			t.Fatalf("view=%#v err=%v", view, err)
		}
		ids[i] = view.CatalogItemID
		if err := e.repo.HongGuo.RecordProgress(t.Context(), "resume-user", "", *view, 30_000, 120_000, false); err != nil {
			t.Fatal(err)
		}
	}
	older := time.Now().UTC().Add(-time.Minute)
	if err := db.Model(&model.HongGuoUserState{}).Where("user_id = ? AND source_id = ? AND episode_number = 1", "resume-user", work.SourceID).Updates(map[string]any{"watched_at": older, "updated_at": older}).Error; err != nil {
		t.Fatal(err)
	}
	params := ItemsParams{UserID: "resume-user", IncludeItemTypes: []string{"Episode"}, Filters: []string{"IsResumable"}, Recursive: true, SortBy: "DatePlayed", SortOrder: "Descending", Limit: 10}
	for _, parentID := range []string{"", library.ID} {
		params.ParentID = parentID
		result, err := e.Items(t.Context(), params)
		items, _ := result["Items"].([]map[string]any)
		if err != nil || result["TotalRecordCount"] != int64(1) || len(items) != 1 || items[0]["Id"] != ids[1] {
			t.Fatalf("parent=%q result=%#v err=%v", parentID, result, err)
		}
	}
}
