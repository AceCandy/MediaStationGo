package service

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestCreateLibraryWithRootsAppendsToExistingLogicalLibrary(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	firstResult, err := svc.CreateLibraryWithRootsAndCover(t.Context(), "欧美电影", "movie", "", []LibraryRootInput{
		{Path: rootA},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !firstResult.Created || len(firstResult.AddedRoots) != 1 {
		t.Fatalf("first result = %#v, want created library with one root", firstResult)
	}
	first := firstResult.Library
	secondResult, err := svc.CreateLibraryWithRootsAndCover(t.Context(), "欧美电影", "movie", "", []LibraryRootInput{
		{Path: rootB},
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondResult.Created || len(secondResult.AddedRoots) != 1 || secondResult.AddedRoots[0].Path != filepath.Clean(rootB) {
		t.Fatalf("second result = %#v, want one appended root", secondResult)
	}
	second := secondResult.Library
	if second.ID != first.ID {
		t.Fatalf("second library id = %q, want existing %q", second.ID, first.ID)
	}

	var libraryCount int64
	if err := db.Model(&model.Library{}).Count(&libraryCount).Error; err != nil {
		t.Fatal(err)
	}
	if libraryCount != 1 {
		t.Fatalf("library count = %d, want one logical library", libraryCount)
	}
	roots, err := repos.Library.ListRoots(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("roots = %#v, want 2", roots)
	}
	if roots[0].Path != filepath.Clean(rootA) || roots[1].Path != filepath.Clean(rootB) {
		t.Fatalf("root paths = %#v, want %q then %q", roots, filepath.Clean(rootA), filepath.Clean(rootB))
	}
}

func TestCreateLibraryWithRootsStoresAndUpdatesCustomCover(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	result, err := svc.CreateLibraryWithRootsAndCover(t.Context(), "收藏", "movie", "https://example.com/cover.jpg", []LibraryRootInput{{Path: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	lib := result.Library
	if lib.CoverURL != "https://example.com/cover.jpg" {
		t.Fatalf("cover_url = %q", lib.CoverURL)
	}
	if err := svc.UpdateLibraryCover(t.Context(), lib.ID, "https://example.com/new.jpg"); err != nil {
		t.Fatal(err)
	}
	updated, err := repos.Library.FindByID(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CoverURL != "https://example.com/new.jpg" {
		t.Fatalf("updated cover_url = %q", updated.CoverURL)
	}
}

func TestCreateLibraryWithRootsKeepsDifferentTypesSeparate(t *testing.T) {
	rootMovie := t.TempDir()
	rootTV := t.TempDir()
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	if _, err := svc.CreateLibraryWithRoots(t.Context(), "综合", "movie", []LibraryRootInput{{Path: rootMovie}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateLibraryWithRoots(t.Context(), "综合", "tv", []LibraryRootInput{{Path: rootTV}}); err != nil {
		t.Fatal(err)
	}
	var libraryCount int64
	if err := db.Model(&model.Library{}).Count(&libraryCount).Error; err != nil {
		t.Fatal(err)
	}
	if libraryCount != 2 {
		t.Fatalf("library count = %d, want separate libraries for different types", libraryCount)
	}
}

func TestCreateLibraryWithRootsRejectsInaccessibleRoot(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	_, err := svc.CreateLibraryWithRoots(t.Context(), "国漫", "anime", []LibraryRootInput{{
		Path: filepath.Join(t.TempDir(), "missing"),
	}})
	if err == nil {
		t.Fatal("expected inaccessible library root to be rejected")
	}
}

func TestListLibraryRootsDoesNotBackfillPath(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{})
	repos := repository.New(db)
	lib := model.Library{Name: "无目录记录", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	roots, err := svc.ListLibraryRoots(t.Context(), lib.ID)
	if err != nil || len(roots) != 0 {
		t.Fatalf("directory read created rows from Path: roots=%+v err=%v", roots, err)
	}
	var count int64
	if err := db.Model(&model.LibraryRoot{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("directory read wrote rows: count=%d err=%v", count, err)
	}
}

func TestLibraryRootUpdateAndDeleteKeepsPrimaryPath(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	lib, err := svc.CreateLibraryWithRoots(t.Context(), "多路径库", "movie", []LibraryRootInput{
		{Path: t.TempDir()}, {Path: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	roots, err := svc.ListLibraryRoots(t.Context(), lib.ID)
	if err != nil || len(roots) != 2 {
		t.Fatalf("roots=%+v err=%v", roots, err)
	}
	newPath := t.TempDir()
	disabled := false
	updated, err := svc.UpdateLibraryRoot(t.Context(), lib.ID, roots[0].ID, LibraryRootInput{Path: newPath, Enabled: &disabled})
	if err != nil || updated == nil || updated.Enabled || updated.Path != newPath {
		t.Fatalf("updated root=%+v err=%v", updated, err)
	}
	got, err := repos.Library.FindByID(t.Context(), lib.ID)
	if err != nil || got == nil || got.Path != newPath || len(got.Roots) != 2 {
		t.Fatalf("updated primary path=%+v err=%v", got, err)
	}
	files := []model.Media{
		{LibraryID: lib.ID, LibraryRootID: roots[0].ID, Path: filepath.Join(newPath, "a.mkv")},
		{LibraryID: lib.ID, LibraryRootID: roots[1].ID, Path: filepath.Join(roots[1].Path, "b.mkv")},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteLibraryRoot(t.Context(), lib.ID, roots[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err = repos.Library.FindByID(t.Context(), lib.ID)
	if err != nil || got == nil || got.Path != roots[1].Path || len(got.Roots) != 1 || got.Roots[0].ID != roots[1].ID {
		t.Fatalf("remaining primary path=%+v err=%v", got, err)
	}
	if err := svc.DeleteLibraryRoot(t.Context(), lib.ID, roots[1].ID); err == nil {
		t.Fatal("last directory was deleted")
	}
	var remaining []model.Media
	if err := db.Find(&remaining).Error; err != nil || len(remaining) != 1 || remaining[0].ID != files[1].ID {
		t.Fatalf("directory deletion affected other media: %+v err=%v", remaining, err)
	}
	if root, err := repos.Library.FindRootByID(t.Context(), lib.ID, roots[1].ID); err != nil || root == nil {
		t.Fatalf("last directory was not preserved: %+v err=%v", root, err)
	}
}
