package service

import (
	"strconv"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testutil"
)

func newServiceTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	{
		models = append(models,
			&model.LibraryRoot{},
			&model.MediaProbeMetadata{}, &model.TranslationCache{}, &model.TMDbCredit{}, &model.TMDbSubject{}, &model.TMDbPersonProfile{}, &model.TMDbPayload{}, &model.TMDbCredit{}, &model.TMDbArtwork{}, &model.TMDbRefreshJob{}, &model.TMDbQueryCache{},
			&model.DoubanSubject{}, &model.DoubanQueryCache{}, &model.MetadataItem{}, &model.MetadataIdentifier{},
			&model.ArtworkAsset{})
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatal(err)
		}
		if db.Migrator().HasTable(&model.Media{}) {
			if err := db.AutoMigrate(&model.NFOItem{}, &model.NFOMediaBinding{}, &model.HongGuoWork{}, &model.HongGuoEpisode{}, &model.HongGuoMediaBinding{}); err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(model.HuangGuoAIModels()...); err != nil {
				t.Fatal(err)
			}
			if err := database.EnsureLatestMediaAddedTriggers(db); err != nil {
				t.Fatal(err)
			}
		}
		if err := database.EnsureMetadataPresentationSchema(db); err != nil {
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
		PermanentBase: model.PermanentBase{ID: assetID}, SHA256: assetID, StorageKey: "test/" + assetID + ".jpg", MimeType: "image/jpeg",
	}).Error; err != nil {
		t.Fatalf("create artwork asset: %v", err)
	}
	if err := testdb.SaveMetadataArtworkFixture(db, metadataID, artworkType, assetID); err != nil {
		t.Fatal(err)
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

func serviceTestTMDbSeries(t *testing.T, repos *repository.Container, view *model.MediaView, tmdbID int) *model.MetadataItem {
	t.Helper()
	series, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, strconv.Itoa(tmdbID))
	if err != nil || series == nil {
		t.Fatalf("find TMDb series %d: %#v %v", tmdbID, series, err)
	}
	if view == nil || view.SeriesID != series.ID {
		t.Fatalf("media series match: view=%#v series=%#v want tmdb=%d", view, series, tmdbID)
	}
	return series
}

func serviceTestArtworkSelections(t *testing.T, repos *repository.Container, metadataID string, artworkTypes ...string) {
	t.Helper()
	for _, artworkType := range artworkTypes {
		asset, err := repos.Artwork.FindSelection(t.Context(), metadataID, artworkType)
		if err != nil || asset == nil {
			t.Fatalf("find %s artwork for metadata %q: %#v %v", artworkType, metadataID, asset, err)
		}
	}
}

func serviceTestLocalMetadataHint(t *testing.T, media model.Media) *LocalMetadata {
	t.Helper()
	local, err := decodeLocalMetadataHint(media.LocalMetadataHint)
	if err != nil || local == nil {
		t.Fatalf("decode local metadata hint for %q: %#v %v", media.Path, local, err)
	}
	return local
}

func createServiceSourceTestMetadata(t *testing.T, db *gorm.DB, item model.MetadataItem, identifiers ...model.MetadataIdentifier) *model.MetadataItem {
	t.Helper()
	saved := createServiceTestMetadata(t, db, item, identifiers...)
	if err := testdb.SaveMetadataPresentationFixture(db, *saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

// createServiceSourceTestEpisodeMetadata 建立展示测试使用的完整来源层级。
func createServiceSourceTestEpisodeMetadata(t *testing.T, db *gorm.DB, series, episode model.MetadataItem, identifiers ...model.MetadataIdentifier) *model.MetadataItem {
	t.Helper()
	seriesItem := createServiceSourceTestMetadata(t, db, series, identifiers...)
	season := createServiceSourceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &seriesItem.ID, SeasonNum: episode.SeasonNum, Title: series.Title, Source: series.Source})
	episode.ParentID = &season.ID
	episode.SeasonNum = 0
	return createServiceSourceTestMetadata(t, db, episode)
}
