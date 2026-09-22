package service

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestOrganizeDirectoryScanAndScrapeAfter(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Spy.x.Family.S01E01.2022.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")
	writeOrgFile(t, filepath.Join(src, "tvshow.nfo"), `<tvshow><title>Spy Family {tmdb-12345}</title><uniqueid type="tmdb">12345</uniqueid></tvshow>`)

	lib := model.Library{
		Name:    "剧集",
		Path:    filepath.Join(dest, "电视剧"),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		MediaType:    "tv",
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1", res.Organized)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, scraper)
	scans, scrapes := scanner.ScanAndScrapeLibrariesForPath(t.Context(), res.DestPath, "", true)
	if len(scans) != 1 || scans[0].Added+scans[0].Updated != 1 {
		t.Fatalf("scans = %#v, want one synchronized file", scans)
	}
	if len(scrapes) != 1 || scrapes[0].Matched != 1 || scrapes[0].Error != "" || scrapes[0].Skipped {
		t.Fatalf("scrapes = %#v, want one successful matched scrape", scrapes)
	}

	var media model.Media
	if err := repos.DB.Where("path LIKE ?", "%Spy Family {tmdb-12345} - S01E01.mkv").First(&media).Error; err != nil {
		t.Fatal(err)
	}
	view := serviceTestMediaView(t, repos, media.ID)
	serviceTestTMDbSeries(t, repos, view, 12345)
	if view.ScrapeStatus != "matched" {
		t.Fatalf("media scrape status=%q, want matched", view.ScrapeStatus)
	}
	if _, err := os.Stat(media.Path); err != nil {
		t.Fatalf("organized file missing at %q: %v", media.Path, err)
	}
}

func TestOrganizeDirectoryUsesScraperMatchBeforeRename(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Spy x Family {tmdb-12345}", "Spy.x.Family.S01E01.2022.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		MediaType:    "tv",
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1", res.Organized)
	}
	want := filepath.Join(dest, "电视剧", "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("organized file should use matched metadata path %q: %v; items=%#v", want, err, res.Items)
	}
	if len(res.Items) != 1 || res.Items[0].Target != want || res.Items[0].Title != "间谍过家家" {
		t.Fatalf("organize preview did not use scraper metadata: %#v", res.Items)
	}
	var media model.Media
	if err := repos.DB.First(&media, "path = ?", want).Error; err != nil {
		t.Fatalf("organized metadata should be persisted before scan: %v", err)
	}
	view := serviceTestMediaView(t, repos, media.ID)
	series := serviceTestTMDbSeries(t, repos, view, 12345)
	if series.Title != "间谍过家家" || view.ScrapeStatus != "matched" {
		t.Fatalf("persisted series = title=%q status=%q, want localized matched metadata", series.Title, view.ScrapeStatus)
	}
}

func TestOrganizePipelineRenamesAfterScrape(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Spy.x.Family.S01E01.2022.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")
	writeOrgFile(t, filepath.Join(src, "tvshow.nfo"), `<tvshow><title>Spy Family {tmdb-12345}</title><uniqueid type="tmdb">12345</uniqueid></tvshow>`)

	lib := model.Library{
		Name:    "剧集",
		Path:    filepath.Join(dest, "电视剧"),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, scraper)
	pipeline := NewOrganizePipelineService(zap.NewNop(), repos, organizer, scanner, nil)
	_, err := pipeline.Run(t.Context(), OrganizePipelineRequest{
		Scope:        OrganizeScopeDirectory,
		Trigger:      OrganizeTriggerManual,
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: string(TransferCopy),
		MediaType:    "tv",
	})
	if err != nil {
		t.Fatalf("pipeline organize: %v", err)
	}

	englishPath := filepath.Join(dest, "电视剧", "Spy Family {tmdb-12345}", "Season 01", "Spy Family {tmdb-12345} - S01E01.mkv")
	if _, err := os.Stat(englishPath); !os.IsNotExist(err) {
		t.Fatalf("english pre-scrape path should be renamed away, stat err=%v", err)
	}
	englishDir := filepath.Join(dest, "电视剧", "Spy Family {tmdb-12345}")
	if _, err := os.Stat(englishDir); !os.IsNotExist(err) {
		t.Fatalf("empty english pre-scrape directory should be cleaned up, stat err=%v", err)
	}
	want := filepath.Join(dest, "电视剧", "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("scraped rename target missing %q: %v", want, err)
	}
	var stored model.Media
	if err := repos.DB.First(&stored, "path = ?", want).Error; err != nil {
		t.Fatal(err)
	}
	got := serviceTestMediaView(t, repos, stored.ID)
	if got.SeriesTitle != "间谍过家家" || got.ScrapeStatus != "matched" {
		t.Fatalf("media after scrape rename = series=%q release=%q status=%q", got.SeriesTitle, got.ReleaseDate, got.ScrapeStatus)
	}
	var series model.MetadataItem
	if err := repos.DB.First(&series, "id = ?", got.SeriesID).Error; err != nil {
		t.Fatal(err)
	}
	if series.ReleaseDate != "2022-04-09" {
		t.Fatalf("series release date = %q, want 2022-04-09", series.ReleaseDate)
	}
}

func TestOrganizeMediaRefreshesMetadataBeforeRename(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media", "电视剧")
	sourceFile := filepath.Join(src, "Spy x Family {tmdb-12345}", "Spy.x.Family.S01E01.2022.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	lib := model.Library{
		Name:    "剧集",
		Path:    dest,
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Spy x Family S01E01 2022 1080p",
		Path:         sourceFile,
		Container:    "mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.Media.Upsert(t.Context(), serviceTestMediaForUpsert(&media)); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	dst, err := organizer.OrganizeMediaWithOptions(t.Context(), media.ID, OrganizeOptions{
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize media: %v", err)
	}
	want := filepath.Join(dest, "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	if dst != want {
		t.Fatalf("dst = %q, want %q", dst, want)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	series := serviceTestTMDbSeries(t, repos, got, 12345)
	if series.Title != "间谍过家家" || got.ScrapeStatus != "matched" {
		t.Fatalf("series = title=%q status=%q, want localized matched metadata", series.Title, got.ScrapeStatus)
	}
	if got.Path != want {
		t.Fatalf("media path = %q, want %q", got.Path, want)
	}
}

func TestOrganizeMediaRefreshesMatchedReleaseTitleBeforeRename(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media", "电视剧")
	sourceFile := filepath.Join(src, "Spy x Family {tmdb-12345}", "Spy.x.Family.S01E01.2022.1080p.WEB-DL.mkv")
	writeOrgFile(t, sourceFile, "episode")

	lib := model.Library{Name: "剧集", Path: dest, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Spy.x.Family.S01E01.2022.1080p.WEB-DL",
		Path:         sourceFile,
		Container:    "mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), serviceTestMediaForUpsert(&media)); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	dst, err := organizer.OrganizeMediaWithOptions(t.Context(), media.ID, OrganizeOptions{
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize media: %v", err)
	}
	want := filepath.Join(dest, "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	if dst != want {
		t.Fatalf("dst = %q, want %q", dst, want)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	series := serviceTestTMDbSeries(t, repos, got, 12345)
	if series.Title != "间谍过家家" || series.OriginalName != "SPY×FAMILY" {
		t.Fatalf("series = title=%q original=%q, want refreshed localized metadata", series.Title, series.OriginalName)
	}
}

func TestOrganizeMediaPersistsMetadataWhenAlreadyInPlace(t *testing.T) {
	scraper, repos, closeServer := newUnboundTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	libRoot := filepath.Join(root, "media", "电视剧")
	mediaPath := filepath.Join(libRoot, "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	writeOrgFile(t, mediaPath, "episode")

	lib := model.Library{Name: "剧集", Path: libRoot, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Spy.x.Family.S01E01.2022.1080p.WEB-DL",
		Path:         mediaPath,
		Container:    "mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), serviceTestMediaForUpsert(&media)); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	dst, err := organizer.OrganizeMediaWithOptions(t.Context(), media.ID, OrganizeOptions{
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize media already in place: %v", err)
	}
	if dst != mediaPath {
		t.Fatalf("dst = %q, want existing path %q", dst, mediaPath)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	series := serviceTestTMDbSeries(t, repos, got, 12345)
	if series.Title != "间谍过家家" || series.OriginalName != "SPY×FAMILY" || got.ScrapeStatus != "matched" {
		t.Fatalf("metadata not persisted for already-in-place media: title=%q original=%q status=%q", series.Title, series.OriginalName, got.ScrapeStatus)
	}
}

func TestOrganizeLibraryPersistsMetadataForInPlaceWeakRows(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	libRoot := filepath.Join(root, "media", "动漫", "日番")
	mediaPath := filepath.Join(libRoot, "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	writeOrgFile(t, mediaPath, "episode")

	lib := model.Library{Name: "日番", Path: libRoot, Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Spy.x.Family.S01E01.2022.1080p.WEB-DL",
		Path:         mediaPath,
		Container:    "mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), serviceTestMediaForUpsert(&media)); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeLibraryWithOptions(t.Context(), lib.ID, OrganizeOptions{})
	if err != nil {
		t.Fatalf("organize library: %v", err)
	}
	if res.Organized != 0 || res.Reclassified != 0 {
		t.Fatalf("result = %+v, want metadata-only refresh without move", res)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	series := serviceTestTMDbSeries(t, repos, got, 12345)
	if series.Title != "间谍过家家" || series.OriginalName != "SPY×FAMILY" || got.ScrapeStatus != "matched" {
		t.Fatalf("metadata not persisted for in-place library row: title=%q original=%q status=%q", series.Title, series.OriginalName, got.ScrapeStatus)
	}
}

func TestOrganizeScanAndScrapeRetriesNoMatchRows(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	libRoot := filepath.Join(root, "media", "电视剧")
	mediaPath := filepath.Join(libRoot, "间谍过家家 {tmdb-12345}", "Season 02", "间谍过家家 - S02E02.mkv")
	writeOrgFile(t, mediaPath, "episode")

	lib := model.Library{
		Name:    "剧集",
		Path:    libRoot,
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "no_match",
	}
	if err := repos.Media.Upsert(t.Context(), serviceTestMediaForUpsert(&media)); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, scraper)
	_, scrapes := scanner.ScanAndScrapeLibrariesForPath(t.Context(), filepath.Join(root, "media"), "", true)
	if len(scrapes) != 1 || scrapes[0].Matched != 1 || scrapes[0].Error != "" || scrapes[0].Skipped {
		t.Fatalf("scrapes = %#v, want one retried no_match row", scrapes)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	serviceTestTMDbSeries(t, repos, got, 12345)
	if got.ScrapeStatus != "matched" {
		t.Fatalf("media scrape status=%q, want matched", got.ScrapeStatus)
	}
}

func TestOrganizeScanAndScrapeRepairsWeakMatchedReleaseTitle(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	libRoot := filepath.Join(root, "media", "电视剧")
	mediaPath := filepath.Join(libRoot, "Spy.x.Family {tmdb-12345}", "Season 01", "Spy.x.Family.S01E01.2022.1080p.WEB-DL.mkv")
	writeOrgFile(t, mediaPath, "episode")

	lib := model.Library{
		Name:    "剧集",
		Path:    libRoot,
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Spy.x.Family.S01E01.2022.1080p.WEB-DL",
		Path:         mediaPath,
		SeasonNum:    1,
		EpisodeNum:   1,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), serviceTestMediaForUpsert(&media)); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, scraper)
	_, scrapes := scanner.ScanAndScrapeLibrariesForPath(t.Context(), libRoot, "", true)
	if len(scrapes) != 1 || scrapes[0].Matched != 1 || scrapes[0].Processed != 1 || scrapes[0].Error != "" || scrapes[0].Skipped {
		t.Fatalf("scrapes = %#v, want one repaired matched release row", scrapes)
	}

	got := serviceTestMediaView(t, repos, media.ID)
	series := serviceTestTMDbSeries(t, repos, got, 12345)
	if series.Title != "间谍过家家" || got.ScrapeStatus != "matched" {
		t.Fatalf("series = title=%q status=%q, want localized matched metadata", series.Title, got.ScrapeStatus)
	}
}

func TestOrganizeResultNeedsVisibilitySyncIgnoresScannedDuplicates(t *testing.T) {
	if OrganizeResultNeedsVisibilitySync(&OrganizeResult{
		Skipped: 1,
		Items:   []OrganizePreviewItem{{Action: "skip", Reason: organizeSkipDuplicateLibrary}},
	}) {
		t.Fatal("already-scanned duplicate should not trigger another visibility scan")
	}
	if OrganizeResultNeedsVisibilitySync(&OrganizeResult{
		Skipped: 1,
		Items:   []OrganizePreviewItem{{Action: "skip", Reason: organizeSkipSampleClip}},
	}) {
		t.Fatal("sample clip skip should not trigger visibility scan")
	}
	if !OrganizeResultNeedsVisibilitySync(&OrganizeResult{
		Skipped: 1,
		Items:   []OrganizePreviewItem{{Action: "skip", Reason: organizeSkipTargetExists}},
	}) {
		t.Fatal("unscanned target file should trigger visibility scan")
	}
	if !OrganizeResultNeedsVisibilitySync(&OrganizeResult{Organized: 1}) {
		t.Fatal("organized files must trigger visibility scan")
	}
	if !OrganizeResultNeedsVisibilitySync(&OrganizeResult{Reclassified: 1}) {
		t.Fatal("reclassified files must trigger visibility scan")
	}
}

func TestOrganizeScrapeAfterEnabledDefaultsOn(t *testing.T) {
	if !OrganizeScrapeAfterEnabled(t.Context(), nil) {
		t.Fatalf("organize scrape-after should default on without a repo")
	}
	repos := newOrganizerTestRepo(t)
	if !OrganizeScrapeAfterEnabled(t.Context(), repos) {
		t.Fatalf("organize scrape-after should default on when setting is absent")
	}
	if err := repos.Setting.Set(t.Context(), "organize.scrape_after", "false"); err != nil {
		t.Fatal(err)
	}
	if OrganizeScrapeAfterEnabled(t.Context(), repos) {
		t.Fatalf("explicit organize.scrape_after=false should be respected")
	}
}

func TestOrganizeMediaViewsKeepsSeriesNamingSeparateFromEpisodeDisplay(t *testing.T) {
	views := []model.MediaView{
		{MetadataKind: model.MetadataKindEpisode, Title: "单集名称", SeriesTitle: "整剧名称"},
		{MetadataKind: model.MetadataKindEpisode, Title: "无父级标题"},
		{MetadataKind: model.MetadataKindMovie, Title: "电影名称"},
	}
	rows := organizeMediaViews(views)
	if rows[0].Title != "整剧名称" || rows[1].Title != "无父级标题" || rows[2].Title != "电影名称" || views[0].Title != "单集名称" {
		t.Fatalf("organizer names=%q/%q/%q, episode display=%q", rows[0].Title, rows[1].Title, rows[2].Title, views[0].Title)
	}
}
