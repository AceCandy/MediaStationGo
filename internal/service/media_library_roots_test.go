package service

import (
	"errors"
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

func TestCreateLibraryWithRootsRejectsCloudRoot(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	_, err := svc.CreateLibraryWithRoots(t.Context(), "国漫", "anime", []LibraryRootInput{{
		Path: "cloud://openlist/动漫/国漫?dir=国漫&auto_category=1",
	}})
	if !errors.Is(err, ErrCloudLibraryRootUnsupported) {
		t.Fatalf("error = %v, want ErrCloudLibraryRootUnsupported", err)
	}
}
