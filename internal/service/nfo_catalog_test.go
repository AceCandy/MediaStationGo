package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestNFOFreshStartupAndScan(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := database.AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	repos := repository.New(db)
	root := t.TempDir()
	path := filepath.Join(root, "旅行", "旅行 - 4K.mkv")
	writeOrgFile(t, path, "video")
	writeOrgFile(t, filepath.Join(filepath.Dir(path), "movie.nfo"), `<movie><title>本地旅行</title><tmdbid>123</tmdbid></movie>`)
	lib := model.Library{Name: "本地", Type: model.LibraryTypeNFOMovie, Path: root, Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}}
	proxy := NewImageProxy(cfg, log)
	proxy.SetLibraryRootsProvider(func() []string { return []string{root} })
	posterPath := filepath.Join(filepath.Dir(path), "poster.png")
	if err := os.WriteFile(posterPath, testArtworkPNG(t, 3, 2), 0600); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(cfg, log, repos, NewHub(log), nil, nil)
	scanner.SetImageProxy(proxy)
	for i := 0; i < 2; i++ {
		result, err := scanner.ScanLibrary(t.Context(), lib.ID)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && result.Added != 1 || i == 1 && result.Skipped != 1 {
			t.Fatalf("scan %d: %+v", i, result)
		}
	}
	media, err := repos.Media.FindByPath(t.Context(), path)
	if err != nil || media == nil || media.CatalogSource != model.CatalogSourceNFO || media.MetadataID != "" {
		t.Fatalf("media=%+v err=%v", media, err)
	}
	rows, _, total, err := repos.MediaView.ListLibraryMetadataPage(t.Context(), lib.ID, model.MetadataKindMovie, "", 0, 50, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Title != "本地旅行" || rows[0].PosterURL == "" {
		t.Fatalf("page=%+v total=%d err=%v", rows, total, err)
	}
	var shared int64
	if err := db.Model(&model.MetadataItem{}).Count(&shared).Error; err != nil || shared != 0 {
		t.Fatalf("shared=%d err=%v", shared, err)
	}
	user := model.User{Username: "nfo-viewer"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	playback := NewPlaybackService(log, repos)
	if _, err := playback.SetFavourite(t.Context(), user.ID, media.ID, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := playback.RecordProgress(t.Context(), user.ID, media.ID, "local-session", 30000, 120000, MediaVisibility{IncludeNSFW: true}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repos.NFO.UserState(t.Context(), user.ID, rows[0].CatalogItemID)
	if err != nil || !state.Favorite || state.PositionMs != 30000 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	var events int64
	if err := db.Model(&model.NFOPlaybackEvent{}).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	history, err := playback.ContinueHistory(t.Context(), user.ID, 20, MediaVisibility{IncludeNSFW: true})
	if err != nil || len(history) != 1 || history[0].Media.ID != media.ID {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	favorites, err := playback.ListFavourites(t.Context(), user.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil || len(favorites) != 1 {
		t.Fatalf("favorites=%+v err=%v", favorites, err)
	}
	emby := NewEmbyService(&config.Config{}, log, repos)
	page, err := emby.Items(t.Context(), ItemsParams{UserID: user.ID, ParentID: lib.ID})
	if err != nil || page["TotalRecordCount"] != int64(1) {
		t.Fatalf("Emby page=%+v err=%v", page, err)
	}
	page, err = emby.Items(t.Context(), ItemsParams{UserID: user.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	if err != nil || page["TotalRecordCount"] != int64(1) {
		t.Fatalf("Emby global=%+v err=%v", page, err)
	}
	if err := emby.RecordProgress(t.Context(), user.ID, rows[0].CatalogItemID, media.ID, "emby-session", 40000*10000, 120000*10000); err != nil {
		t.Fatal(err)
	}
	if err := emby.MarkPlayed(t.Context(), user.ID, rows[0].CatalogItemID, false); err != nil {
		t.Fatal(err)
	}
	state, err = repos.NFO.UserState(t.Context(), user.ID, rows[0].CatalogItemID)
	if err != nil || !state.Favorite || state.WatchedAt != nil {
		t.Fatalf("unwatch state=%+v err=%v", state, err)
	}
	if err := db.Model(&model.PlaybackHistory{}).Count(&shared).Error; err != nil || shared != 0 {
		t.Fatalf("shared histories=%d err=%v", shared, err)
	}
	if err := db.Model(&model.Favorite{}).Count(&shared).Error; err != nil || shared != 0 {
		t.Fatalf("shared favorites=%d err=%v", shared, err)
	}
	search, total, err := repos.MediaView.SearchFilteredPage(t.Context(), "本地旅行", 0, 50, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || total != 1 || len(search) != 1 {
		t.Fatalf("search=%+v total=%d err=%v", search, total, err)
	}
	all, total, err := repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{lib.ID}, 0, 50, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || total != 1 || len(all) != 1 {
		t.Fatalf("list=%+v total=%d err=%v", all, total, err)
	}
	oldPoster := rows[0].PosterURL
	if err := os.WriteFile(posterPath, testArtworkPNG(t, 4, 2), 0600); err != nil {
		t.Fatal(err)
	}
	watcher := NewWatcherService(log, repos, scanner, NewTaskTrackerService(log, NewHub(log)))
	watcher.processBatch(t.Context(), []duePath{{path: posterPath, libraryID: lib.ID}})
	updated, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || updated.PosterURL == oldPoster {
		t.Fatalf("sidecar image unchanged: %+v %v", updated, err)
	}
	if err := os.Remove(posterPath); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	preserved, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil || preserved.PosterURL != updated.PosterURL {
		t.Fatalf("missing image lost accepted asset: %+v %v", preserved, err)
	}
}

func TestNFOSeriesHierarchyAndStateIsolation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	root := t.TempDir()
	lib := model.Library{Name: "本地剧集", Type: model.LibraryTypeNFOTV, Path: root, Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(root, "本地节目")
	writeOrgFile(t, filepath.Join(show, "tvshow.nfo"), `<tvshow><title>本地节目</title></tvshow>`)
	paths := []string{filepath.Join(show, "Season 01", "节目 S01E01.mkv"), filepath.Join(show, "Season 01", "节目 S01E02.mkv"), filepath.Join(show, "Season 01", "节目 S01E02 - 4K.mkv"), filepath.Join(show, "Season 01", "其他 S01E02.mkv")}
	for _, path := range paths {
		writeOrgFile(t, path, "video")
		writeOrgFile(t, nfoPath(path), `<episodedetails><title>本地分集</title></episodedetails>`)
	}
	log := zap.NewNop()
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	for range 2 {
		if result, err := scanner.ScanLibrary(t.Context(), lib.ID); err != nil || result.ErrorCount != 0 {
			t.Fatalf("scan=%+v err=%v", result, err)
		}
	}
	var items []model.NFOItem
	if err := db.Where("kind = 'episode'").Find(&items).Error; err != nil || len(items) != 3 {
		t.Fatalf("episodes=%+v err=%v", items, err)
	}
	var files []model.Media
	if err := db.Order("path").Find(&files).Error; err != nil {
		t.Fatal(err)
	}
	views, err := repos.MediaView.NFOItemViews(t.Context(), items[0].ID, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || len(views) == 0 {
		t.Fatal(err)
	}
	seriesID := views[0].SeriesID
	web := NewMediaService(&config.Config{}, log, repos)
	cards, total, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 20, "", "", MediaVisibility{IncludeNSFW: true})
	if err != nil || total != 1 || len(cards) != 1 || cards[0].Rep.SeriesID != seriesID || cards[0].Rep.Title != "本地节目" {
		t.Fatalf("web series=%+v total=%d err=%v", cards, total, err)
	}
	detailCards, _, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 1, seriesID, "", MediaVisibility{IncludeNSFW: true})
	if err != nil || len(detailCards) != 1 || len(detailCards[0].Seasons) != 1 || detailCards[0].Seasons[0] != 1 {
		t.Fatalf("web series seasons=%+v err=%v", detailCards, err)
	}
	seasonDetails, err := web.GetMediaSeasonVisible(t.Context(), detailCards[0].SeasonMediaIDs[1], MediaVisibility{IncludeNSFW: true})
	if err != nil || seasonDetails == nil || seasonDetails.SeasonNum != 1 {
		t.Fatalf("web season representative=%+v err=%v", seasonDetails, err)
	}
	episodes, err := web.ListLibrarySeriesEpisodes(t.Context(), lib.ID, cards[0].Key, nil, MediaVisibility{IncludeNSFW: true})
	if err != nil || len(episodes) != 4 {
		t.Fatalf("web episodes=%+v err=%v", episodes, err)
	}
	season := 1
	filtered, err := web.ListLibrarySeriesEpisodes(t.Context(), lib.ID, cards[0].Key, &season, MediaVisibility{IncludeNSFW: true})
	if err != nil || len(filtered) != 4 {
		t.Fatalf("web filtered episodes=%+v err=%v", filtered, err)
	}
	recent, err := web.ListRecentSeriesCards(t.Context(), 10, MediaVisibility{IncludeNSFW: true})
	if err != nil || len(recent) != 1 || recent[0].Rep.SeriesID != seriesID {
		t.Fatalf("web recent=%+v err=%v", recent, err)
	}
	user := model.User{Username: "local-series-viewer"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	emby := NewEmbyService(&config.Config{}, log, repos)
	hints, err := emby.SearchHints(t.Context(), ItemsParams{UserID: user.ID, SearchTerm: "本地节目"})
	if err != nil || hints["TotalRecordCount"] != int64(1) || len(hints["SearchHints"].([]map[string]any)) != 1 {
		t.Fatalf("search hints=%+v err=%v", hints, err)
	}
	page, err := emby.Items(t.Context(), ItemsParams{UserID: user.ID, ParentID: seriesID})
	if err != nil || page["TotalRecordCount"] != int64(1) {
		t.Fatalf("seasons=%+v err=%v", page, err)
	}
	page, err = emby.Items(t.Context(), ItemsParams{UserID: user.ID, ParentID: seriesID, Recursive: true, IncludeItemTypes: []string{"Episode"}})
	if err != nil || page["TotalRecordCount"] != int64(3) {
		t.Fatalf("episodes=%+v err=%v", page, err)
	}
	if err := emby.SetFavorite(t.Context(), user.ID, seriesID, true); err != nil {
		t.Fatal(err)
	}
	if err := emby.SetFavorite(t.Context(), user.ID, views[0].CatalogItemID, true); err == nil {
		t.Fatal("accepted episode favorite")
	}
	if err := emby.MarkPlayed(t.Context(), user.ID, seriesID, true); err != nil {
		t.Fatal(err)
	}
	detail, err := emby.Item(t.Context(), seriesID, user.ID)
	if err != nil || detail["UserData"].(map[string]any)["Played"] != true {
		t.Fatalf("played=%+v err=%v", detail, err)
	}
	if err := emby.MarkPlayed(t.Context(), user.ID, seriesID, false); err != nil {
		t.Fatal(err)
	}
	playback := NewPlaybackService(log, repos)
	for _, file := range files {
		if err := playback.RecordProgress(t.Context(), user.ID, file.ID, "session", 30000, 120000, MediaVisibility{IncludeNSFW: true}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := playback.ContinueHistory(t.Context(), user.ID, 10, MediaVisibility{IncludeNSFW: true})
	if err != nil || len(history) != 1 {
		t.Fatalf("continue=%+v err=%v", history, err)
	}
	hidden, err := playback.ContinueHistory(t.Context(), user.ID, 10, MediaVisibility{HiddenLibraryIDs: []string{lib.ID}})
	if err != nil || len(hidden) != 0 {
		t.Fatalf("hidden history=%+v err=%v", hidden, err)
	}
	resume, err := emby.ResumeItems(t.Context(), user.ID, 10)
	if err != nil || resume["TotalRecordCount"] != int64(1) {
		t.Fatalf("Emby resume=%+v err=%v", resume, err)
	}
	var shared int64
	if err := db.Model(&model.MetadataItem{}).Count(&shared).Error; err != nil || shared != 0 {
		t.Fatalf("shared=%d err=%v", shared, err)
	}
}

func TestNFOLocalIdentityAndSnapshots(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "旅行 (2026)")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.nfo"), []byte(`<movie><title>旅行</title><tmdbid>42</tmdbid><plot>本地简介</plot></movie>`), 0600); err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Type: model.LibraryTypeNFOMovie, Path: root}
	first, err := readNFOIngest(lib, &model.Media{Path: filepath.Join(dir, "旅行 (2026) - 1080p.mkv")}, root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := readNFOIngest(lib, &model.Media{Path: filepath.Join(dir, "旅行 (2026) - 4K.mkv")}, root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].LocalKey != second.Items[0].LocalKey || first.Binding.VersionName != "1080p" || second.Binding.VersionName != "4K" {
		t.Fatal("explicit versions did not group")
	}
	otherKey, _ := nfoMovieIdentity(filepath.Join(dir, "另一部电影.mkv"), root)
	if otherKey == first.Items[0].LocalKey {
		t.Fatal("unrelated video grouped by directory")
	}
	rootKey, _ := nfoMovieIdentity(filepath.Join(root, filepath.Base(root)+" - 4K.mkv"), root)
	if rootKey[:11] != "movie-file:" {
		t.Fatal("root files grouped")
	}
	if first.Binding.Title != "旅行" || first.Binding.Overview != "本地简介" {
		t.Fatal("NFO fields missing")
	}
}

func TestNFORepositoryPreservesFilesAndPreviousSnapshot(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.NFOItem{}, &model.NFOMediaBinding{}, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "本地电影", Path: t.TempDir(), Type: model.LibraryTypeNFOMovie}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, "movie.mkv"), CatalogSource: model.CatalogSourceNFO, ScrapeStatus: "matched"}
	input := &repository.NFOIngest{Items: []model.NFOItem{{Kind: "movie", LocalKey: "file:movie.mkv", NFOFields: model.NFOFields{Title: "本地电影"}}}, Binding: model.NFOMediaBinding{Fingerprint: "one", NFOFields: model.NFOFields{Title: "本地电影"}}}
	for i := range 2 {
		if changed, err := repos.NFO.Ingest(t.Context(), &media, input); err != nil || changed != (i == 0) {
			t.Fatalf("scan %d changed=%v err=%v", i, changed, err)
		}
	}
	input.Binding.Title = "更新后的简介版本"
	input.Binding.Fingerprint = "two"
	if changed, err := repos.NFO.Ingest(t.Context(), &media, input); err != nil || !changed {
		t.Fatalf("sidecar-only change ignored: %v %v", changed, err)
	}
	var count int64
	if err := db.Model(&model.NFOItem{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("item count=%d err=%v", count, err)
	}
	media.ScrapeStatus, media.ScrapeError = "error", "损坏 NFO"
	if _, err := repos.NFO.Ingest(t.Context(), &media, nil); err != nil {
		t.Fatal(err)
	}
	var binding model.NFOMediaBinding
	if err := db.First(&binding, "media_id = ?", media.ID).Error; err != nil || binding.Title != "更新后的简介版本" {
		t.Fatalf("lost accepted snapshot: %v", err)
	}
	got, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil || got.MetadataID != "" || got.ScrapeStatus != "error" {
		t.Fatalf("invalid isolation/status: %#v %v", got, err)
	}
	views, err := repos.MediaView.NFOItemViews(t.Context(), "nfo-"+binding.ItemID, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || len(views) != 1 || views[0].Title != binding.Title || views[0].MetadataID != "" || views[0].CatalogItemID != "nfo-"+binding.ItemID {
		t.Fatalf("independent view=%+v err=%v", views, err)
	}
	views, err = repos.MediaView.NFOItemViews(t.Context(), "nfo-"+binding.ItemID, repository.MediaQueryFilter{IncludeNSFW: true, HiddenLibraryIDs: []string{lib.ID}})
	if err != nil || len(views) != 0 {
		t.Fatalf("hidden library leaked: %+v %v", views, err)
	}
	if err := db.Model(&model.NFOItem{}).Where("id = ?", binding.ItemID).Update("nsfw", true).Error; err != nil {
		t.Fatal(err)
	}
	views, err = repos.MediaView.NFOItemViews(t.Context(), binding.ItemID, repository.MediaQueryFilter{})
	if err != nil || len(views) != 0 {
		t.Fatalf("item NSFW leaked despite clean file snapshot: %+v %v", views, err)
	}
	views, err = repos.MediaView.NFOItemViews(t.Context(), binding.ItemID, repository.MediaQueryFilter{IncludeNSFW: true})
	if err != nil || len(views) != 1 || !views[0].NSFW {
		t.Fatalf("projected NSFW lost: %+v %v", views, err)
	}
}

func TestNFORejectsWrongDocumentAndUnknownSeason(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "local.mkv")
	lib := &model.Library{Type: model.LibraryTypeNFOMovie, Path: root}
	for _, body := range []string{`<tvshow><title>wrong</title></tvshow>`, `<html>not metadata</html>`} {
		writeOrgFile(t, nfoPath(path), body)
		if _, err := readNFOIngest(lib, &model.Media{Path: path}, root); err == nil {
			t.Fatalf("accepted wrong document: %s", body)
		}
	}
	lib.Type = model.LibraryTypeNFOTV
	for _, season := range []string{"", "bad", "-1"} {
		writeOrgFile(t, nfoPath(path), `<episodedetails><title>episode</title><season>`+season+`</season><episode>1</episode></episodedetails>`)
		if _, err := readNFOIngest(lib, &model.Media{Path: path}, root); err == nil {
			t.Fatalf("accepted unknown/invalid season %q", season)
		}
	}
	writeOrgFile(t, nfoPath(path), `<episodedetails><title>special</title><season>0</season><episode>1</episode></episodedetails>`)
	if _, err := readNFOIngest(lib, &model.Media{Path: path}, root); err != nil {
		t.Fatalf("explicit specials season rejected: %v", err)
	}
}
