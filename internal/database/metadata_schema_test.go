package database

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMetadataSchemaCanonicalIdentityConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:metadata_schema?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.MetadataItem{}, &model.MetadataIdentifier{},
		&model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.Media{},
	); err != nil {
		t.Fatal(err)
	}

	parent := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{
		Kind: model.MetadataKindSeason, ParentID: &parent.ID,
		SeasonNum: 1, Title: "Season 1", Source: "tmdb",
	}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{
		Kind: model.MetadataKindEpisode, ParentID: &season.ID,
		EpisodeNum: 2, Title: "Episode", Source: "tmdb",
	}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	orphanEpisode := model.MetadataItem{Kind: model.MetadataKindEpisode, EpisodeNum: 3, Title: "Orphan", Source: "tmdb"}
	if err := db.Create(&orphanEpisode).Error; err == nil {
		t.Fatal("expected episode without parent to be rejected")
	}
	invalidMovie := model.MetadataItem{Kind: model.MetadataKindMovie, SeasonNum: 1, EpisodeNum: 1, Title: "Invalid Movie", Source: "tmdb"}
	if err := db.Create(&invalidMovie).Error; err == nil {
		t.Fatal("expected movie with episode identity to be rejected")
	}
	duplicateEpisode := episode
	duplicateEpisode.ID = ""
	if err := db.Create(&duplicateEpisode).Error; err == nil {
		t.Fatal("expected duplicate parent/episode to be rejected")
	}
	duplicateSeason := season
	duplicateSeason.ID = ""
	if err := db.Create(&duplicateSeason).Error; err == nil {
		t.Fatal("expected duplicate parent/season to be rejected")
	}

	movie := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{MetadataID: movie.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42"},
		{MetadataID: parent.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42"},
	}
	if err := db.Create(&identifiers).Error; err != nil {
		t.Fatalf("movie and series identifiers with the same numeric ID must coexist: %v", err)
	}
	duplicateIdentifier := model.MetadataIdentifier{
		MetadataID: parent.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42",
	}
	if err := db.Create(&duplicateIdentifier).Error; err == nil {
		t.Fatal("expected duplicate provider/entity/external ID to be rejected")
	}

	media := model.Media{LibraryID: "library", MetadataID: movie.ID, Title: "Movie", Path: "/movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media with metadata: %v", err)
	}
	if err := db.Create(&model.Media{LibraryID: "library", Title: "Missing", Path: "/missing.mkv"}).Error; err == nil {
		t.Fatal("expected empty media metadata id to be rejected")
	}
	if err := db.Create(&model.Media{LibraryID: "library", MetadataID: "missing", Title: "Dangling", Path: "/dangling.mkv"}).Error; err == nil {
		t.Fatal("expected dangling media metadata id to be rejected")
	}
	if err := db.Unscoped().Delete(&movie).Error; err == nil {
		t.Fatal("expected referenced metadata deletion to be rejected")
	}
}

func TestMediaProbeMetadataOneToOneAndCascade(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:media_probe_schema?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Media{}, &model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: "library", MetadataID: metadata.ID, Title: "Movie", Path: "/probe.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	row := model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: `{"schema_version":1}`, SchemaVersion: 1, ProbedAt: time.Now()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := row
	duplicate.ProbeJSON = `{"schema_version":1,"duplicate":true}`
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("expected one-to-one primary key constraint")
	}
	if err := db.Unscoped().Delete(&media).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.MediaProbeMetadata{}).Where("media_id = ?", media.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("probe rows after media delete = %d, want 0", count)
	}
}
