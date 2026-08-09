package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyItemsExposeSeriesSeasonEpisodeHierarchy(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "番剧", Path: `F:\downloads\日番`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Base: model.Base{ID: "metadata-series"}, Kind: model.MetadataKindSeries,
		Title: "间谍过家家", OriginalName: "SPY×FAMILY", Source: "tmdb",
	})
	season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Base: model.Base{ID: "metadata-season-2"}, Kind: model.MetadataKindSeason,
		ParentID: &series.ID, SeasonNum: 2, Title: "第 2 季", Source: "tmdb",
	})
	episodeMetadata := []model.MetadataItem{
		{Base: model.Base{ID: "metadata-episode-1"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, Title: "任务代号: 猫", EpisodeNum: 1, Source: "tmdb"},
		{Base: model.Base{ID: "metadata-episode-2"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, Title: "接近目标", EpisodeNum: 2, Source: "tmdb"},
	}
	if err := svc.repo.DB.Create(&episodeMetadata).Error; err != nil {
		t.Fatalf("create episodes: %v", err)
	}
	for i, media := range []model.Media{
		{
			Base:         model.Base{ID: "ep-1"},
			LibraryID:    lib.ID,
			MetadataID:   episodeMetadata[0].ID,
			Title:        "间谍过家家",
			OriginalName: "SPY×FAMILY",
			EpisodeTitle: "第 1 集",
			Path:         `F:\downloads\日番\剧集\间谍过家家\Season 02\间谍过家家 - S02E01.mkv`,
			PosterURL:    `F:\poster.jpg`,
			SeasonNum:    2,
			EpisodeNum:   1,
		},
		{
			Base:         model.Base{ID: "ep-2"},
			LibraryID:    lib.ID,
			MetadataID:   episodeMetadata[1].ID,
			Title:        "间谍过家家",
			OriginalName: "SPY×FAMILY",
			EpisodeTitle: "第 2 集",
			Path:         `F:\downloads\日番\剧集\间谍过家家\Season 02\间谍过家家 - S02E02.mkv`,
			PosterURL:    `F:\poster.jpg`,
			SeasonNum:    2,
			EpisodeNum:   2,
		},
	} {
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media %d: %v", i, err)
		}
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	rootItems := root["Items"].([]map[string]any)
	if len(rootItems) != 1 {
		t.Fatalf("expected one series card, got %#v", rootItems)
	}
	seriesID := rootItems[0]["Id"].(string)
	if seriesID != series.ID {
		t.Fatalf("series id = %q, want metadata id %q", seriesID, series.ID)
	}
	if rootItems[0]["Type"] != "Series" || rootItems[0]["IsFolder"] != true || rootItems[0]["Name"] != "间谍过家家" {
		t.Fatalf("unexpected series payload: %#v", rootItems[0])
	}

	seasons, err := svc.Items(t.Context(), ItemsParams{ParentID: seriesID, Limit: 50})
	if err != nil {
		t.Fatalf("series items: %v", err)
	}
	seasonItems := seasons["Items"].([]map[string]any)
	if len(seasonItems) != 1 || seasonItems[0]["Type"] != "Season" || seasonItems[0]["IndexNumber"] != 2 {
		t.Fatalf("unexpected seasons: %#v", seasonItems)
	}
	if seasonItems[0]["Id"] != season.ID {
		t.Fatalf("season id = %#v, want metadata id %q", seasonItems[0]["Id"], season.ID)
	}

	episodes, err := svc.Items(t.Context(), ItemsParams{ParentID: seasonItems[0]["Id"].(string), IncludeItemTypes: []string{"Episode"}, Recursive: true, Limit: 50})
	if err != nil {
		t.Fatalf("season episodes: %v", err)
	}
	episodeItems := episodes["Items"].([]map[string]any)
	if len(episodeItems) != 2 || episodeItems[0]["Type"] != "Episode" || episodeItems[0]["Name"] != "任务代号: 猫" {
		t.Fatalf("unexpected episodes: %#v", episodeItems)
	}
	if episodeItems[0]["SeriesId"] != seriesID || episodeItems[0]["ParentId"] != seasonItems[0]["Id"] {
		t.Fatalf("episode hierarchy not linked: %#v", episodeItems[0])
	}
	if episodeItems[0]["SeriesName"] != series.Title {
		t.Fatalf("episode series name = %#v, want parent series title %q", episodeItems[0]["SeriesName"], series.Title)
	}
	if episodeItems[0]["Id"] != episodeMetadata[0].ID || episodeItems[0]["SeasonId"] != season.ID {
		t.Fatalf("episode identity not metadata-backed: %#v", episodeItems[0])
	}

	latest, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 10)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(latest) != 1 || latest[0]["Type"] != "Series" {
		t.Fatalf("latest should be grouped by series: %#v", latest)
	}

	playback, err := svc.PlaybackInfo(t.Context(), seriesID, "user-1")
	if err != nil {
		t.Fatalf("series playback fallback: %v", err)
	}
	sources := playback["MediaSources"].([]map[string]any)
	if sources[0]["Id"] != "ep-1" {
		t.Fatalf("series playback should fall back to first episode: %#v", sources)
	}
	if sources[0]["DirectStreamUrl"] != "/Videos/ep-1/stream.mkv" {
		t.Fatalf("playback should use Emby-compatible stream URL: %#v", sources[0])
	}
}

func TestEmbySeriesGroupingPaginatesAfterFullLibraryGrouping(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "国漫", Path: `/media/anime`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	rows := make([]model.Media, 0, 25*40)
	for series := 1; series <= 25; series++ {
		for episode := 1; episode <= 40; episode++ {
			created := now.Add(time.Duration(series*1000+episode) * time.Second)
			rows = append(rows, model.Media{
				Base:       model.Base{ID: fmt.Sprintf("show-%02d-ep-%02d", series, episode), CreatedAt: created, UpdatedAt: created},
				LibraryID:  lib.ID,
				Title:      fmt.Sprintf("测试番 %02d", series),
				Path:       fmt.Sprintf(`/media/anime/测试番 %02d/Season 01/测试番 %02d.S01E%02d.mkv`, series, series, episode),
				SeasonNum:  1,
				EpisodeNum: episode,
			})
		}
	}
	if err := svc.repo.DB.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 20})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	if root["TotalRecordCount"] != 25 {
		t.Fatalf("series total = %#v, want 25", root["TotalRecordCount"])
	}
	rootItems := root["Items"].([]map[string]any)
	if len(rootItems) != 20 {
		t.Fatalf("first page series len = %d, want 20", len(rootItems))
	}
	if rootItems[0]["RecursiveItemCount"] != 40 {
		t.Fatalf("first series episode count = %#v, want 40", rootItems[0]["RecursiveItemCount"])
	}

	latest, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 25)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(latest) != 25 {
		t.Fatalf("latest series len = %d, want 25", len(latest))
	}

	counts, err := svc.ItemCounts(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("item counts: %v", err)
	}
	if counts["SeriesCount"] != 25 || counts["EpisodeCount"] != int64(1000) {
		t.Fatalf("counts = %#v, want 25 series and 1000 episodes", counts)
	}
}

func TestEmbySeriesHierarchyCountsEpisodeMetadataOnceAcrossVersions(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "动画", Path: `/media/anime-versions`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	series := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Base: model.Base{ID: "series-version-count"}, Kind: model.MetadataKindSeries,
		Title: "版本计数", Source: "tmdb",
	})
	season := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Base: model.Base{ID: "season-version-count"}, Kind: model.MetadataKindSeason,
		ParentID: &series.ID, SeasonNum: 1, Title: series.Title, Source: "tmdb",
	})
	episode := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Base: model.Base{ID: "episode-version-count"}, Kind: model.MetadataKindEpisode,
		ParentID: &season.ID, EpisodeNum: 1, Title: "第一集", Source: "tmdb",
	})
	versions := []model.Media{
		{Base: model.Base{ID: "episode-version-1080"}, LibraryID: lib.ID, MetadataID: episode.ID, Title: series.Title, Path: `/media/anime-versions/show/Season 01/show.S01E01.1080p.mkv`, SeasonNum: 1, EpisodeNum: 1},
		{Base: model.Base{ID: "episode-version-2160"}, LibraryID: lib.ID, MetadataID: episode.ID, Title: series.Title, Path: `/media/anime-versions/show/Season 01/show.S01E01.2160p.mkv`, SeasonNum: 1, EpisodeNum: 1},
	}
	if err := svc.repo.DB.Create(&versions).Error; err != nil {
		t.Fatalf("create episode versions: %v", err)
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 10})
	if err != nil {
		t.Fatalf("series items: %v", err)
	}
	seriesItems := root["Items"].([]map[string]any)
	if len(seriesItems) != 1 || seriesItems[0]["RecursiveItemCount"] != 1 {
		t.Fatalf("series version count = %#v, want one logical episode", root)
	}
	seasons, err := svc.Items(t.Context(), ItemsParams{ParentID: series.ID, Limit: 10})
	if err != nil {
		t.Fatalf("season items: %v", err)
	}
	seasonItems := seasons["Items"].([]map[string]any)
	if len(seasonItems) != 1 || seasonItems[0]["ChildCount"] != 1 {
		t.Fatalf("season version count = %#v, want one logical episode", seasons)
	}
}

func TestEmbyItemsKeepSpecialsInSeasonZero(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "番剧", Path: `F:\downloads\日番`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	episode := createServiceTestEpisodeMetadata(t, svc.repo.DB,
		model.MetadataItem{Base: model.Base{ID: "metadata-specials-series"}, Kind: model.MetadataKindSeries, Title: "间谍过家家", Source: "tmdb"},
		model.MetadataItem{Base: model.Base{ID: "metadata-specials-episode"}, Kind: model.MetadataKindEpisode,
			Title: "间谍过家家", SeasonNum: 0, EpisodeNum: 1, Source: "tmdb",
		})
	createServiceTestArtwork(t, svc.repo.DB, episode.ID, model.ArtworkTypeStill, "asset-special-still")
	media := model.Media{
		Base:       model.Base{ID: "sp-1"},
		LibraryID:  lib.ID,
		MetadataID: episode.ID,
		Title:      "间谍过家家",
		Path:       `F:\downloads\日番\间谍过家家\Specials\间谍过家家 - S00E01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	rootItems := root["Items"].([]map[string]any)
	if len(rootItems) != 1 || rootItems[0]["Type"] != "Series" {
		t.Fatalf("expected one series card, got %#v", rootItems)
	}

	seasons, err := svc.Items(t.Context(), ItemsParams{ParentID: rootItems[0]["Id"].(string), Limit: 50})
	if err != nil {
		t.Fatalf("series seasons: %v", err)
	}
	seasonItems := seasons["Items"].([]map[string]any)
	if len(seasonItems) != 1 || seasonItems[0]["Type"] != "Season" || seasonItems[0]["IndexNumber"] != 0 || seasonItems[0]["Name"] != "特别篇" {
		t.Fatalf("specials should be exposed as season zero: %#v", seasonItems)
	}
	if episode.ParentID == nil || seasonItems[0]["Id"] != *episode.ParentID {
		t.Fatalf("special season id = %#v, want metadata id %#v", seasonItems[0]["Id"], episode.ParentID)
	}

	episodes, err := svc.Items(t.Context(), ItemsParams{ParentID: seasonItems[0]["Id"].(string), IncludeItemTypes: []string{"Episode"}, Recursive: true, Limit: 50})
	if err != nil {
		t.Fatalf("special episodes: %v", err)
	}
	episodeItems := episodes["Items"].([]map[string]any)
	if len(episodeItems) != 1 {
		t.Fatalf("expected one special episode, got %#v", episodeItems)
	}
	if episodeItems[0]["ParentIndexNumber"] != 0 || episodeItems[0]["SeasonId"] != seasonItems[0]["Id"] || episodeItems[0]["ParentId"] != seasonItems[0]["Id"] {
		t.Fatalf("special episode linked to wrong season: %#v season=%#v", episodeItems[0], seasonItems[0])
	}
	if tags, ok := episodeItems[0]["ImageTags"].(map[string]string); !ok || tags["Primary"] != "metadata-specials-episode" {
		t.Fatalf("episode still should be exposed as Primary image: %#v", episodeItems[0]["ImageTags"])
	}
}

func TestEmbyEpisodeStillIsPrimaryImageNotArt(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "剧集", Path: `/media/tv`, Type: "tv", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	episode := createServiceTestEpisodeMetadata(t, svc.repo.DB,
		model.MetadataItem{Base: model.Base{ID: "metadata-still-series"}, Kind: model.MetadataKindSeries, Title: "间谍过家家", Source: "tmdb"},
		model.MetadataItem{Base: model.Base{ID: "metadata-still-episode"}, Kind: model.MetadataKindEpisode,
			Title: "间谍过家家", SeasonNum: 2, EpisodeNum: 1, Source: "tmdb",
		})
	wantStill := createServiceTestArtwork(t, svc.repo.DB, episode.ID, model.ArtworkTypeStill, "asset-episode-still")
	media := model.Media{
		Base:       model.Base{ID: "ep-still"},
		LibraryID:  lib.ID,
		MetadataID: episode.ID,
		Title:      "间谍过家家",
		Path:       `/media/tv/间谍过家家/Season 02/间谍过家家 - S02E01.mkv`,
		SeasonNum:  2,
		EpisodeNum: 1,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	view, err := svc.repo.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view == nil {
		t.Fatalf("find media view: %#v %v", view, err)
	}
	item := svc.itemPayload(t.Context(), view, "", false, 0, false)
	if tags, ok := item["ImageTags"].(map[string]string); !ok || tags["Primary"] != episode.ID {
		t.Fatalf("episode should expose a primary image tag: %#v", item["ImageTags"])
	}
	if tags, ok := item["BackdropImageTags"].([]string); !ok || len(tags) != 0 {
		t.Fatalf("episode still must not be exposed as art/backdrop: %#v", item["BackdropImageTags"])
	}
	primary, err := svc.ImageURL(t.Context(), "ep-still", "Primary")
	if err != nil {
		t.Fatalf("primary image url: %v", err)
	}
	if primary != wantStill {
		t.Fatalf("episode Primary image = %q, want still %q", primary, wantStill)
	}
	art, err := svc.ImageURL(t.Context(), "ep-still", "Art")
	if err != nil {
		t.Fatalf("art image url: %v", err)
	}
	if art == wantStill {
		t.Fatalf("episode still must not be returned as Art image")
	}
}

func TestEmbySeriesArtworkUsesSharedMetadata(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "番剧", Path: `/media/anime`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	seriesID := "metadata-cache-series"
	episode := createServiceTestEpisodeMetadata(t, svc.repo.DB,
		model.MetadataItem{Base: model.Base{ID: seriesID}, Kind: model.MetadataKindSeries, Title: "剑来", Source: "tmdb"},
		model.MetadataItem{Base: model.Base{ID: "metadata-cache-episode"}, Kind: model.MetadataKindEpisode,
			Title: "剑来", SeasonNum: 1, EpisodeNum: 1, Source: "tmdb",
		})
	wantPoster := createServiceTestArtwork(t, svc.repo.DB, seriesID, model.ArtworkTypePoster, "asset-cache-poster")
	wantBackdrop := createServiceTestArtwork(t, svc.repo.DB, seriesID, model.ArtworkTypeBackdrop, "asset-cache-backdrop")
	media := model.Media{
		Base:       model.Base{ID: "ep-1"},
		LibraryID:  lib.ID,
		MetadataID: episode.ID,
		Title:      "剑来",
		Path:       `/media/anime/剑来/Season 01/剑来 - S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	itemSeriesID := items[0]["Id"].(string)

	poster, err := svc.ImageURL(t.Context(), itemSeriesID, "Primary")
	if err != nil {
		t.Fatalf("image url from cache: %v", err)
	}
	if poster != wantPoster {
		t.Fatalf("poster = %q, want cached poster", poster)
	}
	backdrop, err := svc.ImageURL(t.Context(), itemSeriesID, "Backdrop")
	if err != nil {
		t.Fatalf("backdrop url from cache: %v", err)
	}
	if backdrop != wantBackdrop {
		t.Fatalf("backdrop = %q, want cached backdrop", backdrop)
	}
}

func TestEmbySeasonAndEpisodeDoNotInheritSeriesArtworkOrPeople(t *testing.T) {
	svc := newTestEmbyService(t)
	seriesID := "metadata-no-inherit-series"
	episode := createServiceTestEpisodeMetadata(t, svc.repo.DB,
		model.MetadataItem{Base: model.Base{ID: seriesID}, Kind: model.MetadataKindSeries, Title: "独立元数据", Source: "tmdb"},
		model.MetadataItem{Base: model.Base{ID: "metadata-no-inherit-episode"}, Kind: model.MetadataKindEpisode, Title: "独立元数据", SeasonNum: 1, EpisodeNum: 1, Source: "tmdb"},
	)
	if episode.ParentID == nil {
		t.Fatal("episode season parent is missing")
	}
	createServiceTestArtwork(t, svc.repo.DB, seriesID, model.ArtworkTypePoster, "asset-no-inherit-poster")
	person := model.Person{Name: "Series Actor", OriginalName: "Series Actor", NormalizedName: "series actor", Source: "tmdb"}
	if err := svc.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DB.Create(&model.MetadataCredit{MetadataID: seriesID, PersonID: person.ID, Type: model.CreditTypeActor}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{*episode.ParentID, episode.ID} {
		image, err := svc.ImageURL(t.Context(), id, "Primary")
		if err != nil {
			t.Fatal(err)
		}
		if image != "" {
			t.Fatalf("metadata %s inherited series image %q", id, image)
		}
		if people := svc.peopleForMetadata(t.Context(), id); len(people) != 0 {
			t.Fatalf("metadata %s inherited series people: %#v", id, people)
		}
	}
}

func TestEmbyCloudAnimeUsesCanonicalSeriesMetadata(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "OpenList · 国漫", Path: `cloud://openlist/国漫`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	for _, media := range []model.Media{
		{
			Base:         model.Base{ID: "cloud-ep-1"},
			LibraryID:    lib.ID,
			SeriesID:     "local-cloud-jianlai",
			Title:        "剑来",
			EpisodeTitle: "04",
			Path:         `cloud://openlist/国漫/剑来/第二季/04.mkv`,
			SeasonNum:    2,
			EpisodeNum:   4,
		},
		{
			Base:         model.Base{ID: "cloud-ep-2"},
			LibraryID:    lib.ID,
			SeriesID:     "local-cloud-jianlai",
			Title:        "剑来",
			EpisodeTitle: "05",
			Path:         `cloud://openlist/国漫/剑来/第二季/05.mkv`,
			SeasonNum:    2,
			EpisodeNum:   5,
		},
	} {
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Type"] != "Series" || items[0]["Name"] != "剑来" {
		t.Fatalf("cloud anime should use canonical series metadata, got %#v", items)
	}
}
