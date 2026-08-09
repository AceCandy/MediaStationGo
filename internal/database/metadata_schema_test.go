package database

import (
	"strings"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type legacyRequiredMedia struct {
	ID         string `gorm:"primaryKey;size:36"`
	MetadataID string `gorm:"size:36;not null;check:chk_media_metadata_id,metadata_id <> ''"`
	Path       string `gorm:"uniqueIndex;size:1024;not null"`
}

func TestCatalogMetadataSnapshotAndJobSchema(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataProviderSnapshot{}, &model.CatalogHydrationJob{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := model.MetadataProviderSnapshot{MetadataID: metadata.ID, Provider: "tmdb", Payload: `{"future":true}`, FetchedAt: time.Now().UTC()}
	if err := db.Create(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	columns, err := db.Migrator().ColumnTypes(&model.MetadataProviderSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	foundJSONB := false
	for _, column := range columns {
		if column.Name() == "payload" && strings.EqualFold(column.DatabaseTypeName(), "jsonb") {
			foundJSONB = true
		}
	}
	if !foundJSONB {
		t.Fatal("metadata provider snapshot payload is not PostgreSQL jsonb")
	}
	jobs := []model.CatalogHydrationJob{
		{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot},
		{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot},
	}
	if err := db.Create(&jobs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&jobs[1]).Error; err == nil {
		t.Fatal("expected duplicate catalog hydration identity to be rejected")
	}
}

func (legacyRequiredMedia) TableName() string { return "media" }

func TestMetadataSchemaCanonicalIdentityConstraints(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
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
	unresolved := model.Media{LibraryID: "library", Title: "Missing", Path: "/missing.mkv"}
	if err := db.Create(&unresolved).Error; err != nil {
		t.Fatalf("create unresolved media: %v", err)
	}
	var unresolvedCount int64
	if err := db.Raw(`SELECT COUNT(1) FROM media WHERE id = ? AND metadata_id IS NULL`, unresolved.ID).Scan(&unresolvedCount).Error; err != nil {
		t.Fatal(err)
	}
	if unresolvedCount != 1 {
		t.Fatal("expected unresolved media metadata id to be NULL")
	}
	if err := db.Create(&model.Media{LibraryID: "library", MetadataID: "missing", Title: "Dangling", Path: "/dangling.mkv"}).Error; err == nil {
		t.Fatal("expected dangling media metadata id to be rejected")
	}
	if err := db.Unscoped().Delete(&movie).Error; err == nil {
		t.Fatal("expected referenced metadata deletion to be rejected")
	}
}

func TestMetadataSchemaMigrationMakesMediaMetadataNullable(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &legacyRequiredMedia{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Existing", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyRequiredMedia{ID: "legacy", MetadataID: metadata.ID, Path: "/legacy.mkv"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	unresolved := model.Media{LibraryID: "library", Title: "Pending", Path: "/pending.mkv"}
	if err := db.Create(&unresolved).Error; err != nil {
		t.Fatalf("create unresolved media after migration: %v", err)
	}
	var nullCount int64
	if err := db.Raw(`SELECT COUNT(1) FROM media WHERE id = ? AND metadata_id IS NULL`, unresolved.ID).Scan(&nullCount).Error; err != nil {
		t.Fatal(err)
	}
	if nullCount != 1 {
		t.Fatal("expected migrated metadata_id column to accept NULL")
	}
}

func TestMediaProbeMetadataOneToOneAndCascade(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
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
