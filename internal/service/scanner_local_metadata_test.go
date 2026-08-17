package service

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestScanLibraryUsesLocalMetadata(t *testing.T) {
	root := t.TempDir()
	seasonDir := filepath.Join(root, "Show", "Season 02")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "Show - EP03.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Show", "tvshow.nfo"), []byte(`<tvshow><title>本地剧名</title><year>2025</year></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<episodedetails><title>本地第三集</title><season>2</season><episode>3</episode></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.LocalMetadata != 1 {
		t.Fatalf("LocalMetadata = %d, want 1", res.LocalMetadata)
	}
	if res.Added != 1 || res.Updated != 0 {
		t.Fatalf("scan counts added=%d updated=%d, want 1/0", res.Added, res.Updated)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if media.SeasonNum != 2 || media.EpisodeNum != 3 || media.ScrapeStatus != "pending" || media.MetadataID == "" {
		t.Fatalf("unexpected scanned media: %+v", media)
	}
	local := serviceTestLocalMetadataHint(t, media)
	if !local.HasNFO || local.Title != "本地剧名" || local.EpisodeTitle != "本地第三集" || local.Year != 2025 {
		t.Fatalf("unexpected local metadata hint: %+v", local)
	}

	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Updated != 0 || res.Skipped != 1 {
		t.Fatalf("repeat scan counts added=%d updated=%d skipped=%d, want 0/0/1", res.Added, res.Updated, res.Skipped)
	}
}

func TestScanLibraryDoesNotMarkArtworkOnlyAsMatched(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "SSIS-001-CD1.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "SSIS-001-poster.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "Adult", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.LocalMetadata != 1 {
		t.Fatalf("LocalMetadata = %d, want 1", res.LocalMetadata)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	local := serviceTestLocalMetadataHint(t, media)
	if !local.HasArtwork || local.PosterURL != poster || media.MetadataID == "" || media.ScrapeStatus != "pending" {
		t.Fatalf("unexpected artwork-only scan state: media=%+v hint=%+v", media, local)
	}
}

func TestScanLibraryRefreshesArtworkOnlyMetadata(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPoster := filepath.Join(root, "Movie-thumb.jpg")
	if err := os.WriteFile(oldPoster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	if _, err := scanner.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}

	newPoster := filepath.Join(root, "poster.jpg")
	if err := os.WriteFile(newPoster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}

	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	local := serviceTestLocalMetadataHint(t, media)
	if local.PosterURL != newPoster || media.MetadataID == "" || media.ScrapeStatus != "pending" {
		t.Fatalf("unexpected refreshed artwork hint: media=%+v hint=%+v", media, local)
	}
}

func TestScanLibraryParsesEpisodesForMovieTypedLibrary(t *testing.T) {
	root := t.TempDir()
	seasonDir := filepath.Join(root, "哈哈哈哈哈 (2020)", "Season 06")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "哈哈哈哈哈 - S06E17 - 第 17 集.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "综艺", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	if _, err := scanner.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if media.SeasonNum != 6 || media.EpisodeNum != 17 {
		t.Fatalf("season/episode = %d/%d, want 6/17", media.SeasonNum, media.EpisodeNum)
	}
}

func TestScanLibraryRefreshesStaleNoMatchDerivedMetadata(t *testing.T) {
	root := t.TempDir()
	seasonDir := filepath.Join(root, "Hntv Spring Festival Gala S01e (2026)", "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "Hntv Spring Festival Gala S01e - S01E202-DD5.QHstudIo.6.4K - 第 202 集.ts")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "综艺", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID:    lib.ID,
		Title:        "hntv spring festival gala s01e",
		Path:         mediaPath,
		SizeBytes:    1,
		ScrapeStatus: "no_match",
	}).Error; err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 1 || res.Skipped != 0 {
		t.Fatalf("scan counts updated=%d skipped=%d, want 1/0", res.Updated, res.Skipped)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if media.Title != "hntv spring festival gala" || media.SeasonNum != 1 || media.EpisodeNum != 202 || media.ScrapeStatus != "pending" {
		t.Fatalf("stale no_match row was not refreshed: title=%q s=%d e=%d status=%q", media.Title, media.SeasonNum, media.EpisodeNum, media.ScrapeStatus)
	}
}

func TestScanLibraryPrunesMissingMedia(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Show.S02E03.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	stale := model.Media{
		LibraryID:    lib.ID,
		Title:        "Show",
		Path:         filepath.Join(root, "old", "Show.S02E03.mkv"),
		SizeBytes:    123,
		ScrapeStatus: "pending",
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Fatalf("Removed = %d, want 1", res.Removed)
	}
	if len(res.Changes) != 2 || res.Changes[1].Action != ScanChangeRemoved || res.Changes[1].Path != stale.Path {
		t.Fatalf("changes = %#v, want removed path %q", res.Changes, stale.Path)
	}
	var count int64
	if err := db.Model(&model.Media{}).Where("library_id = ?", lib.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("media count = %d, want 1", count)
	}
}
