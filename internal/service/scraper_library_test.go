package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestManualEnrichLibraryRetriesNoMatchAndCountsRealMatches(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E02.mkv")
	if err := repos.DB.Create(&model.Media{
		LibraryID: lib.ID, TMDbID: 12345,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "no_match",
	}).Error; err != nil {
		t.Fatal(err)
	}

	if matched, err := scraper.EnrichLibrary(t.Context(), lib.ID); err != nil || matched != 0 {
		t.Fatalf("default EnrichLibrary matched=%d err=%v, want skipped no_match", matched, err)
	}
	if matched, err := scraper.EnrichLibrary(t.Context(), lib.ID, true); err != nil || matched != 1 {
		t.Fatalf("manual EnrichLibrary matched=%d err=%v, want one real match", matched, err)
	}
}

func TestEnrichLibraryBindsUnresolvedMediaAfterProviderMatch(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID: lib.ID, TMDbID: 12345, Title: "间谍过家家",
		Path:      filepath.Join(lib.Path, "间谍过家家 - S02E02.mkv"),
		SeasonNum: 2, EpisodeNum: 2, ScrapeStatus: "pending",
	}
	media.SeriesID = localSeriesIdentity(&media)
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.MetadataID != "" {
		t.Fatalf("metadata ID before provider lookup = %q, want empty", media.MetadataID)
	}

	result, err := scraper.EnrichLibraryDetailed(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates != 1 || result.Processed != 1 || result.Matched != 1 || result.Failed != 0 {
		t.Fatalf("result=%+v, want one unresolved media matched", result)
	}
	got, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.MetadataID == "" || got.ScrapeStatus != "matched" {
		t.Fatalf("media after provider lookup = %#v", got)
	}
	view, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || view == nil || view.MetadataKind != model.MetadataKindEpisode || view.SeriesID == "" || view.SeasonID == "" || view.SeasonNum != 2 || view.EpisodeNum != 2 {
		t.Fatalf("canonical episode view = %#v, err = %v", view, err)
	}
}

func TestEnrichOneReusesCanonicalBeforeProviderLookup(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Movies", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Cached movie", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{
		Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "408",
	}}); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID: lib.ID, Title: "Snow White", Path: filepath.Join(lib.Path, "Snow White.mkv"),
		TMDbID: 408, ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	closeServer()

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatalf("reuse canonical metadata without provider request: %v", err)
	}
	got, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.MetadataID != metadata.ID || got.ScrapeStatus != "matched" {
		t.Fatalf("media after canonical reuse = %#v", got)
	}
}

func TestEnrichOneReusesLinkedCanonicalBeforeProviderLookup(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}

	lib := model.Library{Name: "Movies", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Cached movie", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{
		Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "408",
	}}); err != nil {
		t.Fatal(err)
	}
	metadataUpdatedAt := metadata.UpdatedAt.Truncate(time.Microsecond)
	media := model.Media{
		LibraryID: lib.ID, MetadataID: metadata.ID, TMDbID: 408,
		Title: "Snow White", Path: filepath.Join(lib.Path, "Snow White.mkv"), ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	closeServer()

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatalf("reuse linked canonical metadata without provider request: %v", err)
	}
	got, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.MetadataID != metadata.ID || got.ScrapeStatus != "matched" {
		t.Fatalf("media after linked canonical reuse = %#v", got)
	}
	updated, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || !updated.UpdatedAt.Equal(metadataUpdatedAt) {
		t.Fatalf("canonical metadata was touched: before=%s after=%s", metadataUpdatedAt, updated.UpdatedAt)
	}
}

func TestEnrichOneReusesRecentGeneratedEpisodeMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}

	lib := model.Library{Name: "Shows", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{
		Kind: model.MetadataKindEpisode, SeasonNum: 2, EpisodeNum: 1, Title: "第 1 集", Source: "tmdb",
		PermanentBase: model.PermanentBase{UpdatedAt: time.Now().UTC().Add(-6 * 24 * time.Hour)},
	}
	episodeMetadata := createServiceTestEpisodeMetadata(t, repos.DB,
		model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}, episode,
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"})
	media := model.Media{
		LibraryID: lib.ID, MetadataID: episodeMetadata.ID, TMDbID: 12345, SeasonNum: 2, EpisodeNum: 1,
		Title: "Show", Path: filepath.Join(lib.Path, "Show - S02E01.mkv"), ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	closeServer()

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatalf("recent generated episode should be reused: %v", err)
	}
	got, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ScrapeStatus != "matched" || got.MetadataID != episodeMetadata.ID {
		t.Fatalf("recent generated episode reuse = %#v", got)
	}
}

func TestEnrichOneRefreshesStaleGeneratedEpisodeMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}

	lib := model.Library{Name: "Shows", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{
		Kind: model.MetadataKindEpisode, SeasonNum: 2, EpisodeNum: 1, Title: "第 1 集", Source: "tmdb",
		PermanentBase: model.PermanentBase{UpdatedAt: time.Now().UTC().Add(-8 * 24 * time.Hour)},
	}
	episodeMetadata := createServiceTestEpisodeMetadata(t, repos.DB,
		model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}, episode,
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"})
	media := model.Media{
		LibraryID: lib.ID, MetadataID: episodeMetadata.ID, TMDbID: 12345, SeasonNum: 2, EpisodeNum: 1,
		Title: "Show", Path: filepath.Join(lib.Path, "Show - S02E01.mkv"), ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	closeServer()

	if err := scraper.EnrichOne(t.Context(), &media); err == nil {
		t.Fatal("stale generated episode should continue to provider scraping")
	}
	got, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ScrapeStatus == "matched" {
		t.Fatalf("stale generated episode was incorrectly reused: %#v", got)
	}
}

func TestManualEnrichLibraryCanRefreshAlreadyMatchedRows(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID: lib.ID, TMDbID: 12345,
		Title:        "间谍过家家",
		Path:         filepath.Join(lib.Path, "间谍过家家 - S02E02.mkv"),
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "matched",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	defaultResult, err := scraper.EnrichLibraryDetailedWithOptions(t.Context(), lib.ID, ScrapeOptions{RetryNoMatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if defaultResult.Processed != 0 || defaultResult.Candidates != 0 {
		t.Fatalf("default manual scrape result=%+v, want matched rows skipped without IncludeMatched", defaultResult)
	}

	refreshResult, err := scraper.EnrichLibraryDetailedWithOptions(t.Context(), lib.ID, ScrapeOptions{
		RetryNoMatch:   true,
		IncludeMatched: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshResult.Processed != 1 || refreshResult.Matched != 1 || refreshResult.Candidates != 1 {
		t.Fatalf("refresh result=%+v, want already matched row reprocessed", refreshResult)
	}
}

func TestScrapeCandidateRowsPrioritizeLibraryArtworkBeforeEpisodes(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{
			PermanentBase: model.PermanentBase{ID: "001-episode"},
			LibraryID:     lib.ID,
			Title:         "间谍过家家 第 1 集",
			Path:          filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv"),
			SeasonNum:     2,
			EpisodeNum:    1,
			ScrapeStatus:  "pending",
		},
		{
			PermanentBase: model.PermanentBase{ID: "999-series"},
			LibraryID:     lib.ID,
			Title:         "间谍过家家",
			Path:          filepath.Join(lib.Path, "间谍过家家.mkv"),
			ScrapeStatus:  "pending",
		},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	got, err := scraper.scrapeCandidateRows(t.Context(), lib.ID, ScrapeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("candidate rows = %d, want 2", len(got))
	}
	if got[0].ID != "999-series" || got[1].ID != "001-episode" {
		t.Fatalf("scrape order = [%s, %s], want series-level row before episode row", got[0].ID, got[1].ID)
	}
}

func TestEnrichLibraryScopesCandidatesToRequestedLibrary(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	local := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	target := model.Library{
		Name:    "另一个番剧库",
		Path:    t.TempDir(),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.DB.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID:    target.ID,
		Title:        "间谍过家家",
		Path:         filepath.Join(target.Path, "间谍过家家 {tmdb-12345}", "间谍过家家 - S02E02.mkv"),
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "pending",
	}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := scraper.EnrichLibraryDetailed(t.Context(), target.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.Processed != 1 || result.Candidates != 1 || result.Failed != 0 {
		t.Fatalf("result=%+v, want requested library media to be scraped once", result)
	}
	var media model.Media
	if err := repos.DB.First(&media, "library_id = ?", target.ID).Error; err != nil {
		t.Fatal(err)
	}
	got := serviceTestMediaView(t, repos, media.ID)
	serviceTestTMDbSeries(t, repos, got, 12345)
	if got.ScrapeStatus != "matched" {
		t.Fatalf("requested library media was not enriched: status=%q", got.ScrapeStatus)
	}
}

func TestEnrichLibraryScrapesSharedMetadataOnce(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	first := model.Media{
		LibraryID: lib.ID, TMDbID: 12345,
		Title:        "间谍过家家",
		Path:         filepath.Join(lib.Path, "1080p", "间谍过家家 - S02E02.mkv"),
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	second := model.Media{
		LibraryID: lib.ID, TMDbID: 12345,
		MetadataID:   first.MetadataID,
		SeriesID:     first.SeriesID,
		Title:        first.Title,
		Path:         filepath.Join(lib.Path, "2160p", "间谍过家家 - S02E02.mkv"),
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "matched",
	}
	if err := repos.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	result, err := scraper.EnrichLibraryDetailed(t.Context(), lib.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates != 1 || result.Processed != 1 || result.Matched != 1 || result.Failed != 0 {
		t.Fatalf("result=%+v, want one metadata candidate processed and matched", result)
	}

	gotFirst, err := repos.Media.FindByID(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotSecond, err := repos.Media.FindByID(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotFirst.ScrapeStatus != "matched" || gotSecond.ScrapeStatus != "matched" {
		t.Fatalf("shared statuses=[%q, %q], want matched", gotFirst.ScrapeStatus, gotSecond.ScrapeStatus)
	}
	if gotFirst.MetadataID == "" || gotFirst.MetadataID != gotSecond.MetadataID {
		t.Fatalf("shared metadata IDs=[%q, %q], want same non-empty ID", gotFirst.MetadataID, gotSecond.MetadataID)
	}
	if gotFirst.TMDbID != 12345 || gotSecond.TMDbID != 12345 {
		t.Fatalf("shared TMDb IDs=[%d, %d], want 12345", gotFirst.TMDbID, gotSecond.TMDbID)
	}
	if gotFirst.Path != first.Path || gotSecond.Path != second.Path {
		t.Fatalf("media paths changed: [%q, %q]", gotFirst.Path, gotSecond.Path)
	}
}

func TestEnrichLibrarySharesNoMatchAcrossMetadataGroup(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	scraper.tmdb = nil

	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	first := model.Media{
		LibraryID:    lib.ID,
		Title:        "没有匹配的电影",
		Path:         filepath.Join(lib.Path, "没有匹配的电影 1080p.mkv"),
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	second := model.Media{
		LibraryID:    lib.ID,
		MetadataID:   first.MetadataID,
		Title:        first.Title,
		Path:         filepath.Join(lib.Path, "没有匹配的电影 2160p.mkv"),
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	result, err := scraper.EnrichLibraryDetailed(t.Context(), lib.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates != 1 || result.Processed != 1 || result.Matched != 0 || result.Failed != 0 {
		t.Fatalf("result=%+v, want one metadata candidate without a match", result)
	}
	for _, mediaID := range []string{first.ID, second.ID} {
		got, findErr := repos.Media.FindByID(t.Context(), mediaID)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if got.ScrapeStatus != "no_match" {
			t.Fatalf("media %s status=%q, want no_match", mediaID, got.ScrapeStatus)
		}
	}
}
