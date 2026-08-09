package repository

import (
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func newMediaMetadataRepositoryTest(t *testing.T) *Container {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestMediaUpsertLeavesMetadataUnresolvedWithoutCanonicalMatch(t *testing.T) {
	repos := newMediaMetadataRepositoryTest(t)
	media := model.Media{LibraryID: "movies", Title: "Snow White", Path: "/movies/snow-white.mkv", TMDbID: 408}

	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	if media.MetadataID != "" || media.ScrapeStatus != "pending" {
		t.Fatalf("unresolved media = %#v, want empty metadata and pending status", media)
	}
	var nullCount int64
	if err := repos.DB.Raw(`SELECT COUNT(1) FROM media WHERE id = ? AND metadata_id IS NULL`, media.ID).Scan(&nullCount).Error; err != nil {
		t.Fatal(err)
	}
	if nullCount != 1 {
		t.Fatal("expected unresolved media metadata_id to be NULL")
	}
	var metadataCount int64
	if err := repos.DB.Model(&model.MetadataItem{}).Count(&metadataCount).Error; err != nil {
		t.Fatal(err)
	}
	if metadataCount != 0 {
		t.Fatalf("metadata rows = %d, want none before provider lookup", metadataCount)
	}
}

func TestMediaUpsertReusesCanonicalMetadataWithoutOverwritingIt(t *testing.T) {
	repos := newMediaMetadataRepositoryTest(t)
	metadata := model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "Snow White and the Seven Dwarfs",
		Overview: "Provider overview", Year: 1937, Source: "tmdb",
	}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{
		Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "408",
	}}); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID: "movies", Title: "白雪公主和七个小矮人", Year: 1938,
		Path: "/movies/白雪公主和七个小矮人 (1938) [tmdbid=408]/movie.mkv", TMDbID: 408,
	}

	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	if media.MetadataID != metadata.ID || media.ScrapeStatus != "matched" {
		t.Fatalf("media metadata/status = %q/%q, want %q/matched", media.MetadataID, media.ScrapeStatus, metadata.ID)
	}
	saved, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.Title != metadata.Title || saved.Overview != metadata.Overview || saved.Year != metadata.Year || saved.Source != metadata.Source {
		t.Fatalf("canonical metadata was overwritten: %#v", saved)
	}
}

func TestMediaUpsertReusesExistingEpisodeMetadata(t *testing.T) {
	repos := newMediaMetadataRepositoryTest(t)
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &series, []model.MetadataIdentifier{{
		Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "120089",
	}}); err != nil {
		t.Fatal(err)
	}
	season, err := repos.Metadata.UpsertSeason(t.Context(), &model.MetadataItem{
		Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 2, Title: "Season 2", Source: "tmdb",
	})
	if err != nil {
		t.Fatal(err)
	}
	episode, err := repos.Metadata.UpsertEpisode(t.Context(), &model.MetadataItem{
		Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 12,
		Title: "Episode 12", Source: "tmdb",
	})
	if err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID: "shows", Title: "Show", Path: "/shows/show-s02e12.mkv",
		TMDbID: 120089, SeasonNum: 2, EpisodeNum: 12,
	}

	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	if media.MetadataID != episode.ID || media.SeriesID != series.ID || media.ScrapeStatus != "matched" {
		t.Fatalf("episode media = %#v, want existing episode/series binding", media)
	}
}
