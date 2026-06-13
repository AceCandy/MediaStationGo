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

func TestResolveAccessibleLibraryPathMapsConfiguredHostMediaDir(t *testing.T) {
	root := t.TempDir()
	hostRoot := filepath.Join(root, "nas", "host", "media")
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", hostRoot)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	got, err := resolveAccessibleLibraryPath(filepath.Join(hostRoot, "电视剧", "国产剧"))
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestResolveAccessibleLibraryPathKeepsAccessibleContainerPath(t *testing.T) {
	containerLibrary := filepath.Join(t.TempDir(), "media", "电影")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAccessibleLibraryPath(containerLibrary)
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestMappedPathCandidatesMapWindowsDriveDownloadMarker(t *testing.T) {
	root := t.TempDir()
	containerDownloads := filepath.Join(root, "container", "downloads")
	containerLibrary := filepath.Join(containerDownloads, "国产剧")
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", containerDownloads)

	want := filepath.Clean(containerLibrary)
	for _, got := range mappedPathCandidates(`F:\downloads\国产剧`) {
		if got == want {
			return
		}
	}
	t.Fatalf("mappedPathCandidates() missing %q", want)
}

func TestDeleteCloudLibraryPurgesMountWithoutRecycleBin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "OpenList · 剑来", Path: "cloud://openlist/Anime/JianLai", Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(t.Context(), &model.Media{
		LibraryID: lib.ID,
		Title:     "剑来",
		Path:      "cloud://openlist/Anime/JianLai/Season 1/01.mkv",
		STRMURL:   "/api/cloud/play/openlist?ref=/Anime/JianLai/Season%201/01.mkv",
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	if err := svc.DeleteLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}

	var mediaCount int64
	if err := db.Unscoped().Model(&model.Media{}).Where("library_id = ?", lib.ID).Count(&mediaCount).Error; err != nil {
		t.Fatal(err)
	}
	if mediaCount != 0 {
		t.Fatalf("cloud mount media should be purged, count=%d", mediaCount)
	}
	recycle, err := svc.ListRecycleBin(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recycle) != 0 {
		t.Fatalf("cloud mount removal must not populate recycle bin: %#v", recycle)
	}
	var libCount int64
	if err := db.Unscoped().Model(&model.Library{}).Where("id = ?", lib.ID).Count(&libCount).Error; err != nil {
		t.Fatal(err)
	}
	if libCount != 0 {
		t.Fatalf("cloud mount library should be purged, count=%d", libCount)
	}
}

func TestSoftDeleteCloudMediaPurgesRecordWithoutRecycleBin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	media := model.Media{
		Base:    model.Base{ID: "cloud-media"},
		Title:   "网盘电影",
		Path:    "cloud://openlist/电影/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=/电影/Movie.mkv",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	if err := svc.SoftDelete(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Unscoped().Model(&model.Media{}).Where("id = ?", media.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cloud media should be purged, count=%d", count)
	}
	recycle, err := svc.ListRecycleBin(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recycle) != 0 {
		t.Fatalf("cloud media removal must not populate recycle bin: %#v", recycle)
	}
}
