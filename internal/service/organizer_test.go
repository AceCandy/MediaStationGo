package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestOrganizeMediaReDetectsSeasonFromPath(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "Incoming", "Some Show", "Season 02")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "Some Show - EP03.mkv")
	if err := os.WriteFile(source, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Some Show",
		Path:         source,
		Container:    "mkv",
		SeasonNum:    1,
		EpisodeNum:   3,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	dst, err := organizer.OrganizeMedia(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Some Show", "Season 02", "Some Show - S02E03.mkv")
	if dst != want {
		t.Fatalf("dst = %q, want %q", dst, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("organized file missing: %v", err)
	}
	var refreshed model.Media
	if err := db.First(&refreshed, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshed.SeasonNum != 2 || refreshed.EpisodeNum != 3 || refreshed.Path != want {
		t.Fatalf("unexpected refreshed media: %+v", refreshed)
	}
}

func TestOrganizeMediaUsesEpisodeNFOSeason(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "Incoming", "Some Show")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "Some Show - EP03.mkv")
	if err := os.WriteFile(source, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := `<episodedetails><title>第三集</title><season>2</season><episode>3</episode></episodedetails>`
	if err := os.WriteFile(nfoPath(source), []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "Some Show",
		Path:         source,
		Container:    "mkv",
		SeasonNum:    1,
		EpisodeNum:   3,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	dst, err := organizer.OrganizeMedia(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Some Show", "Season 02", "Some Show - S02E03.mkv")
	if dst != want {
		t.Fatalf("dst = %q, want %q", dst, want)
	}
}
