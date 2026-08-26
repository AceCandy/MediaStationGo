package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestListTMDbEpisodeMetadataRecheckAfterFiltersAndPages(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	series := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000001"}, Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000002"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"}
	generated := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000003"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", Overview: "Overview", ReleaseDate: "2026-08-01", Source: "tmdb"}
	missingDate := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000004"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "Real title", Overview: "Overview", Source: "tmdb"}
	complete := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000005"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 3, Title: "EpisodeXYZ", Overview: "Overview", ReleaseDate: "2026-08-03", Source: "tmdb"}
	noMedia := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000006"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 4, Title: "No media", Source: "tmdb"}
	cooled := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000007"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 5, Title: "Cooled", Source: "tmdb", TMDbEpisodeCheckedAt: &now}
	if err := db.Create(&[]model.MetadataItem{series, season, generated, missingDate, complete, noMedia, cooled}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "still", StorageKey: "still.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	for _, episode := range []model.MetadataItem{generated, missingDate, complete} {
		if err := db.Create(&model.MetadataArtwork{MetadataID: episode.ID, AssetID: asset.ID, ArtworkType: model.ArtworkTypeStill, SourceProvider: "tmdb"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, episode := range []model.MetadataItem{generated, missingDate, complete, cooled} {
		if err := db.Create(&model.Media{MetadataID: episode.ID, Path: "/library/episode-" + string(rune('1'+i)) + ".mkv"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db).Metadata
	first, err := repo.ListTMDbEpisodeMetadataRecheckAfter(t.Context(), "", now.Add(-72*time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].MetadataID != generated.ID || first[0].SeriesTMDbID != "42" || first[0].StillMissing {
		t.Fatalf("first page = %#v", first)
	}
	second, err := repo.ListTMDbEpisodeMetadataRecheckAfter(t.Context(), first[0].MetadataID, now.Add(-72*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].MetadataID != missingDate.ID {
		t.Fatalf("second page = %#v", second)
	}
}
