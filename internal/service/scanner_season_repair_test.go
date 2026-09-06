package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestScanRepairsExplicitNegativeSeasonWithoutFileChanges(t *testing.T) {
	_, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	scanner := &ScannerService{repo: repos}
	lib := model.Library{Name: "测试剧集", Type: "tv", Path: t.TempDir()}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		season, ep int
		wantRepair bool
	}{
		{"孤独的美食家.S00E25.strm", -1, 25, true},
		{"Last Man.S00E01.strm", -1, 1, true},
		{"Show.S02E03.strm", -1, 3, true},
		{"Unknown.strm", -1, 1, false},
		{"Normal.S01E01.strm", 1, 1, false},
		{"Special.S00E02.strm", 0, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(lib.Path, tc.name)
			media := model.Media{LibraryID: lib.ID, Path: path, Title: tc.name, SeasonNum: tc.season, EpisodeNum: tc.ep, ScrapeStatus: "error", ScrapeError: "old error", ScanFileSizeBytes: 10, ScanFileMTimeNS: 100}
			if err := repos.Media.Upsert(t.Context(), &media); err != nil {
				t.Fatal(err)
			}
			cached, err := scanner.existingLocalMediaSnapshot(t.Context(), lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, snapshot := range []map[string]existingLocalMedia{nil, cached} {
				isNew, skip, _ := scanner.localMediaScanState(localMediaScanStateInput{ctx: t.Context(), libraryID: lib.ID, path: path, cleanPath: path, size: 10, modTimeNS: 100, existingMedia: snapshot})
				if isNew || skip == tc.wantRepair {
					t.Fatalf("new=%v skip=%v want repair=%v", isNew, skip, tc.wantRepair)
				}
			}
			if !tc.wantRepair {
				return
			}
			season, ep := scanEpisodeNumbers(&lib, path)
			incoming := model.Media{LibraryID: lib.ID, Path: path, SeasonNum: season, EpisodeNum: ep, ScanFileSizeBytes: 10, ScanFileMTimeNS: 100}
			if err := repos.Media.Upsert(t.Context(), &incoming); err != nil {
				t.Fatal(err)
			}
			stored, err := repos.Media.FindByID(t.Context(), media.ID)
			if err != nil || stored == nil || stored.SeasonNum != season || stored.EpisodeNum != ep || stored.ScrapeStatus != "pending" || stored.ScrapeError != "" {
				t.Fatalf("repair result=%#v err=%v", stored, err)
			}
			_, skip, _ := scanner.localMediaScanState(localMediaScanStateInput{ctx: t.Context(), libraryID: lib.ID, path: path, cleanPath: path, size: 10, modTimeNS: 100})
			if !skip {
				t.Fatal("repaired unchanged file should skip next scan")
			}
		})
	}
}

func TestScrapeRetryRepairsSpecialSeasonWithoutRescan(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "测试剧", Type: "tv", Path: t.TempDir()}
	if err := os.WriteFile(filepath.Join(lib.Path, "tvshow.nfo"), []byte(`<tvshow><title>测试剧</title><season>-1</season><episode>-1</episode><tmdbid>12345</tmdbid></tvshow>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	series := &model.MetadataItem{Kind: model.MetadataKindSeries, Title: "测试剧", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), series, []model.MetadataIdentifier{{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"}}); err != nil {
		t.Fatal(err)
	}
	season, err := repos.Metadata.UpsertSeason(t.Context(), &model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 0, Title: "特别篇", Source: "tmdb"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range []int{1, 25} {
		episode, err := repos.Metadata.UpsertEpisode(t.Context(), &model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: ep, Title: "特别故事", Source: "tmdb"})
		if err != nil {
			t.Fatal(err)
		}
		media := &model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, fmt.Sprintf("Show.S00E%02d.strm", ep)), SeasonNum: -1, EpisodeNum: ep, TMDbID: 12345, ScrapeStatus: "error"}
		if ep == 25 {
			media.SeasonNum = 0 // 已被旧补丁修过数据库，重读 NFO 仍必须保留第 0 季。
		}
		if err := os.WriteFile(nfoPath(media.Path), []byte(fmt.Sprintf(`<episodedetails><season>0</season><episode>%d</episode></episodedetails>`, ep)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := repos.Media.Upsert(t.Context(), media); err != nil {
			t.Fatal(err)
		}
		if ep == 1 {
			_, err = s.ResetLibraryScrape(t.Context(), lib.ID, false)
		} else {
			_, err = s.ResetMediaScrape(t.Context(), media.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnrichOne(t.Context(), media); err != nil {
			t.Fatal(err)
		}
		stored, err := repos.Media.FindByID(t.Context(), media.ID)
		if err != nil || stored.SeasonNum != 0 || stored.EpisodeNum != ep || stored.MetadataID != episode.ID || stored.ScrapeStatus != "matched" {
			t.Fatalf("retry result=%#v err=%v", stored, err)
		}
	}
}
