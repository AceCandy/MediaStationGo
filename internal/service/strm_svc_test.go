package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestGenerateSTRMForLibraryWritesFilesAndRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.STRMRecord{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "电影", Path: "cloud://openlist/电影", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{Base: model.Base{ID: "cloud-media"}, LibraryID: lib.ID, Title: "云盘电影", Year: 2026, Path: "cloud://openlist/电影/云盘电影.mkv", STRMURL: "/api/cloud/play/openlist?ref=movie"},
		{Base: model.Base{ID: "local-media"}, LibraryID: lib.ID, Title: "本地电影", Year: 2025, Path: filepath.Join(t.TempDir(), "本地电影.mkv")},
	}
	for i := range rows {
		if err := repos.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	outDir := filepath.Join(t.TempDir(), "strm")
	svc := NewSTRMService(zap.NewNop(), repos, &config.Config{})

	res, err := svc.GenerateForLibrary(t.Context(), GenerateSTRMOptions{
		LibraryID:    lib.ID,
		OutputDir:    outDir,
		BaseURL:      "http://nas.example:18080",
		IncludeLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 2 || res.Skipped != 0 {
		t.Fatalf("result = %#v, want generated=2 skipped=0", res)
	}
	cloudSTRM := filepath.Join(outDir, "云盘电影 (2026)", "云盘电影 (2026).strm")
	localSTRM := filepath.Join(outDir, "本地电影 (2025)", "本地电影 (2025).strm")
	assertFileContains(t, cloudSTRM, "http://nas.example:18080/api/cloud/play/openlist?ref=movie")
	assertFileContains(t, localSTRM, "http://nas.example:18080/api/stream/local-media")

	var count int64
	if err := repos.DB.Model(&model.STRMRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("strm record count = %d, want 2", count)
	}

	res, err = svc.GenerateForLibrary(t.Context(), GenerateSTRMOptions{
		LibraryID:    lib.ID,
		OutputDir:    outDir,
		BaseURL:      "http://nas.example:18080",
		IncludeLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 2 {
		t.Fatalf("second run skipped = %d, want 2", res.Skipped)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
