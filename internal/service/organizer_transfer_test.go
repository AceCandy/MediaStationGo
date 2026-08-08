package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newOrganizerTestRepo(t *testing.T) *repository.Container {
	t.Helper()
	return repository.New(newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.AccessLog{}))
}

func TestOrganizeMediaHonorsTargetDirAndCopyMode(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(srcDir, "Some Movie.mkv")
	if err := os.WriteFile(source, []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "custom-library")

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organize.target_dir", target); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Movies", Path: filepath.Join(root, "lib"), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "Some Movie", Path: source, Year: 2020, Container: "mkv", ScrapeStatus: "matched"}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	dst, err := org.OrganizeMediaWithOptions(t.Context(), media.ID, OrganizeOptions{TransferMode: TransferCopy})
	if err != nil {
		t.Fatalf("organize: %v", err)
	}
	if !strings.HasPrefix(dst, target) {
		t.Fatalf("dst %q should be under target dir %q", dst, target)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("copy mode must keep source: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("organized file missing: %v", err)
	}
}
