package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestOrganizeDirectoryReclassifiesExistingWrongCategoryMedia(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search/tv" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"id":                292696,
				"name":              "莫离",
				"original_name":     "The First Jasmine",
				"original_language": "zh",
				"origin_country":    []string{"CN"},
				"genre_ids":         []int{18},
				"first_air_date":    "2026-06-23",
			}},
		})
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
	sourceFile := filepath.Join(srcRoot, "欧美剧", "The.First.Jasmine.S01.1080p.TX.WEB-DL.AAC2.0.H.264-MWeb", "The.First.Jasmine.S01E01.1080p.TX.WEB-DL.AAC2.0.H.264-MWeb.mkv")
	writeOrgFile(t, sourceFile, "episode")

	euusLib := model.Library{Name: "欧美剧", Path: filepath.Join(dest, "电视剧", "欧美剧"), Type: "tv", Enabled: true}
	domesticLib := model.Library{Name: "国产剧", Path: filepath.Join(dest, "电视剧", "国产剧"), Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &euusLib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &domesticLib); err != nil {
		t.Fatal(err)
	}

	wrongPath := filepath.Join(euusLib.Path, "The First Jasmine", "Season 01", "The First Jasmine - S01E01.mkv")
	writeOrgFile(t, wrongPath, "existing")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    euusLib.ID,
		Title:        "莫离",
		OriginalName: "The First Jasmine",
		Path:         wrongPath,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       292696,
		Languages:    "zh",
		Countries:    "CN",
		Genres:       "剧情",
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   srcRoot,
		DestPath:     euusLib.Path,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(domesticLib.Path, "莫离", "Season 01", "莫离 - S01E01.mkv")
	if res.Reclassified != 1 || res.Organized != 0 || res.Skipped != 0 {
		t.Fatalf("result = %+v, want reclassified=1 only", res)
	}
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Fatalf("wrong category path should be moved away, stat err=%v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("reclassified media missing at %q: %v; items=%#v", want, err, res.Items)
	}
	if _, err := os.Stat(sourceFile); err != nil {
		t.Fatalf("source download should remain untouched: %v", err)
	}

	var got model.Media
	if err := repos.DB.First(&got, "path = ?", want).Error; err != nil {
		t.Fatal(err)
	}
	if got.LibraryID != domesticLib.ID {
		t.Fatalf("library_id = %q, want domestic library %q", got.LibraryID, domesticLib.ID)
	}
}

func TestOrganizeDirectoryReclassifiesScannedAnimeUsingDBMetadata(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Organizer.SmartClassify = true

	root := t.TempDir()
	dest := filepath.Join(root, "media")
	euusLib := model.Library{Name: "欧美剧", Path: filepath.Join(dest, "电视剧", "欧美剧"), Type: "tv", Enabled: true}
	tvAnimeLib := model.Library{Name: "国漫", Path: filepath.Join(dest, "电视剧", "国漫"), Type: "tv", Enabled: true}
	animeLib := model.Library{Name: "国漫", Path: filepath.Join(dest, "动漫", "国漫"), Type: "anime", Enabled: true}
	for _, lib := range []*model.Library{&euusLib, &tvAnimeLib, &animeLib} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}

	wrongPath := filepath.Join(euusLib.Path, "Blades Of The Guardians", "Season 2", "Blades Of The Guardians - S02E01-1080p.TX.WEB-DL.mkv")
	writeOrgFile(t, wrongPath, "episode")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    euusLib.ID,
		Title:        "镖人",
		OriginalName: "Blades Of The Guardians",
		Path:         wrongPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		TMDbID:       107463,
		Languages:    "zh",
		Countries:    "CN",
		Genres:       "动画,动作冒险",
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   filepath.Join(euusLib.Path, "Blades Of The Guardians"),
		DestPath:     euusLib.Path,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	want := filepath.Join(animeLib.Path, "镖人", "Season 02", "镖人 - S02E01.mkv")
	if res.Reclassified != 1 || res.Organized != 0 {
		t.Fatalf("result = %+v, want scanned DB metadata reclassified only", res)
	}
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Fatalf("wrong anime path should be moved away, stat err=%v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("anime should move to physical anime library at %q: %v; items=%#v", want, err, res.Items)
	}
	var got model.Media
	if err := repos.DB.First(&got, "path = ?", want).Error; err != nil {
		t.Fatal(err)
	}
	if got.LibraryID != animeLib.ID {
		t.Fatalf("library_id = %q, want anime library %q", got.LibraryID, animeLib.ID)
	}
}

func TestReclassifyMisclassifiedMediaMovesScannedAnimeToPhysicalAnimeLibrary(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	cfg := &config.Config{}
	cfg.Organizer.SmartClassify = true

	root := t.TempDir()
	dest := filepath.Join(root, "media")
	euusLib := model.Library{Name: "欧美剧", Path: filepath.Join(dest, "电视剧", "欧美剧"), Type: "tv", Enabled: true}
	tvAnimeLib := model.Library{Name: "国漫", Path: filepath.Join(dest, "电视剧", "国漫"), Type: "tv", Enabled: true}
	animeLib := model.Library{Name: "国漫", Path: filepath.Join(dest, "动漫", "国漫"), Type: "anime", Enabled: true}
	for _, lib := range []*model.Library{&euusLib, &tvAnimeLib, &animeLib} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}

	wrongPath := filepath.Join(euusLib.Path, "Blades Of The Guardians", "Season 2", "Blades Of The Guardians - S02E01.mkv")
	writeOrgFile(t, wrongPath, "episode")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    euusLib.ID,
		Title:        "镖人",
		OriginalName: "Blades Of The Guardians",
		Path:         wrongPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		TMDbID:       107463,
		Languages:    "zh",
		Countries:    "CN",
		Genres:       "动画,动作冒险",
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	res, err := organizer.ReclassifyMisclassifiedMedia(t.Context(), MediaCategoryReclassifyOptions{})
	if err != nil {
		t.Fatalf("reclassify media: %v", err)
	}
	want := filepath.Join(animeLib.Path, "镖人", "Season 02", "镖人 - S02E01.mkv")
	if res.Reclassified != 1 {
		t.Fatalf("reclassified = %d, want 1; items=%#v errors=%#v", res.Reclassified, res.Items, res.Errors)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("bulk reclassify target missing at %q: %v", want, err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "path = ?", want).Error; err != nil {
		t.Fatal(err)
	}
	if got.LibraryID != animeLib.ID {
		t.Fatalf("library_id = %q, want anime library %q", got.LibraryID, animeLib.ID)
	}
}

func TestOrganizeDirectoryCleansWrongCategoryDuplicateWhenTargetExists(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search/tv" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"id":                292696,
				"name":              "莫离",
				"original_name":     "The First Jasmine",
				"original_language": "zh",
				"origin_country":    []string{"CN"},
				"genre_ids":         []int{18},
				"first_air_date":    "2026-06-23",
			}},
		})
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
	sourceFile := filepath.Join(srcRoot, "欧美剧", "The.First.Jasmine.S01.1080p.TX.WEB-DL.AAC2.0.H.264-MWeb", "The.First.Jasmine.S01E01.1080p.TX.WEB-DL.AAC2.0.H.264-MWeb.mkv")
	writeOrgFile(t, sourceFile, "episode")

	euusLib := model.Library{Name: "欧美剧", Path: filepath.Join(dest, "电视剧", "欧美剧"), Type: "tv", Enabled: true}
	domesticLib := model.Library{Name: "国产剧", Path: filepath.Join(dest, "电视剧", "国产剧"), Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &euusLib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &domesticLib); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(domesticLib.Path, "莫离", "Season 01", "莫离 - S01E01.mkv")
	wrongPath := filepath.Join(euusLib.Path, "The First Jasmine", "Season 01", "The First Jasmine - S01E01.mkv")
	writeOrgFile(t, targetPath, "same-bytes")
	writeOrgFile(t, wrongPath, "same-bytes")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    domesticLib.ID,
		Title:        "莫离",
		Path:         targetPath,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       292696,
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID:    euusLib.ID,
		Title:        "莫离",
		Path:         wrongPath,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       292696,
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(cfg, zap.NewNop(), repos)
	organizer.SetScraper(scraper)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   srcRoot,
		DestPath:     euusLib.Path,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Reclassified != 1 || res.Organized != 0 || res.Skipped != 0 {
		t.Fatalf("result = %+v, want reclassified=1 only", res)
	}
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("canonical target should remain: %v", err)
	}
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Fatalf("wrong category duplicate should be removed, stat err=%v", err)
	}
	var wrongRows int64
	if err := repos.DB.Model(&model.Media{}).Where("path = ?", wrongPath).Count(&wrongRows).Error; err != nil {
		t.Fatal(err)
	}
	if wrongRows != 0 {
		t.Fatalf("wrong DB rows = %d, want 0", wrongRows)
	}
	if _, err := os.Stat(sourceFile); err != nil {
		t.Fatalf("source download should remain untouched: %v", err)
	}
}
