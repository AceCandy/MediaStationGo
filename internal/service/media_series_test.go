package service

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaSeriesKeyCollapsesNestedSpecialFolders(t *testing.T) {
	main := model.Media{
		LibraryID:  "lib-tv",
		Path:       `cloud://openlist/动漫/国漫/示例剧/Season 01/示例剧.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	special := model.Media{
		LibraryID: "lib-tv",
		Path:      `cloud://openlist/动漫/国漫/示例剧/Extras/Season 01/示例剧.SP01.mkv`,
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
		Base:       model.Base{CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour)},
		LibraryID:  "lib-tv",
		Title:      "更新合集",
		Path:       `F:\media\电视剧\国产剧\更新合集\Season 01\更新合集.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	newerLatestEpisode := model.Media{
		Base:       model.Base{CreatedAt: now, UpdatedAt: now},
		LibraryID:  "lib-tv",
		Title:      "更新合集",
		Path:       `F:\media\电视剧\国产剧\更新合集\Season 01\更新合集.S01E02.mkv`,
		SeasonNum:  1,
		EpisodeNum: 2,
	}
	olderSeries := model.Media{
		Base:       model.Base{CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour)},
		LibraryID:  "lib-tv",
		Title:      "较早合集",
		Path:       `F:\media\电视剧\国产剧\较早合集\Season 01\较早合集.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
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
		Path:       `cloud://openlist/电视剧/欧美剧/Example Show/Season 01/Example.Show.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	special := model.Media{
		LibraryID:  "lib-tv",
		Path:       `cloud://openlist/电视剧/欧美剧/Example Show Specials/Example.Show.Special.01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	chineseSpecial := model.Media{
		LibraryID:  "lib-tv",
		Path:       `cloud://openlist/动漫/国漫/示例剧 特别篇/示例剧.SP01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	chineseMain := model.Media{
		LibraryID:  "lib-tv",
		Path:       `cloud://openlist/动漫/国漫/示例剧/Season 01/示例剧.S01E01.mkv`,
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
		Path:       `cloud://openlist/动漫/日番/宝可梦 (1997) {tmdb-60572}/Season 1/宝可梦.S01E01.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	seasonZero := model.Media{
		LibraryID:  "lib-anime",
		Path:       `cloud://openlist/动漫/日番/宝可梦 (1997) {tmdb-60572}/Season 0/宝可梦.S00E34.mkv`,
		SeasonNum:  0,
		EpisodeNum: 34,
	}
	specialEpisode := model.Media{
		LibraryID:  "lib-anime",
		Path:       `cloud://openlist/动漫/日番/宝可梦 Special Episode/宝可梦.SP01.mkv`,
		SeasonNum:  0,
		EpisodeNum: 1,
	}
	extraEpisode := model.Media{
		LibraryID:  "lib-anime",
		Path:       `cloud://openlist/动漫/日番/宝可梦 番外篇/宝可梦.SP02.mkv`,
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
		// Some local/cloud scans may miss S/E at first while local NFO or
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
