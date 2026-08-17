package service

import (
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestExistingLocalMediaSnapshotFiltersAndCleansLocalRows(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{})
	repos := repository.New(db)
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)

	rawPath := filepath.Join("D:", "media", "Movies", "..", "Movie.mkv")
	cleanPath := filepath.Clean(rawPath)
	if err := db.Create(&[]model.Media{
		{
			LibraryID:         "lib-1",
			Path:              rawPath,
			ScanFileSizeBytes: 4096,
			ScanFileMTimeNS:   123456789,
			FileID:            "dev:inode",
		},
		{LibraryID: "lib-1", Path: "cloud://openlist/Movie.mkv", SizeBytes: 99},
		{LibraryID: "lib-2", Path: filepath.Join("D:", "media", "Other.mkv"), SizeBytes: 88},
	}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := scanner.existingLocalMediaSnapshot(t.Context(), "lib-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("snapshot len = %d, want 1: %#v", len(got), got)
	}
	row, ok := got[cleanPath]
	if !ok {
		t.Fatalf("snapshot key %q not found in %#v", cleanPath, got)
	}
	if row.ScanFileSizeBytes != 4096 || row.ScanFileMTimeNS != 123456789 || row.FileID != "dev:inode" {
		t.Fatalf("fingerprint fields not preserved: %#v", row)
	}
}
