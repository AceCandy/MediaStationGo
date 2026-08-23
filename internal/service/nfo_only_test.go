package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestNFOOnlyMovieDoesNotUsePathHintOrProvider(t *testing.T) {
	scraper, repos, providerCalls := newNFOOnlyTestScraper(t)
	root := t.TempDir()
	library := model.Library{Name: "个人短片", Path: root, Type: model.LibraryTypeNFOMovie, Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "网络元数据", Source: "tmdb"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "27205"}).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "个人短片", Path: filepath.Join(root, "个人短片 {tmdb-27205}.mkv"), ScrapeStatus: "pending"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	stored, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ScrapeStatus != "no_match" || stored.MetadataID != "" {
		t.Fatalf("missing NFO status=%q metadata=%q, want no_match without canonical binding", stored.ScrapeStatus, stored.MetadataID)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
	}
}

func TestNFOOnlyMoviePersistsValidNFOAndReportsBrokenNFO(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		scraper, repos, providerCalls := newNFOOnlyTestScraper(t)
		root := t.TempDir()
		library := model.Library{Name: "个人短片", Path: root, Type: model.LibraryTypeNFOMovie, Enabled: true}
		if err := repos.DB.Create(&library).Error; err != nil {
			t.Fatal(err)
		}
		mediaPath := filepath.Join(root, "旅行记录.mkv")
		if err := os.WriteFile(nfoPath(mediaPath), []byte(`<movie><title>旅行记录</title><year>2026</year><plot>个人录制。</plot></movie>`), 0o644); err != nil {
			t.Fatal(err)
		}
		media := model.Media{LibraryID: library.ID, Title: "旅行记录 raw", Path: mediaPath, ScrapeStatus: "pending"}
		if err := repos.DB.Create(&media).Error; err != nil {
			t.Fatal(err)
		}

		if err := scraper.EnrichOne(t.Context(), &media); err != nil {
			t.Fatal(err)
		}
		got := serviceTestMediaView(t, repos, media.ID)
		if got.ScrapeStatus != "matched" || got.Title != "旅行记录" || got.Year != 2026 || got.Overview != "个人录制。" {
			t.Fatalf("valid NFO result = %#v", got)
		}
		if providerCalls.Load() != 0 {
			t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
		}
	})

	t.Run("broken", func(t *testing.T) {
		scraper, repos, providerCalls := newNFOOnlyTestScraper(t)
		root := t.TempDir()
		library := model.Library{Name: "个人短片", Path: root, Type: model.LibraryTypeNFOMovie, Enabled: true}
		if err := repos.DB.Create(&library).Error; err != nil {
			t.Fatal(err)
		}
		mediaPath := filepath.Join(root, "损坏.mkv")
		if err := os.WriteFile(nfoPath(mediaPath), []byte(`<movie><title>`), 0o644); err != nil {
			t.Fatal(err)
		}
		media := model.Media{LibraryID: library.ID, Title: "损坏", Path: mediaPath, ScrapeStatus: "pending"}
		if err := repos.DB.Create(&media).Error; err != nil {
			t.Fatal(err)
		}

		if err := scraper.EnrichOne(t.Context(), &media); err == nil {
			t.Fatal("broken NFO should return an error")
		}
		stored, err := repos.Media.FindByID(t.Context(), media.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.ScrapeStatus != "error" || strings.TrimSpace(stored.ScrapeError) == "" {
			t.Fatalf("broken NFO status=%q error=%q", stored.ScrapeStatus, stored.ScrapeError)
		}
		if providerCalls.Load() != 0 {
			t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
		}
	})
}

func TestNFOOnlyTVPersistsShowAndEpisodeNFO(t *testing.T) {
	scraper, repos, providerCalls := newNFOOnlyTestScraper(t)
	root := t.TempDir()
	showDir := filepath.Join(root, "自制节目")
	seasonDir := filepath.Join(showDir, "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(`<tvshow><title>自制节目</title><year>2026</year><plot>节目简介</plot></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "自制节目 S01E02.mkv")
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<episodedetails><title>第二集</title><season>1</season><episode>2</episode><plot>单集简介</plot></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "自制节目", Path: root, Type: model.LibraryTypeNFOTV, Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "raw", Path: mediaPath, ScrapeStatus: "pending"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	got := serviceTestMediaView(t, repos, media.ID)
	if got.ScrapeStatus != "matched" || got.SeriesTitle != "自制节目" || got.Title != "第二集" || got.SeasonNum != 1 || got.EpisodeNum != 2 {
		t.Fatalf("valid TV NFO result = %#v", got)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
	}
}

func TestNFOOnlyTVGroupReadsEachEpisodeNFO(t *testing.T) {
	scraper, repos, providerCalls := newNFOOnlyTestScraper(t)
	root := t.TempDir()
	showDir := filepath.Join(root, "自制节目")
	seasonDir := filepath.Join(showDir, "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(`<tvshow><title>自制节目</title><year>2026</year></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(seasonDir, "自制节目 S01E01.mkv"), filepath.Join(seasonDir, "自制节目 S01E02.mkv")}
	nfos := []string{
		`<episodedetails><title>第一集</title><season>1</season><episode>1</episode></episodedetails>`,
		`<episodedetails><title>第二集</title><season>1</season><episode>2</episode></episodedetails>`,
	}
	for i, path := range paths {
		if err := os.WriteFile(nfoPath(path), []byte(nfos[i]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	library := model.Library{Name: "自制节目", Path: root, Type: model.LibraryTypeNFOTV, Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, SeriesID: "local:show", Title: "raw 1", Path: paths[0], SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "running"},
		{LibraryID: library.ID, SeriesID: "local:show", Title: "raw 2", Path: paths[1], SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "running"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	group := scrapeCandidateGroup{Representative: rows[0], MediaIDs: []string{rows[0].ID, rows[1].ID}}
	if err := scraper.enrichCandidateGroup(t.Context(), group, ScrapeOptions{result: &scrapeResult{}}); err != nil {
		t.Fatal(err)
	}
	first := serviceTestMediaView(t, repos, rows[0].ID)
	second := serviceTestMediaView(t, repos, rows[1].ID)
	if first.Title != "第一集" || second.Title != "第二集" || first.MetadataID == "" || second.MetadataID == "" || first.MetadataID == second.MetadataID {
		t.Fatalf("episode metadata was not persisted independently: first=%#v second=%#v", first, second)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
	}
}

func TestNFOOnlyLibraryRejectsProviderManualMatching(t *testing.T) {
	scraper, repos, providerCalls := newNFOOnlyTestScraper(t)
	library := model.Library{Name: "个人短片", Path: t.TempDir(), Type: model.LibraryTypeNFOMovie, Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "短片", Path: filepath.Join(library.Path, "短片.mkv")}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := scraper.ManualSearch(t.Context(), &media, "短片", "tmdb", "movie"); err == nil {
		t.Fatal("NFO-only manual search should be rejected")
	}
	if _, err := scraper.ApplyManualMatch(t.Context(), media.ID, ManualScrapeRequest{Source: "manual", Title: "短片"}); err == nil {
		t.Fatal("NFO-only manual apply should be rejected")
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
	}
}

func TestNFOOnlyScannerDoesNotMergePathHint(t *testing.T) {
	_, repos, _ := newNFOOnlyTestScraper(t)
	root := t.TempDir()
	mediaPath := filepath.Join(root, "个人短片 {tmdb-27205}.mkv")
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<movie><title>个人短片</title><uniqueid type="tmdb">999</uniqueid></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "个人短片", Path: root, Type: model.LibraryTypeNFOMovie, Enabled: true}
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	local := scanner.readLocalScanMetadata(&library, nil, mediaPath, 0, 0)
	if local == nil || !local.HasNFO || local.TMDbID != 999 {
		t.Fatalf("scanner local metadata = %#v, want NFO ID without path hint override", local)
	}
	media := scanner.buildLocalScanMedia(localScanMediaInput{lib: &library, path: mediaPath, ext: ".mkv", localMeta: local})
	if media.TMDbID != 0 || media.LocalMetadataHint == "" {
		t.Fatalf("scanner media = %#v, want local hint without canonical lookup ID", media)
	}
}

func newNFOOnlyTestScraper(t *testing.T) (*ScraperService, *repository.Container, *atomic.Int32) {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateScraperTestModels(t, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "provider must not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	log := zap.NewNop()
	scraper := NewScraperService(cfg, log, repos, NewTMDbProvider(cfg, log, nil), nil, nil, nil, NewHub(log))
	return scraper, repos, &calls
}
