package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testutil"
)

func newServiceTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if len(models) > 0 {
		models = append(models,
			&model.MetadataItem{}, &model.MetadataIdentifier{},
			&model.ArtworkAsset{}, &model.MetadataArtwork{},
		)
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatal(err)
		}
		if err := testutil.RegisterMediaMetadataFixtures(db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func createServiceTestMetadata(t *testing.T, db *gorm.DB, item model.MetadataItem, identifiers ...model.MetadataIdentifier) *model.MetadataItem {
	t.Helper()
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	for i := range identifiers {
		identifiers[i].MetadataID = item.ID
	}
	if len(identifiers) > 0 {
		if err := db.Create(&identifiers).Error; err != nil {
			t.Fatalf("create metadata identifiers: %v", err)
		}
	}
	return &item
}

func createServiceTestEpisodeMetadata(t *testing.T, db *gorm.DB, series, episode model.MetadataItem, identifiers ...model.MetadataIdentifier) *model.MetadataItem {
	t.Helper()
	seriesItem := createServiceTestMetadata(t, db, series, identifiers...)
	season := createServiceTestMetadata(t, db, model.MetadataItem{
		Kind:      model.MetadataKindSeason,
		ParentID:  &seriesItem.ID,
		SeasonNum: episode.SeasonNum,
		Title:     series.Title,
		Source:    series.Source,
	})
	episode.ParentID = &season.ID
	episode.SeasonNum = 0
	return createServiceTestMetadata(t, db, episode)
}

func serviceTestMediaForUpsert(media *model.Media) *model.Media {
	if media != nil && media.EpisodeNum > 0 && media.SeriesID == "" {
		media.SeriesID = localSeriesIdentity(media)
	}
	return media
}

func createServiceTestArtwork(t *testing.T, db *gorm.DB, metadataID, artworkType, assetID string) string {
	t.Helper()
	if err := db.Create(&model.ArtworkAsset{
		Base: model.Base{ID: assetID}, SHA256: assetID, StorageKey: "test/" + assetID + ".jpg", MimeType: "image/jpeg",
	}).Error; err != nil {
		t.Fatalf("create artwork asset: %v", err)
	}
	if err := db.Create(&model.MetadataArtwork{
		MetadataID: metadataID, AssetID: assetID, ArtworkType: artworkType,
	}).Error; err != nil {
		t.Fatalf("create metadata artwork: %v", err)
	}
	return "/api/artwork/" + assetID
}

func serviceTestMediaView(t *testing.T, repos *repository.Container, mediaID string) *model.MediaView {
	t.Helper()
	view, err := repos.MediaView.FindByID(t.Context(), mediaID)
	if err != nil || view == nil {
		t.Fatalf("find media view %q: %#v %v", mediaID, view, err)
	}
	return view
}

func serviceTestLocalMetadataHint(t *testing.T, media model.Media) *LocalMetadata {
	t.Helper()
	local, err := decodeLocalMetadataHint(media.LocalMetadataHint)
	if err != nil || local == nil {
		t.Fatalf("decode local metadata hint for %q: %#v %v", media.Path, local, err)
	}
	return local
}
