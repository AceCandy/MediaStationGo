package repository

import (
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestTMDbArtworkRecheckCandidateIsNotORMRelation(t *testing.T) {
	if _, err := schema.Parse(&TMDbArtworkRecheckCandidate{}, &sync.Map{}, schema.NamingStrategy{}); err != nil {
		t.Fatalf("parse TMDb artwork recheck candidate: %v", err)
	}
}

func TestSaveCatalogSelectionPreservesExistingSelection(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.MetadataArtworkCandidate{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Artwork
	manual := &model.ArtworkAsset{SHA256: "manual", StorageKey: "manual.jpg", MimeType: "image/jpeg"}
	if _, err := repo.SaveSelection(t.Context(), metadata.ID, model.ArtworkTypePoster, "manual", "manual.jpg", manual); err != nil {
		t.Fatal(err)
	}
	catalog := &model.ArtworkAsset{SHA256: "catalog", StorageKey: "catalog.jpg", MimeType: "image/jpeg"}
	if _, selected, err := repo.SaveCatalogSelection(t.Context(), metadata.ID, model.ArtworkTypePoster, "tmdb", "catalog.jpg", catalog); err != nil || selected {
		t.Fatal(err)
	}
	selected, err := repo.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || selected.ID != manual.ID {
		t.Fatalf("catalog replaced manual selection: %#v", selected)
	}
	douban := &model.ArtworkAsset{SHA256: "douban", StorageKey: "douban.jpg", MimeType: "image/jpeg"}
	if _, promoted, err := repo.SaveCandidate(t.Context(), metadata.ID, model.ArtworkTypePoster, "douban", "douban.jpg", douban); err != nil || promoted {
		t.Fatalf("save douban candidate: promoted=%v err=%v", promoted, err)
	}
	selected, err = repo.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil || selected == nil || selected.ID != manual.ID {
		t.Fatalf("candidate replaced current selection: %#v, %v", selected, err)
	}
	if exists, err := repo.HasCandidate(t.Context(), metadata.ID, model.ArtworkTypePoster, "douban"); err != nil || !exists {
		t.Fatalf("douban candidate exists=%v err=%v", exists, err)
	}
}

func TestRepairTMDbSelectionPreservesConcurrentManualSelection(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	old := model.ArtworkAsset{SHA256: "old", StorageKey: "old.jpg", MimeType: "image/jpeg"}
	manual := model.ArtworkAsset{SHA256: "manual-race", StorageKey: "manual-race.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: metadata.ID, ArtworkType: model.ArtworkTypePoster, AssetID: old.ID, SourceProvider: "tmdb", SourceURL: "https://old.test/poster.jpg"}
	if err := db.Create(&selection).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := TMDbArtworkSelection{SelectionID: selection.ID, MetadataID: metadata.ID, ArtworkType: model.ArtworkTypePoster, AssetID: old.ID, SourceURL: selection.SourceURL}
	repo := New(db).Artwork
	if _, err := repo.SaveSelection(t.Context(), metadata.ID, model.ArtworkTypePoster, "manual", "manual.jpg", &manual); err != nil {
		t.Fatal(err)
	}
	replacement := model.ArtworkAsset{SHA256: "replacement", StorageKey: "replacement.jpg", MimeType: "image/jpeg"}
	if _, updated, err := repo.RepairTMDbSelection(t.Context(), snapshot, "https://new.test/poster.jpg", &replacement); err != nil || updated {
		t.Fatalf("conditional repair updated=%v err=%v", updated, err)
	}
	selected, err := repo.FindSelection(t.Context(), metadata.ID, model.ArtworkTypePoster)
	if err != nil || selected == nil || selected.ID != manual.ID {
		t.Fatalf("selection after repair race = %#v, %v", selected, err)
	}
}

func TestArtworkRecheckExcludesNeverHydratedWithoutState(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.MetadataArtworkRecheck{}); err != nil {
		t.Fatal(err)
	}
	items := []model.MetadataItem{
		{Kind: model.MetadataKindMovie, Title: "Never hydrated", Source: "tmdb"},
		{Kind: model.MetadataKindMovie, Title: "Task handoff", Source: "tmdb"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	for i := range items {
		if err := db.Create(&model.MetadataIdentifier{MetadataID: items[i].ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: string(rune('1' + i))}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db).Artwork
	if err := repo.UpsertArtworkRecheck(t.Context(), items[1].ID, model.ArtworkTypePoster, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{MetadataID: items[1].ID, Path: "/library/task-handoff.mkv"}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListTMDbArtworkRecheckMetadataAfter(t.Context(), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].MetadataID != items[1].ID {
		t.Fatalf("recheck candidates = %#v", rows)
	}
}

func TestArtworkRecheckExcludesMetadataWithoutMedia(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.MetadataArtworkRecheck{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	withoutMedia := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000001"}, Kind: model.MetadataKindMovie, Title: "Without media", Source: "tmdb", CatalogArtworkHydratedAt: &now}
	series := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000002"}, Kind: model.MetadataKindSeries, Title: "With episode media", Source: "tmdb", CatalogArtworkHydratedAt: &now}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000003"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"}
	episode := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000004"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Source: "tmdb"}
	if err := db.Create(&[]model.MetadataItem{withoutMedia, series, season, episode}).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{MetadataID: withoutMedia.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "1"},
		{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "2"},
	}
	if err := db.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{MetadataID: episode.ID, Path: "/library/show/season-1/episode-1.mkv"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Artwork
	first, err := repo.ListTMDbArtworkRecheckMetadataAfter(t.Context(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].MetadataID != series.ID {
		t.Fatalf("first recheck page = %#v", first)
	}
	second, err := repo.ListTMDbArtworkRecheckMetadataAfter(t.Context(), first[0].MetadataID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second recheck page = %#v", second)
	}
}
