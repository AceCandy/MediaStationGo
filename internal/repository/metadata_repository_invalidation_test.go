package repository

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestInvalidateTMDbIdentifierResetsExactSeriesWorkUnit(t *testing.T) {
	repos := newMediaMetadataRepositoryTest(t)
	series := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Invalid Series", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "404"})
	other := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Other Series", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "405"})
	media := []model.Media{
		{SeriesID: series.ID, Title: "Episode 1", Path: "/series/invalid/s01e01.mkv", TMDbID: 404, ScrapeStatus: "matched", ScrapeTrigger: "manual", ScrapeError: "old"},
		{SeriesID: series.ID, Title: "Episode 2", Path: "/series/invalid/s01e02.mkv", TMDbID: 999, ScrapeStatus: "error", ScrapeTrigger: "manual", ScrapeError: "temporary"},
		{SeriesID: other.ID, Title: "Other Episode", Path: "/series/other/s01e01.mkv", TMDbID: 404, ScrapeStatus: "matched", ScrapeTrigger: "manual"},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	reset, err := repos.Metadata.InvalidateTMDbIdentifier(t.Context(), series.ID, model.MetadataKindSeries, "404")
	if err != nil || reset != 2 {
		t.Fatalf("reset = %d, err = %v", reset, err)
	}
	var gotOther model.Media
	if err := repos.DB.First(&gotOther, "id = ?", media[2].ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotOther.TMDbID != 404 || gotOther.ScrapeStatus != "matched" || gotOther.ScrapeTrigger != "manual" {
		t.Fatalf("other series changed: %+v", gotOther)
	}
	var gotMatching model.Media
	if err := repos.DB.First(&gotMatching, "id = ?", media[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotMatching.TMDbID != 0 || gotMatching.ScrapeStatus != "pending" || gotMatching.ScrapeTrigger != "event" || gotMatching.ScrapeError != "" {
		t.Fatalf("matching episode not repaired: %+v", gotMatching)
	}
	var gotSibling model.Media
	if err := repos.DB.First(&gotSibling, "id = ?", media[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotSibling.TMDbID != 999 || gotSibling.ScrapeStatus != "pending" || gotSibling.ScrapeTrigger != "event" || gotSibling.ScrapeError != "" {
		t.Fatalf("series work unit not fully reset: %+v", gotSibling)
	}
	var identifiers int64
	if err := repos.DB.Model(&model.MetadataIdentifier{}).Where("metadata_id = ?", series.ID).Count(&identifiers).Error; err != nil || identifiers != 0 {
		t.Fatalf("active identifiers = %d, err = %v", identifiers, err)
	}
}

func TestInvalidateTMDbIdentifierScopesMovieAndDirectoryMetadata(t *testing.T) {
	repos := newMediaMetadataRepositoryTest(t)
	movie := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Invalid Movie", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "404"})
	other := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Other Movie", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "405"})
	directory := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Directory Only", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "406"})
	media := []model.Media{
		{MetadataID: movie.ID, Title: movie.Title, Path: "/movies/invalid.mkv", TMDbID: 404, ScrapeStatus: "matched"},
		{MetadataID: other.ID, Title: other.Title, Path: "/movies/other.mkv", TMDbID: 404, ScrapeStatus: "matched"},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	reset, err := repos.Metadata.InvalidateTMDbIdentifier(t.Context(), movie.ID, model.MetadataKindMovie, "404")
	if err != nil || reset != 1 {
		t.Fatalf("movie reset = %d, err = %v", reset, err)
	}
	reset, err = repos.Metadata.InvalidateTMDbIdentifier(t.Context(), directory.ID, model.MetadataKindMovie, "406")
	if err != nil || reset != 0 {
		t.Fatalf("directory reset = %d, err = %v", reset, err)
	}
	var gotOther model.Media
	if err := repos.DB.First(&gotOther, "id = ?", media[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotOther.TMDbID != 404 || gotOther.ScrapeStatus != "matched" {
		t.Fatalf("other movie changed: %+v", gotOther)
	}
	var directoryIdentifiers int64
	if err := repos.DB.Model(&model.MetadataIdentifier{}).Where("metadata_id = ?", directory.ID).Count(&directoryIdentifiers).Error; err != nil || directoryIdentifiers != 0 {
		t.Fatalf("directory identifiers = %d, err = %v", directoryIdentifiers, err)
	}
}
