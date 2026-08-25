package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestAutoGenerateSTRMAfterScanUsesAllScopeRoot(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.STRMRecord{}, &model.Setting{})
	repos := repository.New(db)
	outDir := t.TempDir()
	if err := repos.Setting.Set(t.Context(), "strm.auto_generate_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "strm.output_dir", outDir); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "strm.output_scope", "all"); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{
		Base:    model.Base{ID: "tv-lib"},
		Name:    "欧美剧",
		Path:    filepath.Join(t.TempDir(), "电视剧", "欧美剧"),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		PermanentBase: model.PermanentBase{ID: "show-1"},
		LibraryID:     lib.ID,
		Title:         "第一集",
		Path:          filepath.Join(lib.Path, "Show", "Season 01", "Show.S01E01.mkv"),
		SeasonNum:     1,
		EpisodeNum:    1,
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	scanner.maybeGenerateSTRMAfterScan(lib.ID)

	want := filepath.Join(outDir, "电视剧", "欧美剧", "Show", "Season 01", "Show - S01E01.strm")
	waitForFile(t, want)
	assertFileContains(t, want, "/api/stream/show-1")
	if _, err := os.Stat(filepath.Join(outDir, "电视剧", "欧美剧", "电视剧", "欧美剧")); !os.IsNotExist(err) {
		t.Fatalf("auto STRM output was nested twice")
	}
}

func TestAutoGenerateSTRMSkipsExistingSTRMSource(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.STRMRecord{}, &model.Setting{})
	repos := repository.New(db)
	outDir := filepath.Join(t.TempDir(), "电影")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(outDir, "remote.strm")
	if err := os.WriteFile(source, []byte("https://cdn.example.test/remote.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}

	lib := model.Library{Name: "电影", Path: outDir, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		PermanentBase: model.PermanentBase{ID: "remote-media"},
		LibraryID:     lib.ID,
		Title:         "Remote",
		Path:          source,
		Container:     "strm",
		STRMURL:       "https://cdn.example.test/remote.mkv",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewSTRMService(zap.NewNop(), repos, &config.Config{})
	res, err := svc.GenerateForLibrary(t.Context(), GenerateSTRMOptions{
		LibraryID:      lib.ID,
		OutputDir:      outDir,
		BaseURL:        "http://nas.example:18080",
		IncludeLocal:   true,
		Overwrite:      true,
		SkipSTRMSource: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || len(res.Items) != 1 || res.Items[0].Reason != "source already strm" {
		t.Fatalf("result = %#v, want existing STRM source skipped", res)
	}
	if _, err := os.Stat(filepath.Join(outDir, "Remote", "Remote.strm")); !os.IsNotExist(err) {
		t.Fatalf("nested STRM should not be generated, stat err=%v", err)
	}
	after, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("source STRM mtime changed: before=%s after=%s", before.ModTime(), after.ModTime())
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
