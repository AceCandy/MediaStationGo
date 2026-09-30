package repository

import (
	"context"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestNFOIngestResultCommitBoundaries(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.NFOItem{}, &model.NFOMediaBinding{}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Local", Type: model.LibraryTypeNFOMovie}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).NFO
	media := model.Media{LibraryID: lib.ID, Path: "/local/movie.mkv", CatalogSource: model.CatalogSourceNFO, ScrapeStatus: "matched"}
	// 事务已经插入媒体后资料校验失败，不能返回新增或变化，也不能留下文件。
	changed, added, err := repo.IngestWithResult(t.Context(), &media, &NFOIngest{})
	if err == nil || changed || added {
		t.Fatalf("rollback changed=%v added=%v err=%v", changed, added, err)
	}
	var count int64
	if err := db.Model(&model.Media{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback media count=%d err=%v", count, err)
	}
	input := &NFOIngest{
		Items:   []model.NFOItem{{Kind: model.MetadataKindMovie, LocalKey: "movie", NFOFields: model.NFOFields{Title: "Local"}}},
		Binding: model.NFOMediaBinding{Fingerprint: "one", ScanInputs: "initial inputs"},
	}
	changed, added, err = repo.IngestWithResult(t.Context(), &media, input)
	if err != nil || !changed || !added {
		t.Fatalf("new changed=%v added=%v err=%v", changed, added, err)
	}
	changed, added, err = repo.IngestWithResult(t.Context(), &media, input)
	if err != nil || changed || added {
		t.Fatalf("unchanged changed=%v added=%v err=%v", changed, added, err)
	}
	var before model.NFOMediaBinding
	if err := db.First(&before, "media_id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	input.Binding.ScanInputs = "new dependency versions"
	changed, added, err = repo.IngestWithResult(t.Context(), &media, input)
	if err != nil || changed || added {
		t.Fatalf("inputs-only changed=%v added=%v err=%v", changed, added, err)
	}
	var after model.NFOMediaBinding
	if err := db.First(&after, "media_id = ?", media.ID).Error; err != nil || after.ScanInputs != input.Binding.ScanInputs || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("inputs-only binding=%+v err=%v", after, err)
	}
	input.Binding.Fingerprint = "two"
	changed, added, err = repo.IngestWithResult(t.Context(), &media, input)
	if err != nil || !changed || added {
		t.Fatalf("updated changed=%v added=%v err=%v", changed, added, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	input.Binding.ScanInputs = "cancelled inputs"
	changed, added, err = repo.IngestWithResult(ctx, &media, input)
	if err == nil || changed || added {
		t.Fatalf("cancelled changed=%v added=%v err=%v", changed, added, err)
	}
	var saved model.NFOMediaBinding
	if err := db.First(&saved, "media_id = ?", media.ID).Error; err != nil || saved.ScanInputs == "cancelled inputs" {
		t.Fatalf("cancelled inputs were saved: %+v %v", saved, err)
	}
}
