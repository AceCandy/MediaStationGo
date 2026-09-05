package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newFileManagerTestServiceWithRepo(t *testing.T, root string) (*FileManagerService, *repository.Container) {
	t.Helper()
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := model.Library{Name: "downloads", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.App.DataDir = root
	cfg.Cache.CacheDir = root
	return NewFileManagerService(cfg, zap.NewNop(), repos), repos
}

func newFileManagerTestService(t *testing.T, root string) *FileManagerService {
	t.Helper()
	svc, _ := newFileManagerTestServiceWithRepo(t, root)
	return svc
}

func TestFileManagerRecursiveListAndMutations(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "downloads", "国产剧")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(nested, "狂飙.S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newFileManagerTestService(t, root)

	listing, err := svc.List(root, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range listing.Entries {
		if entry.Path == mediaPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("recursive listing did not include %s", mediaPath)
	}

	created, err := svc.CreateFolder(root, "整理目标")
	if err != nil {
		t.Fatal(err)
	}
	emptyListing, err := svc.List(created.Path, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if emptyListing.Entries == nil || len(emptyListing.Entries) != 0 {
		t.Fatalf("empty directory entries = %#v, want empty slice", emptyListing.Entries)
	}
	copied, err := svc.Transfer(mediaPath, created.Path, TransferCopy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copied.Path); err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	renamed, err := svc.Rename(copied.Path, "狂飙 - S01E01.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(renamed.Path); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if err := svc.Delete(renamed.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(renamed.Path); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err=%v", err)
	}
}

func TestFileManagerDeletesOnlyResolvedSTRMTarget(t *testing.T) {
	root := t.TempDir()
	svc, repos := newFileManagerTestServiceWithRepo(t, root)
	dir := filepath.Join(root, "Movie")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "Movie.mkv")
	strmPath := filepath.Join(root, "Movie.strm")
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-file"}, Title: "Movie", Path: strmPath}
	if err := repos.DB.Create(media).Error; err != nil {
		t.Fatal(err)
	}

	resolved, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TargetPath != target || resolved.ParentPath != dir {
		t.Fatalf("resolved target = %#v", resolved)
	}
	deleted, err := svc.DeleteSTRMTarget(t.Context(), media.ID, false)
	if err != nil || deleted != target {
		t.Fatalf("deleted path/error = %q/%v", deleted, err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists: %v", err)
	}
	if _, err := os.Stat(strmPath); err != nil {
		t.Fatalf("STRM sidecar was removed: %v", err)
	}
	if stored, err := repos.Media.FindByID(t.Context(), media.ID); err != nil || stored == nil {
		t.Fatalf("media record was removed: %#v, %v", stored, err)
	}
}

func TestFileManagerDeletesResolvedSTRMTargetParent(t *testing.T) {
	root := t.TempDir()
	svc, repos := newFileManagerTestServiceWithRepo(t, root)
	dir := filepath.Join(root, "Show", "Season 01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "Show.S01E01.mkv")
	strmPath := filepath.Join(root, "Show.strm")
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-parent"}, Title: "Show", Path: strmPath}
	if err := repos.DB.Create(media).Error; err != nil {
		t.Fatal(err)
	}

	deleted, err := svc.DeleteSTRMTarget(t.Context(), media.ID, true)
	if err != nil || deleted != dir {
		t.Fatalf("deleted path/error = %q/%v", deleted, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("parent directory still exists: %v", err)
	}
	if _, err := os.Stat(strmPath); err != nil {
		t.Fatalf("STRM sidecar was removed: %v", err)
	}
	if stored, err := repos.Media.FindByID(t.Context(), media.ID); err != nil || stored == nil {
		t.Fatalf("media record was removed: %#v, %v", stored, err)
	}
}

func TestFileManagerResolvesMappedSTRMTargetOutsideFileRoots(t *testing.T) {
	root := t.TempDir()
	svc, repos := newFileManagerTestServiceWithRepo(t, root)
	mappingRoot := t.TempDir()
	target := filepath.Join(mappingRoot, "Mapped.mkv")
	strmPath := filepath.Join(root, "Mapped.strm")
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strmPath, []byte("https://media.example.test/archive/Mapped.mkv"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), FFprobePathMappingsSettingKey, "https://media.example.test/archive/ => "+mappingRoot); err != nil {
		t.Fatal(err)
	}
	media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-mapped"}, Title: "Mapped", Path: strmPath}
	if err := repos.DB.Create(media).Error; err != nil {
		t.Fatal(err)
	}

	resolved, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID)
	if err != nil || resolved.TargetPath != target {
		t.Fatalf("resolved target/error = %#v/%v", resolved, err)
	}
	if _, err := svc.DeleteSTRMTarget(t.Context(), media.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("mapped target still exists: %v", err)
	}
}

func TestFileManagerRejectsUnsafeSTRMDeleteTargets(t *testing.T) {
	root := t.TempDir()
	svc, repos := newFileManagerTestServiceWithRepo(t, root)

	t.Run("unmapped remote URL", func(t *testing.T) {
		strmPath := filepath.Join(root, "Remote.strm")
		if err := os.WriteFile(strmPath, []byte("https://media.example.test/Remote.mkv"), 0o644); err != nil {
			t.Fatal(err)
		}
		media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-remote"}, Title: "Remote", Path: strmPath}
		if err := repos.DB.Create(media).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID); !errors.Is(err, ErrSTRMTargetNotDeletable) {
			t.Fatalf("error = %v, want ErrSTRMTargetNotDeletable", err)
		}
	})

	t.Run("allowed root parent", func(t *testing.T) {
		target := filepath.Join(root, "RootMovie.mkv")
		strmPath := filepath.Join(root, "RootMovie.strm")
		if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
			t.Fatal(err)
		}
		media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-root"}, Title: "Root", Path: strmPath}
		if err := repos.DB.Create(media).Error; err != nil {
			t.Fatal(err)
		}
		resolved, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID)
		if err != nil || resolved.ParentPath != "" {
			t.Fatalf("resolved target/error = %#v/%v", resolved, err)
		}
		if _, err := svc.DeleteSTRMTarget(t.Context(), media.ID, true); !errors.Is(err, ErrRootMutation) {
			t.Fatalf("error = %v, want ErrRootMutation", err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("protected target was removed: %v", err)
		}
	})

	t.Run("parent containing sidecar", func(t *testing.T) {
		dir := filepath.Join(root, "Together")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "Together.mkv")
		strmPath := filepath.Join(dir, "Together.strm")
		if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
			t.Fatal(err)
		}
		media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-together"}, Title: "Together", Path: strmPath}
		if err := repos.DB.Create(media).Error; err != nil {
			t.Fatal(err)
		}
		resolved, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID)
		if err != nil || resolved.ParentPath != "" {
			t.Fatalf("resolved target/error = %#v/%v", resolved, err)
		}
		if _, err := svc.DeleteSTRMTarget(t.Context(), media.ID, true); !errors.Is(err, ErrRootMutation) {
			t.Fatalf("error = %v, want ErrRootMutation", err)
		}
		if _, err := os.Stat(strmPath); err != nil {
			t.Fatalf("STRM sidecar was removed: %v", err)
		}
	})

	t.Run("symlink target", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "Outside.mkv")
		link := filepath.Join(root, "Linked.mkv")
		strmPath := filepath.Join(root, "Linked.strm")
		if err := os.WriteFile(outside, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := os.WriteFile(strmPath, []byte(link), 0o644); err != nil {
			t.Fatal(err)
		}
		media := &model.Media{PermanentBase: model.PermanentBase{ID: "strm-delete-link"}, Title: "Link", Path: strmPath}
		if err := repos.DB.Create(media).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ResolveSTRMDeleteTarget(t.Context(), media.ID); !errors.Is(err, ErrPathOutOfBounds) {
			t.Fatalf("error = %v, want ErrPathOutOfBounds", err)
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("symlink target was removed: %v", err)
		}
	})

	t.Run("filesystem root", func(t *testing.T) {
		target := filepath.Join(root, "RootMapped.mkv")
		if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := secureSTRMDeleteTarget(target, []string{string(filepath.Separator)}); !errors.Is(err, ErrPathOutOfBounds) {
			t.Fatalf("error = %v, want ErrPathOutOfBounds", err)
		}
	})
}

func TestFileManagerTransferDirectoryHardlinksFiles(t *testing.T) {
	root := t.TempDir()
	if hardlinksUnsupported(t, root) {
		t.Skip("hardlinks unsupported on this filesystem")
	}
	sourceDir := filepath.Join(root, "downloads", "Show")
	seasonDir := filepath.Join(sourceDir, "Season 01")
	targetRoot := filepath.Join(root, "media")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(seasonDir, "Show.S01E01.mkv")
	if err := os.WriteFile(sourceFile, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newFileManagerTestService(t, root)

	res, err := svc.Transfer(sourceDir, targetRoot, TransferHardlink)
	if err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(res.Path, "Season 01", "Show.S01E01.mkv")
	sourceInfo, err := os.Stat(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("hardlinked directory file missing: %v", err)
	}
	if !os.SameFile(sourceInfo, targetInfo) {
		t.Fatal("directory hardlink should hardlink contained files")
	}
	listing, err := svc.List(res.Path, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range listing.Entries {
		if entry.Path == targetFile {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("target directory listing did not include %s", targetFile)
	}
}

func TestFileManagerRefusesRootMutation(t *testing.T) {
	root := t.TempDir()
	svc := newFileManagerTestService(t, root)
	if err := svc.Delete(root); !errors.Is(err, ErrRootMutation) {
		t.Fatalf("Delete(root) err = %v, want ErrRootMutation", err)
	}
}

func hardlinksUnsupported(t *testing.T, root string) bool {
	t.Helper()
	src := filepath.Join(root, "hardlink-probe-src")
	dst := filepath.Join(root, "hardlink-probe-dst")
	if err := os.WriteFile(src, []byte("probe"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := os.Link(src, dst)
	_ = os.Remove(src)
	_ = os.Remove(dst)
	return err != nil
}

func TestFileManagerIncludesConfiguredOrganizeRoots(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "downloads")
	targetDir := filepath.Join(root, "media")
	for _, dir := range []string{sourceDir, targetDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	svc, repos := newFileManagerTestServiceWithRepo(t, root)
	if err := repos.Setting.Set(t.Context(), "organize.source_dir", sourceDir); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organize.target_dir", targetDir); err != nil {
		t.Fatal(err)
	}
	listing, err := svc.List("", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, root := range listing.Roots {
		got[root.Label] = root.Path
	}
	for label, want := range map[string]string{
		"organize-source": filepath.Clean(sourceDir),
		"organize-target": filepath.Clean(targetDir),
	} {
		if got[label] != want {
			t.Fatalf("root %s = %q, want %q; roots=%#v", label, got[label], want, listing.Roots)
		}
	}
}

func TestFileManagerIncludesAllLibraryRoots(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	nestedB := filepath.Join(rootB, "second-root")
	if err := os.MkdirAll(nestedB, 0o755); err != nil {
		t.Fatal(err)
	}
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	lib := &model.Library{Name: "电影", Path: rootA, Type: "movie", Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), lib, []model.LibraryRoot{
		{Path: rootA, Enabled: true},
		{Path: rootB, Enabled: true, SortOrder: 1},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.App.DataDir = t.TempDir()
	cfg.Cache.CacheDir = t.TempDir()
	svc := NewFileManagerService(cfg, zap.NewNop(), repos)

	listing, err := svc.List("", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, root := range listing.Roots {
		got[root.Label] = root.Path
	}
	if got["library:电影:路径1"] != filepath.Clean(rootA) {
		t.Fatalf("root A missing from listing: %#v", listing.Roots)
	}
	if got["library:电影:路径2"] != filepath.Clean(rootB) {
		t.Fatalf("root B missing from listing: %#v", listing.Roots)
	}
	if _, err := svc.List(nestedB, 100); err != nil {
		t.Fatalf("list second library root: %v", err)
	}
}
