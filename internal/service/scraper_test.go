package service

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestScraperAnyEnabledIgnoresAdultProvider(t *testing.T) {
	scraper := &ScraperService{adult: &AdultProvider{}}
	if scraper.AnyEnabled() {
		t.Fatal("adult provider alone must not enable the regular scrape chain")
	}
}

func TestEnrichOneReturnsNoMatchPersistenceError(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Definitely Missing Metadata Candidate",
		Path:         filepath.Join(lib.Path, "missing.mkv"),
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("forced no-match update failure")
	callbackName := "test:fail-no-match-update"
	if err := repos.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "media" {
			tx.AddError(wantErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repos.DB.Callback().Update().Remove(callbackName) })

	if err := scraper.EnrichOne(t.Context(), &media); !errors.Is(err, wantErr) {
		t.Fatalf("EnrichOne() error=%v, want %v", err, wantErr)
	}
}

func TestEnrichOneUsesExistingTMDbIDWithoutAdultLookup(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	var adultCalls atomic.Int32
	adult := NewAdultProvider(scraper.log, nil)
	adult.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		adultCalls.Add(1)
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})}
	scraper.adult = adult

	lib := model.Library{Name: "OpenList · 国漫", Path: "cloud://openlist/STRM-115/国漫", Type: "anime", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "dirty release title",
		Path:         "cloud://openlist/STRM-115/国漫/间谍过家家 (2022) {tmdb-12345}/Season 1/间谍过家家.S01E01.2160p.mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	got := serviceTestMediaView(t, repos, media.ID)
	if got.ScrapeStatus != "matched" || got.Title != "间谍过家家" || got.TMDbID != 12345 || got.PosterURL == "" {
		t.Fatalf("tmdb id scrape did not apply match: title=%q status=%q tmdb=%d poster=%q", got.Title, got.ScrapeStatus, got.TMDbID, got.PosterURL)
	}
	if calls := adultCalls.Load(); calls != 0 {
		t.Fatalf("adult provider was called %d times during regular scrape", calls)
	}
}

func TestEnrichOneWritesTMDbIdentifier(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}).Error; err != nil {
		t.Fatal(err)
	}

	var media model.Media
	if err := repos.DB.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	if got.ScrapeStatus != "matched" || got.TMDbID != 12345 {
		t.Fatalf("unexpected scraped media: status=%q tmdb=%d", got.ScrapeStatus, got.TMDbID)
	}
}

func TestEnrichOneTreatsEpisodicMediaInMovieLibraryAsTV(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "混合库", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家 S02E01",
		Path:         filepath.Join(lib.Path, "间谍过家家", "Season 02", "间谍过家家 - S02E01.mkv"),
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	if got.ScrapeStatus != "matched" || got.TMDbID != 12345 {
		t.Fatalf("episodic media in movie library should use tv scrape: status=%q tmdb=%d", got.ScrapeStatus, got.TMDbID)
	}
}

func TestDetermineMediaTypeForMediaHonorsExplicitMatchType(t *testing.T) {
	scraper := &ScraperService{}
	lib := &model.Library{Name: "欧美剧", Type: "tv"}
	media := &model.Media{
		Title:      "错误识别的电影",
		Path:       filepath.Join("library", "欧美剧", "错误识别的电影 (2024)", "错误识别的电影.S01E202.mkv"),
		SeasonNum:  1,
		EpisodeNum: 202,
	}

	tests := []struct {
		name  string
		match *Match
		want  string
	}{
		{name: "movie match overrides stale episode hints", match: &Match{MediaType: "movie"}, want: "movie"},
		{name: "tv match stays tv", match: &Match{MediaType: "tv"}, want: "tv"},
		{name: "anime match uses tmdb tv endpoint", match: &Match{MediaType: "anime"}, want: "tv"},
		{name: "variety match uses tmdb tv endpoint", match: &Match{MediaType: "variety"}, want: "tv"},
		{name: "adult match uses tmdb movie endpoint", match: &Match{MediaType: "adult"}, want: "movie"},
		{name: "unknown match falls back to episodic hints", match: &Match{}, want: "tv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scraper.determineMediaTypeForMedia(lib, media, tt.match); got != tt.want {
				t.Fatalf("determineMediaTypeForMedia() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnrichOneWritesTMDbEpisodeMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv")
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	// 单集专属信息(简介/剧照/评分/时长)应回填到该集行。
	if got.Overview != "单集剧情" {
		t.Fatalf("episode overview not saved: overview=%q", got.Overview)
	}
	if got.BackdropURL == "" || got.DurationSec != 24*60 {
		t.Fatalf("episode still/runtime not saved: backdrop=%q duration=%d", got.BackdropURL, got.DurationSec)
	}
	if got.Rating < 9.09 || got.Rating > 9.11 {
		t.Fatalf("episode rating = %v, want 9.1", got.Rating)
	}
	if got.Title != "任务代号: 猫" || got.SeriesTitle != "间谍过家家" {
		t.Fatalf("episode/series titles = %q/%q", got.Title, got.SeriesTitle)
	}
	if got.OriginalName != "" {
		t.Fatalf("episode original_name must not inherit series metadata, got %q", got.OriginalName)
	}
}

func TestEnrichOneSkipsTMDbEpisodeStillWhenDisabled(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv")
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	episodeArtwork := false
	if err := scraper.EnrichOneWithOptions(t.Context(), &media, ScrapeOptions{EpisodeArtwork: &episodeArtwork}); err != nil {
		t.Fatal(err)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	if got.Overview != "单集剧情" || got.DurationSec != 24*60 {
		t.Fatalf("episode metadata should still be saved: overview=%q duration=%d", got.Overview, got.DurationSec)
	}
	if got.Rating < 9.09 || got.Rating > 9.11 {
		t.Fatalf("episode rating = %v, want 9.1", got.Rating)
	}
	var stillCount int64
	if got.MetadataID == "" {
		t.Fatal("matched episode has no metadata link")
	}
	if err := repos.DB.Model(&model.MetadataArtwork{}).
		Where("metadata_id = ? AND artwork_type = ?", got.MetadataID, model.ArtworkTypeStill).Count(&stillCount).Error; err != nil {
		t.Fatal(err)
	}
	if stillCount != 0 {
		t.Fatalf("episode still should not be saved when disabled, rows=%d", stillCount)
	}
	if got.PosterURL == "" || got.BackdropURL == "" {
		t.Fatalf("series artwork should remain available: poster=%q backdrop=%q", got.PosterURL, got.BackdropURL)
	}
}

func TestApplyManualMatchSkipsTMDbEpisodeStillWhenDisabled(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "待匹配",
		Path:         filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv"),
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	episodeArtwork := false
	got, err := scraper.ApplyManualMatch(t.Context(), media.ID, ManualScrapeRequest{
		Source:         "tmdb",
		MediaType:      "tv",
		Title:          "间谍过家家",
		TMDbID:         12345,
		EpisodeArtwork: &episodeArtwork,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("manual match returned nil media")
	}
	view := serviceTestMediaView(t, repos, media.ID)
	if view.Overview != "单集剧情" || view.DurationSec != 24*60 {
		t.Fatalf("episode metadata should still be saved: overview=%q duration=%d", view.Overview, view.DurationSec)
	}
	var stillCount int64
	if view.MetadataID == "" {
		t.Fatal("manual matched episode has no metadata link")
	}
	if err := repos.DB.Model(&model.MetadataArtwork{}).
		Where("metadata_id = ? AND artwork_type = ?", view.MetadataID, model.ArtworkTypeStill).Count(&stillCount).Error; err != nil {
		t.Fatal(err)
	}
	if stillCount != 0 {
		t.Fatalf("manual episode still should not be saved when disabled, rows=%d", stillCount)
	}
	if view.PosterURL == "" || view.BackdropURL == "" {
		t.Fatalf("series artwork should remain available: poster=%q backdrop=%q", view.PosterURL, view.BackdropURL)
	}
}
