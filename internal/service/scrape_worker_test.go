package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestClaimNextPendingMediaGroupClaimsWholeSeries(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "pending"},
		{LibraryID: library.ID, SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e02.mkv", SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "pending"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	group, err := scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if group == nil || len(group.MediaIDs) != 2 {
		t.Fatalf("claimed group = %#v, want complete two-episode series", group)
	}
	var running int64
	if err := repos.DB.Model(&model.Media{}).Where("series_hint = ? AND scrape_status = ?", "series-1", "running").Count(&running).Error; err != nil {
		t.Fatal(err)
	}
	if running != 2 {
		t.Fatalf("running rows = %d, want 2", running)
	}

	late := model.Media{LibraryID: library.ID, SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e03.mkv", SeasonNum: 1, EpisodeNum: 3, ScrapeStatus: "pending"}
	if err := repos.DB.Create(&late).Error; err != nil {
		t.Fatal(err)
	}
	group, err = scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if group != nil {
		t.Fatalf("claimed late episode while series is running: %#v", group)
	}
}

func TestRecoverRunningMediaScrapes(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "Movie", Path: "/media/movies/movie.mkv", ScrapeStatus: "running"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.recoverRunningMediaScrapes(t.Context()); err != nil {
		t.Fatal(err)
	}
	var stored model.Media
	if err := repos.DB.First(&stored, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ScrapeStatus != "pending" {
		t.Fatalf("scrape status = %q, want pending", stored.ScrapeStatus)
	}
}

func TestResetMediaScrapeResetsCompleteSeries(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, MetadataID: "episode-1", SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "matched"},
		{LibraryID: library.ID, MetadataID: "episode-2", SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e02.mkv", SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "matched"},
	}
	metadata := []model.MetadataItem{
		{Base: model.Base{ID: "episode-1"}, Kind: model.MetadataKindMovie, Title: "Episode 1", Source: "test"},
		{Base: model.Base{ID: "episode-2"}, Kind: model.MetadataKindMovie, Title: "Episode 2", Source: "test"},
	}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := scraper.ResetMediaScrape(t.Context(), rows[0].ID); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := repos.DB.Model(&model.Media{}).Where("series_hint = ? AND scrape_status = ?", "series-1", "pending").Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Fatalf("pending rows = %d, want complete two-episode series", pending)
	}
}
