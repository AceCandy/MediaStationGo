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
	preserved, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || preserved == nil || preserved.MetadataID != episode.ID || preserved.Title != episode.Title || preserved.Overview != episode.Overview {
		t.Fatalf("Series edit changed Episode: %#v, %v", preserved, err)
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
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
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
}

func TestFilterLibrarySeriesCardsUsesSeriesCardMetadata(t *testing.T) {
	cards := func() []SeriesCard {
		return []SeriesCard{
			{Rep: model.Media{SeriesTitle: "中文剧集"}},
			{Rep: model.Media{SeriesTitle: "English Missing"}},
			{Rep: model.Media{SeriesTitle: "English Poster", PosterURL: "/api/artwork/poster"}},
		}
	}

	filtered := filterLibrarySeriesCards(cards(), MediaVisibility{MissingPoster: true})
	if len(filtered) != 2 || filtered[0].Rep.SeriesTitle != "中文剧集" || filtered[1].Rep.SeriesTitle != "English Missing" {
		t.Fatalf("missing poster cards = %#v", filtered)
	}
	filtered = filterLibrarySeriesCards(cards(), MediaVisibility{MissingChineseTitle: true})
	if len(filtered) != 2 || filtered[0].Rep.SeriesTitle != "English Missing" || filtered[1].Rep.SeriesTitle != "English Poster" {
		t.Fatalf("missing Chinese title cards = %#v", filtered)
	}
	filtered = filterLibrarySeriesCards(cards(), MediaVisibility{MissingPoster: true, MissingChineseTitle: true})
	if len(filtered) != 1 || filtered[0].Rep.SeriesTitle != "English Missing" {
		t.Fatalf("combined cards = %#v", filtered)
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
