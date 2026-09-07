package service

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestLibrarySeriesCardsUseSeriesPresentationAndKeepEpisodeTarget(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "剧库", Path: "/fixture/series-cards", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "以吾之名", Overview: "整剧简介", Year: 2021, Rating: 8.3})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "第一季"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "复仇", Overview: "单集简介", Rating: 9.1})
	poster := createServiceTestArtwork(t, db, series.ID, model.ArtworkTypePoster, "series-card-poster")
	media := model.Media{LibraryID: lib.ID, MetadataID: episode.ID, Path: lib.Path + "/以吾之名/Season 1/S01E01.mkv", SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "matched"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	cards, total, err := svc.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 50, "", "", MediaVisibility{})
	if err != nil || total != 1 || len(cards) != 1 {
		t.Fatalf("cards=%+v total=%d err=%v", cards, total, err)
	}
	card := cards[0]
	if card.Rep.Title != series.Title || card.Rep.PosterURL != poster || card.Rep.Year != 2021 || card.Rep.Rating != series.Rating || card.Rep.Overview != series.Overview {
		t.Fatalf("wrong card presentation: %+v", card.Rep)
	}
	if card.Rep.ID != media.ID || card.Rep.MetadataID != episode.ID || card.LinkMedia.ID != media.ID || card.LinkMedia.MetadataID != episode.ID || card.LinkMedia.Title != episode.Title {
		t.Fatalf("card changed episode target: %+v", card)
	}
	if card.Key != "metadata:"+series.ID {
		t.Fatal("presentation changed navigation key")
	}
	missing, n, err := svc.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 50, "", "", MediaVisibility{MissingPoster: true})
	if err != nil || n != 0 || len(missing) != 0 {
		t.Fatalf("series with poster reported missing: %+v %v", missing, err)
	}
	view, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view.Title != episode.Title || view.PosterURL != "" || view.Year != 0 {
		t.Fatalf("episode projection changed: %+v %v", view, err)
	}
	for _, visibility := range []MediaVisibility{{HiddenLibraryIDs: []string{lib.ID}}, {AllowedLibraryIDs: []string{"other-library"}}} {
		cards, n, err := svc.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 50, "", "", visibility)
		if err != nil || n != 0 || len(cards) != 0 {
			t.Fatalf("hidden library leaked: %+v %v", cards, err)
		}
	}
	if err := db.Where("metadata_id = ?", series.ID).Delete(&model.MetadataArtwork{}).Error; err != nil {
		t.Fatal(err)
	}
	createServiceTestArtwork(t, db, episode.ID, model.ArtworkTypePoster, "episode-card-poster")
	cards, n, err = svc.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 50, "", "", MediaVisibility{MissingPoster: true})
	if err != nil || n != 1 || cards[0].Rep.PosterURL != "" {
		t.Fatalf("episode artwork substituted for series: %+v %v", cards, err)
	}
}

func TestMediaSeasonDetailUsesCanonicalArtworkAndVisibility(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "季封面", Path: "/fixture/season", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "整剧"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "123"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 2, Title: "第二季", Overview: "季介绍", Rating: 8.5}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "season", ExternalID: "456"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "第一集"})
	poster := createServiceTestArtwork(t, db, season.ID, model.ArtworkTypePoster, "season-poster")
	createServiceTestArtwork(t, db, series.ID, model.ArtworkTypePoster, "series-poster")
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	var episodeMediaID string
	for _, target := range []*model.MetadataItem{episode, season, series} {
		media := model.Media{LibraryID: lib.ID, MetadataID: target.ID, Path: lib.Path + "/" + target.Kind + ".mkv", SeasonNum: 99}
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		if target == episode {
			episodeMediaID = media.ID
		}
		got, err := svc.GetMediaSeasonVisible(t.Context(), media.ID, MediaVisibility{})
		if err != nil {
			t.Fatal(err)
		}
		if target == series {
			if got != nil {
				t.Fatalf("invented season: %+v", got)
			}
			continue
		}
		if got == nil || got.ID != season.ID || got.SeasonNum != 2 || got.Title != season.Title || got.PosterURL != poster || got.Path != "" {
			t.Fatalf("wrong season presentation: %+v", got)
		}
		if got.SeriesTMDbID != 123 || got.TMDbID != 456 || got.TMDbStatus != providerStatusMissing || got.TMDbSnapshot || got.Overview != season.Overview || got.Rating != season.Rating {
			t.Fatalf("wrong season provider details: %+v", got)
		}
		for _, visibility := range []MediaVisibility{{LibraryRestricted: true}, {HiddenLibraryIDs: []string{lib.ID}}, {AllowedLibraryIDs: []string{"other"}}} {
			got, err = svc.GetMediaSeasonVisible(t.Context(), media.ID, visibility)
			if err != nil || got != nil {
				t.Fatalf("visibility leak: %+v %v", got, err)
			}
		}
		if err := db.Model(season).Update("nsfw", true).Error; err != nil {
			t.Fatal(err)
		}
		got, err = svc.GetMediaSeasonVisible(t.Context(), media.ID, MediaVisibility{})
		if err != nil || got != nil {
			t.Fatalf("NSFW season leak: %+v %v", got, err)
		}
		got, err = svc.GetMediaSeasonVisible(t.Context(), media.ID, MediaVisibility{IncludeNSFW: true})
		if err != nil || got == nil {
			t.Fatalf("NSFW opt-in failed: %+v %v", got, err)
		}
		if err := db.Model(season).Update("nsfw", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.GetMediaSeasonVisible(t.Context(), season.ID, MediaVisibility{})
	if err != nil || got != nil {
		t.Fatalf("metadata ID accepted as file: %+v %v", got, err)
	}
	if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: season.ID, Provider: "tmdb", Payload: `{"id":456}`, FetchedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{providerStatusPartial, providerStatusComplete} {
		if status == providerStatusComplete {
			if err := db.Model(&model.MetadataArtwork{}).Where("metadata_id = ?", season.ID).Update("source_provider", "tmdb").Error; err != nil {
				t.Fatal(err)
			}
		}
		got, err := svc.GetMediaSeasonVisible(t.Context(), episodeMediaID, MediaVisibility{})
		if err != nil || got == nil || !got.TMDbSnapshot || got.TMDbStatus != status {
			t.Fatalf("season status %s: %+v %v", status, got, err)
		}
	}
}

func TestMediaSeriesDetailSupportsEveryAttachmentLevel(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.MetadataProviderSnapshot{})
	repos := repository.New(db)
	lib := model.Library{Name: "剧集关联测试", Path: "/fixture/series-attachment", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "整剧", Overview: "整剧简介"})
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	check := func(target *model.MetadataItem) {
		t.Helper()
		media := model.Media{LibraryID: lib.ID, MetadataID: target.ID, Path: lib.Path + "/" + target.Kind + ".mkv"}
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetMediaSeriesVisible(t.Context(), media.ID, MediaVisibility{})
		if err != nil || got == nil || got.ID != series.ID || got.Title != series.Title || got.Overview != series.Overview || got.Path != "" {
			t.Fatalf("%s attachment: %#v, %v", target.Kind, got, err)
		}
		for _, visibility := range []MediaVisibility{{LibraryRestricted: true}, {HiddenLibraryIDs: []string{lib.ID}}, {AllowedLibraryIDs: []string{"other"}}} {
			got, err := svc.GetMediaSeriesVisible(t.Context(), media.ID, visibility)
			if err != nil || got != nil {
				t.Fatalf("visibility leak: %#v, %v", got, err)
			}
		}
		if err := db.Model(series).Update("nsfw", true).Error; err != nil {
			t.Fatal(err)
		}
		got, err = svc.GetMediaSeriesVisible(t.Context(), media.ID, MediaVisibility{})
		if err != nil || got != nil {
			t.Fatalf("NSFW Series leaked: %#v, %v", got, err)
		}
		got, err = svc.GetMediaSeriesVisible(t.Context(), media.ID, MediaVisibility{IncludeNSFW: true})
		if err != nil || got == nil {
			t.Fatalf("NSFW opt-in failed: %#v, %v", got, err)
		}
		if err := db.Model(series).Update("nsfw", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	check(series) // 尚无季、集时，也必须能读取整剧。
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "第一季"})
	check(season)
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "第一集"})
	check(episode)
	got, err := svc.GetMediaSeriesVisible(t.Context(), series.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil || got != nil {
		t.Fatalf("metadata ID accepted as file: %#v, %v", got, err)
	}
}

func TestMediaSeriesDetailOwnsMetadataAndUserScope(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.MetadataProviderSnapshot{}, &model.PlaybackHistory{}, &model.Favorite{})
	repos := repository.New(db)
	lib := model.Library{Name: "剧集", Path: "/media/series-detail-check", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "整剧标题", Overview: "整剧简介", Source: "test"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, Title: "特别篇", ParentID: &series.ID, SeasonNum: 0, Source: "test"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, Title: "单集标题", Overview: "单集简介", ParentID: &season.ID, EpisodeNum: 1, Source: "test"})
	media := model.Media{LibraryID: lib.ID, MetadataID: episode.ID, Path: lib.Path + "/S00E01.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	view, err := svc.GetMediaSeriesVisible(t.Context(), media.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.ID != series.ID || view.MetadataID != series.ID || view.Title != series.Title || view.Overview != series.Overview || view.MetadataKind != model.MetadataKindSeries || view.Path != "" || view.DurationSec != 0 || view.EpisodeNum != 0 || view.SeasonID != "" {
		t.Fatalf("wrong Series projection: %#v", view)
	}
	newTitle, doubanID := "整剧新标题", "series-douban"
	updated, err := svc.UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{Scope: "series", Title: &newTitle, DoubanID: &doubanID})
	if err != nil || updated == nil || updated.Title != newTitle || updated.DoubanID != doubanID {
		t.Fatalf("Series edit: %#v, %v", updated, err)
	}
	seasonTitle, seasonOverview := "特别篇新标题", "特别篇新简介"
	updated, err = svc.UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{Scope: "season", Title: &seasonTitle, Overview: &seasonOverview})
	if err != nil || updated == nil || updated.MetadataID != season.ID || updated.Title != seasonTitle || updated.Overview != seasonOverview || updated.SeasonNum != 0 {
		t.Fatalf("Season edit: %#v, %v", updated, err)
	}
	changedSeason := 2
	if _, err := svc.UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{Scope: "season", SeasonNum: &changedSeason}); err == nil {
		t.Fatal("season edit accepted hierarchy change")
	}
	seriesAfter, err := repos.Metadata.FindByID(t.Context(), series.ID)
	if err != nil || seriesAfter.Title != newTitle {
		t.Fatalf("Season edit changed Series: %#v, %v", seriesAfter, err)
	}
	preserved, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || preserved == nil || preserved.MetadataID != episode.ID || preserved.Title != episode.Title || preserved.Overview != episode.Overview {
		t.Fatalf("Scoped edit changed Episode: %#v, %v", preserved, err)
	}
	for _, visibility := range []MediaVisibility{{HiddenLibraryIDs: []string{lib.ID}}, {LibraryRestricted: true}, {AllowedLibraryIDs: []string{"other-library"}}} {
		got, err := svc.GetMediaSeriesVisible(t.Context(), media.ID, visibility)
		if err != nil || got != nil {
			t.Fatalf("invisible Series returned: %#v, %v", got, err)
		}
	}
	if _, err := repos.Favorite.SetByIdentity(t.Context(), "viewer", series.ID, media.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []struct {
		user, metadata string
		want           bool
	}{{"viewer", series.ID, true}, {"viewer", episode.ID, false}, {"other", series.ID, false}} {
		got, err := repos.Favorite.IsFavoriteByIdentity(t.Context(), identity.user, identity.metadata, "")
		if err != nil || got != identity.want {
			t.Fatalf("favorite scope: %v, %v", got, err)
		}
	}
	for _, user := range []string{"viewer", "other"} {
		if err := db.Create(&model.PlaybackHistory{UserID: user, MetadataID: episode.ID, MediaID: media.ID, PositionMs: 100_000, DurationMs: 900_000, WatchedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	history, err := repos.History.ListByUserMetadataIDs(t.Context(), "viewer", []string{episode.ID})
	if err != nil || len(history) != 1 || history[0].UserID != "viewer" {
		t.Fatalf("history scope: %#v, %v", history, err)
	}
	history, err = repos.History.ListByUserMetadataIDs(t.Context(), "viewer", nil)
	if err != nil || len(history) != 0 {
		t.Fatalf("empty history: %#v, %v", history, err)
	}
}

func TestListRecentSeriesCardsCountsAllEpisodesInSeries(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	lib := model.Library{Name: "国漫", Path: "/media/anime", Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "史上最强炼体老祖", Source: "test"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: series.Title, Source: "test"})
	rows := make([]model.Media, 0, 40)
	for i := 1; i <= 40; i++ {
		created := now.Add(-48 * time.Hour)
		if i > 23 {
			created = now.Add(time.Duration(i) * time.Minute)
		}
		episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: i, Title: fmt.Sprintf("第 %d 集", i), Source: "test"})
		rows = append(rows, model.Media{
			PermanentBase: model.PermanentBase{ID: fmt.Sprintf("recent-ep-%02d", i), CreatedAt: created, UpdatedAt: created},
			LibraryID:     lib.ID,
			MetadataID:    episode.ID,
			Title:         "史上最强炼体老祖",
			Path:          fmt.Sprintf("/media/anime/国漫/史上最强炼体老祖/Season 01/史上最强炼体老祖.S01E%02d.mkv", i),
			SeasonNum:     1,
			EpisodeNum:    i,
		})
		if i == 1 {
			rows = append(rows, model.Media{
				PermanentBase: model.PermanentBase{ID: "recent-ep-01-alt", CreatedAt: created.Add(time.Second), UpdatedAt: created.Add(time.Second)},
				LibraryID:     lib.ID,
				MetadataID:    episode.ID,
				Title:         series.Title,
				Path:          "/media/anime/国漫/史上最强炼体老祖/Season 01/史上最强炼体老祖.S01E01.2160p.mkv",
				SeasonNum:     1,
				EpisodeNum:    1,
			})
		}
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	cards, err := svc.ListRecentSeriesCards(t.Context(), 24, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("recent cards = %#v, want one series card", cards)
	}
	if cards[0].Count != 40 {
		t.Fatalf("recent series count = %d, want full 40 episodes", cards[0].Count)
	}
	if cards[0].Rep.Title != series.Title {
		t.Fatalf("recent card title = %q", cards[0].Rep.Title)
	}
}

func TestMediaSeriesKeyCollapsesNestedSpecialFolders(t *testing.T) {
	main := model.Media{
		LibraryID:  "lib-tv",
		Path:       `/media/anime/示例剧/Season 01/示例剧.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	special := model.Media{
		LibraryID: "lib-tv",
		Path:      `/media/anime/示例剧/Extras/Season 01/示例剧.SP01.mkv`,
	}

	if got, want := mediaSeriesKey(special), mediaSeriesKey(main); got != want {
		t.Fatalf("special key=%q, want main key=%q", got, want)
	}

	cards := groupMediaSeriesCards([]model.Media{main, special})
	if len(cards) != 1 || cards[0].Count != 2 {
		t.Fatalf("cards=%#v, want one merged series card with two items", cards)
	}
}

func TestGroupMediaSeriesCardsSortsByLatestEpisodeTime(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	newerFirstEpisode := model.Media{
		PermanentBase: model.PermanentBase{CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour)},
		LibraryID:     "lib-tv",
		Title:         "更新合集",
		Path:          `F:\media\电视剧\国产剧\更新合集\Season 01\更新合集.S01E01.mkv`,
		SeasonNum:     1,
		EpisodeNum:    1,
	}
	newerLatestEpisode := model.Media{
		PermanentBase: model.PermanentBase{CreatedAt: now, UpdatedAt: now},
		LibraryID:     "lib-tv",
		Title:         "更新合集",
		Path:          `F:\media\电视剧\国产剧\更新合集\Season 01\更新合集.S01E02.mkv`,
		SeasonNum:     1,
		EpisodeNum:    2,
	}
	olderSeries := model.Media{
		PermanentBase: model.PermanentBase{CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour)},
		LibraryID:     "lib-tv",
		Title:         "较早合集",
		Path:          `F:\media\电视剧\国产剧\较早合集\Season 01\较早合集.S01E01.mkv`,
		SeasonNum:     1,
		EpisodeNum:    1,
	}

	cards := groupMediaSeriesCards([]model.Media{olderSeries, newerFirstEpisode, newerLatestEpisode})
	if len(cards) != 2 {
		t.Fatalf("cards=%#v, want two series cards", cards)
	}
	if cards[0].Key != mediaSeriesKey(newerFirstEpisode) {
		t.Fatalf("first card key=%q, want latest series key=%q", cards[0].Key, mediaSeriesKey(newerFirstEpisode))
	}
	if cards[0].Count != 2 {
		t.Fatalf("latest series count=%d, want 2", cards[0].Count)
	}
}

func TestMediaSeriesKeyCollapsesSpecialTitleSuffix(t *testing.T) {
	main := model.Media{
		LibraryID:  "lib-tv",
		Path:       `/media/tv/Example Show/Season 01/Example.Show.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	special := model.Media{
		LibraryID:  "lib-tv",
		Path:       `/media/tv/Example Show Specials/Example.Show.Special.01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	chineseSpecial := model.Media{
		LibraryID:  "lib-tv",
		Path:       `/media/anime/示例剧 特别篇/示例剧.SP01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	chineseMain := model.Media{
		LibraryID:  "lib-tv",
		Path:       `/media/anime/示例剧/Season 01/示例剧.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}

	if got, want := mediaSeriesKey(special), mediaSeriesKey(main); got != want {
		t.Fatalf("english special key=%q, want main key=%q", got, want)
	}
	if got, want := mediaSeriesKey(chineseSpecial), mediaSeriesKey(chineseMain); got != want {
		t.Fatalf("chinese special key=%q, want main key=%q", got, want)
	}
}

func TestMediaSeriesKeyCollapsesSeasonZeroAndSpecialAliases(t *testing.T) {
	main := model.Media{
		LibraryID:  "lib-anime",
		Path:       `/media/anime/宝可梦 (1997) {tmdb-60572}/Season 1/宝可梦.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	seasonZero := model.Media{
		LibraryID:  "lib-anime",
		Path:       `/media/anime/宝可梦 (1997) {tmdb-60572}/Season 0/宝可梦.S00E34.mkv`,
		SeasonNum:  0,
		EpisodeNum: 34,
	}
	specialEpisode := model.Media{
		LibraryID:  "lib-anime",
		Path:       `/media/anime/宝可梦 Special Episode/宝可梦.SP01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	extraEpisode := model.Media{
		LibraryID:  "lib-anime",
		Path:       `/media/anime/宝可梦 番外篇/宝可梦.SP02.mkv`,
		SeasonNum:  0,
		EpisodeNum: 2,
	}

	want := mediaSeriesKey(main)
	for name, item := range map[string]model.Media{
		"season zero":     seasonZero,
		"special episode": specialEpisode,
		"番外篇":             extraEpisode,
	} {
		if got := mediaSeriesKey(item); got != want {
			t.Fatalf("%s key=%q, want main key=%q", name, got, want)
		}
	}
}

func TestMediaSeriesKeyCollapsesNumberedSpecialSuffixes(t *testing.T) {
	main := model.Media{
		LibraryID:  "lib-tv",
		Path:       `F:\media\电视剧\欧美剧\Example Show\Season 01\Example Show - S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	chineseMain := model.Media{
		LibraryID:  "lib-tv",
		Path:       `F:\media\电视剧\欧美剧\示例剧\Season 01\示例剧.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	cases := map[string]struct {
		item model.Media
		want model.Media
	}{
		"sp number": {
			item: model.Media{
				LibraryID:  "lib-tv",
				Path:       `F:\media\电视剧\欧美剧\Example Show SP01\Example Show.SP01.mkv`,
				SeasonNum:  0,
				EpisodeNum: 1,
			},
			want: main,
		},
		"ova number": {
			item: model.Media{
				LibraryID:  "lib-tv",
				Path:       `F:\media\电视剧\欧美剧\Example Show OVA 1\Example Show.OVA.1.mkv`,
				SeasonNum:  0,
				EpisodeNum: 1,
			},
			want: main,
		},
		"season zero episode": {
			item: model.Media{
				LibraryID:  "lib-tv",
				Path:       `F:\media\电视剧\欧美剧\Example Show S00E01\Example Show.S00E01.mkv`,
				SeasonNum:  0,
				EpisodeNum: 1,
			},
			want: main,
		},
		"wrapped special": {
			item: model.Media{
				LibraryID:  "lib-tv",
				Path:       `F:\media\电视剧\欧美剧\Example Show [Special]\Example Show.Special.mkv`,
				SeasonNum:  0,
				EpisodeNum: 1,
			},
			want: main,
		},
		"chinese numbered special": {
			item: model.Media{
				LibraryID:  "lib-tv",
				Path:       `F:\media\电视剧\欧美剧\示例剧 特别篇 第1集\示例剧.SP01.mkv`,
				SeasonNum:  0,
				EpisodeNum: 1,
			},
			want: chineseMain,
		},
	}
	for name, tt := range cases {
		want := mediaSeriesKey(tt.want)
		if got := mediaSeriesKey(tt.item); got != want {
			t.Fatalf("%s key=%q, want main key=%q", name, got, want)
		}
	}
}

func TestMediaSeriesKeyCleansReleaseNoiseFolders(t *testing.T) {
	clean := model.Media{
		LibraryID:  "lib-variety",
		Path:       `F:\media\电视剧\综艺\Hntv Spring Festival Gala S01e (2026)\Season 1\Hntv Spring Festival Gala S01e - S01E202.ts`,
		SeasonNum:  1,
		EpisodeNum: 202,
	}
	dirty := model.Media{
		LibraryID:  "lib-variety",
		Path:       `F:\media\电视剧\综艺\Hntv Spring Festival Gala Fps Hlg Qhstudio S01e (2026)\Season 1\Hntv Spring Festival Gala Fps Hlg Qhstudio S01e - S01E202.ts`,
		SeasonNum:  1,
		EpisodeNum: 202,
	}
	if got, want := mediaSeriesKey(dirty), mediaSeriesKey(clean); got != want {
		t.Fatalf("dirty folder key=%q, want clean folder key=%q", got, want)
	}

	noisyRelease := model.Media{
		LibraryID:  "lib-tv",
		Path:       `F:\media\电视剧\欧美剧\Motherhood Of Taihang Aac2 Mweb\Season 1\Motherhood Of Taihang Aac2 Mweb - S01E01-Aac2.Mweb.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	cleanRelease := model.Media{
		LibraryID:  "lib-tv",
		Path:       `F:\media\电视剧\欧美剧\Motherhood Of Taihang\Season 1\Motherhood Of Taihang - S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	if got, want := mediaSeriesKey(noisyRelease), mediaSeriesKey(cleanRelease); got != want {
		t.Fatalf("release-noise folder key=%q, want clean key=%q", got, want)
	}
}

func TestMediaSeriesKeyTreatsDomesticTelevisionFolderAsSeries(t *testing.T) {
	main := model.Media{
		LibraryID:  "lib-domestic-tv",
		Path:       `/media/国产电视剧/人世间 (2022) [TMDBID-156568]/人世间.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
		TMDbID:     156568,
	}
	weakEpisode := model.Media{
		LibraryID: "lib-domestic-tv",
		Path:      `/media/国产电视剧/人世间 (2022) [TMDBID-156568]/人世间.S01E02.mkv`,
		// Some scans may miss S/E at first while local NFO or
		// scraper metadata already carries an episode-level TMDb id.
		TMDbID: 4375419,
	}
	folderRecord := model.Media{
		LibraryID: "lib-domestic-tv",
		Path:      `/media/国产电视剧/人世间 (2022) [TMDBID-156568]`,
		Title:     "人世间",
		TMDbID:    156568,
	}

	if got, want := mediaSeriesKey(weakEpisode), mediaSeriesKey(main); got != want {
		t.Fatalf("domestic television folder key=%q, want main key=%q", got, want)
	}
	if got, want := mediaSeriesKey(folderRecord), mediaSeriesKey(main); got != want {
		t.Fatalf("domestic television folder record key=%q, want main key=%q", got, want)
	}

	cards := groupMediaSeriesCards([]model.Media{main, weakEpisode, folderRecord})
	if len(cards) != 1 || cards[0].Count != 3 {
		t.Fatalf("cards=%#v, want one merged series card with three items", cards)
	}
}

func TestMediaSeriesKeyUsesSeriesDirectoryExternalID(t *testing.T) {
	episodeIDOnly := model.Media{
		LibraryID:  "lib-domestic-tv",
		Path:       `/media/电视剧/国产剧/人世间 (2022)/Season 01/人世间.S01E03.{tmdb-7129826}.mkv`,
		SeasonNum:  1,
		EpisodeNum: 3,
		TMDbID:     7129826,
	}
	cleanFolder := model.Media{
		LibraryID:  "lib-domestic-tv",
		Path:       `/media/电视剧/国产剧/人世间 (2022)/Season 01/人世间.S01E04.mkv`,
		SeasonNum:  1,
		EpisodeNum: 4,
		TMDbID:     156568,
	}
	if got, want := mediaSeriesKey(episodeIDOnly), mediaSeriesKey(cleanFolder); got != want {
		t.Fatalf("episode filename tmdb id should not split clean folder key=%q, want %q", got, want)
	}
}

func TestGroupMediaSeriesCardsPrioritizesCanonicalSeriesOverEpisodeHints(t *testing.T) {
	var items []model.Media
	for _, show := range []struct {
		id                 string
		episodes, versions int
	}{{"series-one", 167, 2}, {"series-three", 123, 1}} {
		for episode := 1; episode <= show.episodes; episode++ {
			for version := 0; version < show.versions; version++ {
				item := model.Media{
					PermanentBase: model.PermanentBase{ID: fmt.Sprintf("%s-%d-%d", show.id, episode, version)},
					LibraryID:     "tv", SeriesID: show.id, MetadataID: fmt.Sprintf("%s-episode-%d", show.id, episode),
					SeriesTitle: "相同展示名", Title: fmt.Sprintf("第 %d 集", episode),
					Path:      fmt.Sprintf("/media/tv/%s/version-%d/S01E%03d.mkv", show.id, version, episode),
					SeasonNum: 1, EpisodeNum: episode, TMDbID: 1000 + episode, ScrapeStatus: "matched",
				}
				if episode <= 36 {
					item.Year = 2011
				}
				items = append(items, item)
			}
		}
	}
	cards := groupMediaSeriesCards(items)
	if len(cards) != 2 {
		t.Fatalf("got %d cards, want two canonical Series", len(cards))
	}
	resolver := newMediaSeriesKeyResolver(items)
	for _, card := range cards {
		want := 167
		if card.Rep.SeriesID == "series-three" {
			want = 123
		}
		if card.Count != want {
			t.Fatalf("%s count=%d, want %d", card.Rep.SeriesID, card.Count, want)
		}
		files := 0
		for _, item := range items {
			if resolver.key(item) == card.Key {
				files++
				if item.SeriesID != card.Rep.SeriesID {
					t.Fatal("different Series merged through shared hints")
				}
				if newMediaSeriesKeyResolver([]model.Media{item}).key(item) != card.Key {
					t.Fatal("group key depends on page contents")
				}
			}
		}
		wantFiles := want
		if card.Rep.SeriesID == "series-one" {
			wantFiles *= 2
		}
		if files != wantFiles {
			t.Fatalf("selection returned %d files, want %d", files, wantFiles)
		}
	}
}

func TestGroupMediaSeriesCardsMergesPollutedEpisodeFoldersBySharedShowID(t *testing.T) {
	items := []model.Media{
		{
			LibraryID:    "lib-variety",
			Title:        "脱口秀和Ta的朋友们",
			Path:         `F:\media\电视剧\综艺\脱口秀和Ta的朋友们 第01期\Season 01\show.S01E01.mkv`,
			SeasonNum:    1,
			EpisodeNum:   1,
			TMDbID:       260001,
			ScrapeStatus: "matched",
		},
		{
			LibraryID:    "lib-variety",
			Title:        "脱口秀和Ta的朋友们",
			Path:         `F:\media\电视剧\综艺\脱口秀和Ta的朋友们 第02期\Season 01\show.S01E02.mkv`,
			SeasonNum:    1,
			EpisodeNum:   2,
			TMDbID:       260001,
			ScrapeStatus: "matched",
		},
	}

	cards := groupMediaSeriesCards(items)
	if len(cards) != 1 || cards[0].Count != 2 {
		t.Fatalf("cards=%#v, want one show card with two episodes", cards)
	}
}

func TestGroupMediaSeriesCardsKeepsMovieVersionsAsOneMovie(t *testing.T) {
	items := []model.Media{
		{
			PermanentBase: model.PermanentBase{ID: "movie-copy-a"},
			LibraryID:     "foreign-movies",
			Title:         "杀的就是你",
			Path:          `F:\media\电影\外语电影\They Will Kill You (2026)\movie-a.mkv`,
			TMDbID:        1292695,
		},
		{
			PermanentBase: model.PermanentBase{ID: "movie-copy-b"},
			LibraryID:     "western-movies",
			Title:         "杀的就是你",
			Path:          `F:\media\电影\欧美电影\They Will Kill You (2026)\movie-b.mkv`,
			TMDbID:        1292695,
		},
	}

	cards := groupMediaSeriesCards(items)
	if len(cards) != 1 {
		t.Fatalf("cards=%#v, want duplicate movie locations folded into one card", cards)
	}
	if cards[0].Count != 1 {
		t.Fatalf("movie card count=%d, want 1 so versions are not shown as episodes", cards[0].Count)
	}
}

func TestGroupMediaSeriesCardsDoesNotCollideMovieAndTVExternalIDs(t *testing.T) {
	movie := model.Media{
		PermanentBase: model.PermanentBase{ID: "movie"},
		LibraryID:     "movies",
		Title:         "同号电影",
		Path:          `/media/电影/同号电影 (2026)/movie.mkv`,
		TMDbID:        12345,
	}
	episode := model.Media{
		PermanentBase: model.PermanentBase{ID: "episode"},
		LibraryID:     "tv",
		Title:         "同号剧集",
		Path:          `/media/tv/同号剧集/episode.mkv`,
		SeasonNum:     1,
		EpisodeNum:    1,
		TMDbID:        12345,
	}

	cards := groupMediaSeriesCards([]model.Media{movie, episode})
	if len(cards) != 2 {
		t.Fatalf("cards=%#v, want movie and TV item kept separate despite equal numeric TMDb IDs", cards)
	}
}
