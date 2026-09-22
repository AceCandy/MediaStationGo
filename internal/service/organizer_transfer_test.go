package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestOrganizeDatabaseFailureRestoresTransfer(t *testing.T) {
	for _, mode := range []TransferMode{TransferMove, TransferCopy, TransferHardlink, TransferSymlink} {
		t.Run(string(mode), func(t *testing.T) {
			repos := newOrganizerTestRepo(t)
			dir := t.TempDir()
			src := writeTemp(t, dir, "source", "payload")
			dst := filepath.Join(dir, "target")
			media := model.Media{Path: src}
			if err := repos.Media.Upsert(t.Context(), &media); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("injected database failure")
			if err := repos.DB.Callback().Update().Before("gorm:update").Register("test:fail_media", func(tx *gorm.DB) {
				if tx.Statement.Table == "media" {
					tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
			_, err := org.applyOrganizeMedia(t.Context(), organizeMediaRequest{media: &media, transferMode: mode}, organizeMediaDestination{path: dst})
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v", err)
			}
			if data, err := os.ReadFile(src); err != nil || string(data) != "payload" {
				t.Fatalf("source not restored: %q %v", data, err)
			}
			if _, err := os.Lstat(dst); !os.IsNotExist(err) {
				t.Fatalf("target left behind: %v", err)
			}
			row, err := repos.Media.FindByID(t.Context(), media.ID)
			if err != nil || row.Path != src {
				t.Fatalf("row changed: %+v %v", row, err)
			}
		})
	}
}

func TestRollbackTransferDoesNotOverwriteNewSource(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "source", "new file")
	dst := writeTemp(t, dir, "target", "original file")
	failure := errors.New("database failed")
	transferred, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	err = rollbackTransfer(src, dst, TransferMove, transferred, failure)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "restore transfer") {
		t.Fatalf("error = %v", err)
	}
	for path, want := range map[string]string{src: "new file", dst: "original file"} {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Fatalf("lost file: %q %v", data, err)
		}
	}
}

func TestRollbackTransferPreservesReplacedDestination(t *testing.T) {
	for _, mode := range []TransferMode{TransferMove, TransferCopy, TransferHardlink, TransferSymlink} {
		dir := t.TempDir()
		src := writeTemp(t, dir, "source", "original")
		dst := filepath.Join(dir, "target")
		if err := transferFile(src, dst, mode); err != nil {
			t.Fatal(err)
		}
		transferred, err := os.Lstat(dst)
		if err != nil {
			t.Fatal(err)
		}
		// 移走原文件而非删除，避免文件系统立即复用 inode。
		if err := os.Rename(dst, filepath.Join(dir, "saved")); err != nil {
			t.Fatal(err)
		}
		writeTemp(t, dir, "target", "replacement")
		failure := errors.New("database failed")
		err = rollbackTransfer(src, dst, mode, transferred, failure)
		if !errors.Is(err, failure) || !strings.Contains(err.Error(), "destination changed") {
			t.Fatalf("error = %v", err)
		}
		if data, err := os.ReadFile(dst); err != nil || string(data) != "replacement" {
			t.Fatalf("removed replacement: %q %v", data, err)
		}
	}
}

func TestReclassificationDatabaseFailureRestoresFile(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		repos := newOrganizerTestRepo(t)
		dir := t.TempDir()
		src := writeTemp(t, dir, "source.mkv", "payload")
		dst := filepath.Join(dir, "category", "target.mkv")
		media := model.Media{Path: src}
		if err := repos.Media.Upsert(t.Context(), &media); err != nil {
			t.Fatal(err)
		}
		failure := errors.New("injected database failure")
		if err := repos.DB.Callback().Update().Before("gorm:update").Register("test:fail_media", func(tx *gorm.DB) {
			if tx.Statement.Table == "media" {
				tx.AddError(failure)
			}
		}); err != nil {
			t.Fatal(err)
		}
		org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
		req := organizeExistingReclassifyRequest{Target: dst, DestRoot: dir, Category: "category", Existing: []string{src}, Result: &OrganizeResult{}}
		var err error
		if conflict {
			_, err = org.moveReclassifiedConflict(t.Context(), req, src, dst)
		} else {
			_, err = org.reclassifyExistingMedia(t.Context(), req)
		}
		if !errors.Is(err, failure) {
			t.Fatalf("error = %v", err)
		}
		if data, err := os.ReadFile(src); err != nil || string(data) != "payload" {
			t.Fatalf("source lost: %q %v", data, err)
		}
		files, err := os.ReadDir(filepath.Dir(dst))
		if err != nil || len(files) != 0 {
			t.Fatalf("target files left behind: %v %v", files, err)
		}
	}
}

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
