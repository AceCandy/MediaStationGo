package repository

import (
	"context"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLibraryRootsMissingTableReturnsErrors(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.LibraryRoot{}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "现行库", Path: "/existing", Type: "movie", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	root := model.LibraryRoot{LibraryID: lib.ID, Path: lib.Path, Enabled: true}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.LibraryRoot{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).Library
	for _, tc := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"list", func(ctx context.Context) error { _, err := repo.List(ctx); return err }},
		{"find", func(ctx context.Context) error { _, err := repo.FindByID(ctx, lib.ID); return err }},
		{"list roots", func(ctx context.Context) error { _, err := repo.ListRoots(ctx, lib.ID); return err }},
		{"find root", func(ctx context.Context) error { _, err := repo.FindRootByID(ctx, lib.ID, root.ID); return err }},
		{"create root", func(ctx context.Context) error {
			return repo.CreateRoot(ctx, &model.LibraryRoot{LibraryID: lib.ID, Path: "/new", Enabled: true})
		}},
		{"update root", func(ctx context.Context) error { return repo.UpdateRoot(ctx, &root, map[string]any{"path": "/new"}) }},
		{"delete root", func(ctx context.Context) error { return repo.DeleteRoot(ctx, lib.ID, root.ID) }},
		{"create library", func(ctx context.Context) error {
			return repo.CreateWithRoots(ctx, &model.Library{Name: "新库", Path: "/new", Type: "movie"}, []model.LibraryRoot{{Path: "/new", Enabled: true}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(t.Context()); err == nil {
				t.Fatal("missing current directory table was accepted")
			}
		})
	}
	var count int64
	if err := db.Model(&model.Library{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("failed create left a library without directories: count=%d err=%v", count, err)
	}
}
