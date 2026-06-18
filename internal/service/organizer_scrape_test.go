package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if len(scans) != 1 || scans[0].Added != 1 {
		t.Fatalf("scans = %#v, want one scan with added=1", scans)
	}
	if len(scrapes) != 1 || scrapes[0].Matched != 1 || scrapes[0].Error != "" || scrapes[0].Skipped {
		t.Fatalf("scrapes = %#v, want one successful matched scrape", scrapes)
	}

	var media model.Media
	if err := repos.DB.Where("path LIKE ?", "%Spy Family - S01E01.mkv").First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.ScrapeStatus != "matched" || media.TMDbID != 12345 {
		t.Fatalf("media scrape status=%q tmdb=%d, want matched/12345", media.ScrapeStatus, media.TMDbID)
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
	sourceFile := filepath.Join(src, "Spy.x.Family.S01E01.2022.1080p.mkv")
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
}

func TestOrganizeDirectoryUsesAdultMetadataBeforeRename(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<a class="box" href="/v/ssis001"><strong>SSIS-001 整理候选</strong></a>`))
		case "/v/ssis001":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<h2 class="title"><strong>SSIS-001 整理成人标题</strong></h2><div>日期 2024-01-02</div>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	if err := repos.DB.AutoMigrate(&model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	apiConfig := NewAPIConfigService(zap.NewNop(), repos, NewCryptoService("", zap.NewNop()))
	baseURL := upstream.URL
	if _, err := apiConfig.Update(t.Context(), "adult", APIConfigPatch{BaseURL: &baseURL}); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scraper := NewScraperService(&config.Config{}, log, repos, nil, nil, nil, nil, NewHub(log), NewAdultProvider(log, apiConfig))

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "SSIS-001.1080p.mkv")
	writeOrgFile(t, sourceFile, "adult")

	organizer := NewOrganizerService(&config.Config{}, log, repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		MediaType:    "adult",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("organize adult directory: %v", err)
	}
	if res.Organized != 1 || len(res.Items) != 1 {
		t.Fatalf("result = %+v, want one organized preview", res)
	}
	if res.Items[0].Title != "整理成人标题" || res.Items[0].MediaType != "adult" {
		t.Fatalf("adult organize item = %+v", res.Items[0])
	}
	wantSuffix := filepath.Join("成人", "整理成人标题 (2024)", "整理成人标题 (2024).mkv")
	if !strings.Contains(res.Items[0].Target, wantSuffix) {
		t.Fatalf("adult target = %q, want suffix %q", res.Items[0].Target, wantSuffix)
	}
}

func TestOrganizeDirectoryClassifiesScraperMatchBeforeRename(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search/movie":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":                45745,
					"title":             "寻龙记",
					"original_title":    "Sintel",
					"original_language": "en",
					"genre_ids":         []int{16, 14},
					"release_date":      "2010-09-27",
					"vote_average":      7.4,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Organizer.SmartClassify = true
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, NewHub(zap.NewNop()))

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Sintel.2010.1080p.CodexVerify.mp4")
	writeOrgFile(t, sourceFile, "movie")

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		MediaType:    "movie",
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(dest, "电影", "动画电影", "寻龙记 (2010)", "寻龙记 (2010).mp4")
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1; items=%#v errors=%#v", res.Organized, res.Items, res.Errors)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("organized movie should use metadata category path %q: %v; items=%#v", want, err, res.Items)
	}
	if len(res.Items) != 1 || res.Items[0].Category != "动画电影" {
		t.Fatalf("organize category = %#v, want 动画电影", res.Items)
	}
}

func TestOrganizeDirectoryMetadataCategoryOverridesDownloadFolder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":                12345,
					"name":              "间谍过家家",
					"original_name":     "SPY×FAMILY",
					"original_language": "ja",
					"origin_country":    []string{"JP"},
					"genre_ids":         []int{16, 35},
					"first_air_date":    "2022-04-09",
					"vote_average":      8.6,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Organizer.SmartClassify = true
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, NewHub(zap.NewNop()))

	root := t.TempDir()
	srcRoot := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(srcRoot, "国产剧", "Spy.x.Family.S01E01.2022.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   srcRoot,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(dest, "动漫", "日番", "间谍过家家", "Season 01", "间谍过家家 - S01E01.mkv")
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1; items=%#v errors=%#v", res.Organized, res.Items, res.Errors)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("metadata category should override wrong source folder at %q: %v; items=%#v", want, err, res.Items)
	}
	if len(res.Items) != 1 || res.Items[0].Category != "日番" || res.Items[0].MediaType != "anime" {
		t.Fatalf("organize metadata category/type = %#v, want 日番/anime", res.Items)
	}
}

func TestOrganizeDirectoryDoesNotScrapeByDownloadCategoryFolder(t *testing.T) {
	var queries []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search/tv" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query().Get("query")
		queries = append(queries, query)
		switch {
		case query == "国产剧":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":             843248,
					"name":           "高达 G之复国运动 剧场版III 来自宇宙的遗产",
					"first_air_date": "2021-07-22",
				}},
			})
		case strings.EqualFold(query, "ashes to crown"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":                289271,
					"name":              "翘楚",
					"original_name":     "Ashes to Crown",
					"original_language": "zh",
					"origin_country":    []string{"CN"},
					"genre_ids":         []int{18},
					"first_air_date":    "2026-06-01",
				}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		}
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Organizer.SmartClassify = true
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, NewHub(zap.NewNop()))

	root := t.TempDir()
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(root, "downloads", "国产剧", "Ashes.to.Crown.S01.1080p.YOUKU.WEB-DL.AAC2.0.H.264-MWeb", "Ashes.to.Crown.S01E06.1080p.YOUKU.WEB-DL.AAC2.0.H.264-MWeb.mkv")
	writeOrgFile(t, sourceFile, "episode")

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:           sourceFile,
		DestPath:             dest,
		TransferMode:         TransferCopy,
		MediaType:            "tv",
		MediaCategory:        "国产剧",
		AllowReplaceExisting: false,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(dest, "电视剧", "国产剧", "翘楚", "Season 01", "翘楚 - S01E06.mkv")
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1; items=%#v errors=%#v queries=%#v", res.Organized, res.Items, res.Errors, queries)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("organized file should use release title metadata, not category query, at %q: %v; items=%#v queries=%#v", want, err, res.Items, queries)
	}
	if len(queries) == 0 || queries[0] == "国产剧" {
		t.Fatalf("first scrape query = %#v, want release title before category folder", queries)
	}
	if len(res.Items) != 1 || res.Items[0].Title != "翘楚" || res.Items[0].Category != "国产剧" {
		t.Fatalf("organize item = %#v, want title 翘楚 in category 国产剧", res.Items)
	}
}

func TestOrganizeDirectoryEpisodeMarkerOverridesMovieSourceFolder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":                100088,
					"name":              "The Last of Us",
					"original_language": "en",
					"origin_country":    []string{"US"},
					"genre_ids":         []int{18},
					"first_air_date":    "2023-01-15",
					"vote_average":      8.7,
				}},
			})
		case r.URL.Path == "/search/movie":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":           999,
					"title":        "Wrong Movie",
					"release_date": "2023-01-01",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Organizer.SmartClassify = true
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, NewHub(zap.NewNop()))

	root := t.TempDir()
	srcRoot := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(srcRoot, "外语电影", "The.Last.of.Us.S01E01.2023.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   srcRoot,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(dest, "电视剧", "欧美剧", "The Last of Us", "Season 01", "The Last of Us - S01E01.mkv")
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1; items=%#v errors=%#v", res.Organized, res.Items, res.Errors)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("episode marker should force TV organize path %q: %v; items=%#v", want, err, res.Items)
	}
	if len(res.Items) != 1 || res.Items[0].Category != "欧美剧" || res.Items[0].MediaType != "tv" {
		t.Fatalf("organize episode category/type = %#v, want 欧美剧/tv", res.Items)
	}
}

func TestOrganizeDirectoryRejectsWrongYearScraperRename(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":             999,
					"name":           "Parade of Stars Auto Show",
					"first_air_date": "1952-01-01",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, NewHub(zap.NewNop()))

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Auto.Show.S01E03.2026.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")
	secondSourceFile := filepath.Join(src, "Auto.Show.S01E04.2026.1080p.mkv")
	writeOrgFile(t, secondSourceFile, "episode")

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
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

	rejected := filepath.Join(dest, "电视剧", "Parade of Stars Auto Show", "Season 01", "Parade of Stars Auto Show - S01E03.mkv")
	if _, err := os.Stat(rejected); err == nil {
		t.Fatalf("wrong-year metadata match should not rename to %q", rejected)
	}
	want := filepath.Join(dest, "电视剧", "Auto Show", "Season 01", "Auto Show - S01E03.mkv")
	if res.Organized != 2 {
		t.Fatalf("organized = %d, want 2; items=%#v errors=%#v", res.Organized, res.Items, res.Errors)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("organize should keep parsed title at %q: %v; items=%#v", want, err, res.Items)
	}
	secondWant := filepath.Join(dest, "电视剧", "Auto Show", "Season 01", "Auto Show - S01E04.mkv")
	if _, err := os.Stat(secondWant); err != nil {
		t.Fatalf("organize should not reuse rejected cached match at %q: %v; items=%#v", secondWant, err, res.Items)
	}
}

func TestOrganizeDirectoryDedupsByExternalIDBeforeRename(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Spy.x.Family.S01E01.2022.2160p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	existingPath := filepath.Join(dest, "电视剧", "旧错误名", "Season 01", "旧错误名 - S01E01.mkv")
	writeOrgFile(t, existingPath, "existing")
	lib := model.Library{Name: "剧集", Path: filepath.Join(dest, "电视剧"), Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID:    lib.ID,
		Title:        "旧错误名",
		Path:         existingPath,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}

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
	if res.Organized != 0 || res.Skipped != 1 {
		t.Fatalf("organize result = organized %d skipped %d, want 0/1; items=%#v errors=%#v", res.Organized, res.Skipped, res.Items, res.Errors)
	}
	if len(res.Items) != 1 || res.Items[0].Reason != organizeSkipDuplicateLibrary {
		t.Fatalf("source should be skipped as external-id duplicate: %#v", res.Items)
	}
	if _, err := os.Stat(sourceFile); err != nil {
		t.Fatalf("duplicate source should remain untouched: %v", err)
	}
}

func TestOrganizeDirectoryUsesBangumiForAnimeRename(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/subject/frieren" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": 1,
			"list": []map[string]any{{
				"id":       889,
				"name":     "Frieren",
				"name_cn":  "葬送的芙莉莲",
				"air_date": "2023-09-29",
			}},
		})
	}))
	defer upstream.Close()

	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	bangumi := NewBangumiProvider(cfg, zap.NewNop())
	bangumi.base = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, nil, bangumi, nil, nil, NewHub(zap.NewNop()))

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Frieren.S01E01.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		MediaType:    "anime",
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(dest, "动漫", "葬送的芙莉莲", "Season 01", "葬送的芙莉莲 - S01E01.mkv")
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1; items=%#v errors=%#v", res.Organized, res.Items, res.Errors)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("organized file should use Bangumi metadata path %q: %v", want, err)
	}
}

func TestOrganizeScanAndScrapeRetriesNoMatchRows(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	root := t.TempDir()
	libRoot := filepath.Join(root, "media", "电视剧")
	mediaPath := filepath.Join(libRoot, "间谍过家家", "Season 02", "间谍过家家 - S02E02.mkv")
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
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, scraper)
	_, scrapes := scanner.ScanAndScrapeLibrariesForPath(t.Context(), filepath.Join(root, "media"), "", true)
	if len(scrapes) != 1 || scrapes[0].Matched != 1 || scrapes[0].Error != "" || scrapes[0].Skipped {
		t.Fatalf("scrapes = %#v, want one retried no_match row", scrapes)
	}

	var got model.Media
	if err := repos.DB.First(&got, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "matched" || got.TMDbID != 12345 {
		t.Fatalf("media scrape status=%q tmdb=%d, want matched/12345", got.ScrapeStatus, got.TMDbID)
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
