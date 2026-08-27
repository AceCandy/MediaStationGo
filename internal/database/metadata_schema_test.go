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

type legacyPositiveSeasonMetadataItem struct {
	ID         string  `gorm:"primaryKey;size:36"`
	Kind       string  `gorm:"size:16;not null;check:chk_metadata_identity,(kind = 'season' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num > 0 AND episode_num = 0) OR (kind = 'episode' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num = 0 AND episode_num > 0) OR (kind IN ('movie','series') AND parent_id IS NULL AND season_num = 0 AND episode_num = 0)"`
	ParentID   *string `gorm:"size:36"`
	SeasonNum  int
	EpisodeNum int
	Title      string `gorm:"size:255;not null"`
	Source     string `gorm:"size:32;not null"`
}

type legacyEpisodeTitleMetadataItem struct {
	ID           string  `gorm:"primaryKey;size:36"`
	Kind         string  `gorm:"size:16;not null"`
	ParentID     *string `gorm:"size:36"`
	SeasonNum    int
	EpisodeNum   int
	Title        string `gorm:"size:255;not null"`
	EpisodeTitle string `gorm:"size:255"`
	Source       string `gorm:"size:32;not null"`
}

type legacyTMDbEpisodeCheckedMetadataItem struct {
	ID                   string     `gorm:"primaryKey;size:36"`
	Kind                 string     `gorm:"size:16;not null"`
	Title                string     `gorm:"size:255;not null"`
	Source               string     `gorm:"size:32;not null"`
	TMDbEpisodeCheckedAt *time.Time `gorm:"index"`
}

func TestCatalogMetadataSnapshotAndJobSchema(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataProviderSnapshot{}, &model.CatalogHydrationJob{}); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&model.MetadataItem{}, "PeopleHydratedAt") {
		t.Fatal("metadata items people_hydrated_at column is missing")
	}
	if !db.Migrator().HasColumn("metadata_items", "tmdb_episode_checked_at") {
		t.Fatal("metadata items tmdb_episode_checked_at column is missing")
	}
	if db.Migrator().HasColumn("metadata_items", "tm_db_episode_checked_at") {
		t.Fatal("metadata items legacy tm_db_episode_checked_at column exists")
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

func TestAutoMigrateReplacesLegacyMetadataIdentityConstraint(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyPositiveSeasonMetadataItem{}); err != nil {
		t.Fatal(err)
	}
	parent := legacyPositiveSeasonMetadataItem{ID: "legacy-series", Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	legacySeason := legacyPositiveSeasonMetadataItem{ID: "legacy-season-zero", Kind: model.MetadataKindSeason, ParentID: &parent.ID, Title: "Specials", Source: "tmdb"}
	if err := db.Create(&legacySeason).Error; err == nil {
		t.Fatal("expected legacy constraint to reject season zero")
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasConstraint(&model.MetadataItem{}, "chk_metadata_identity") {
		t.Fatal("legacy metadata identity constraint still exists")
	}
	if !db.Migrator().HasConstraint(&model.MetadataItem{}, "chk_metadata_identity_season_zero") {
		t.Fatal("replacement metadata identity constraint is missing")
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &parent.ID, Title: "Specials", Source: "tmdb"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("season zero rejected after migration: %v", err)
	}
	invalidMovie := model.MetadataItem{Kind: model.MetadataKindMovie, SeasonNum: 1, Title: "Invalid", Source: "tmdb"}
	if err := db.Create(&invalidMovie).Error; err == nil {
		t.Fatal("expected migrated constraint to reject invalid movie identity")
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}
	if !db.Migrator().HasConstraint(&model.MetadataItem{}, "chk_metadata_identity_season_zero") {
		t.Fatal("replacement metadata identity constraint was lost after repeated migration")
	}
}

func (legacyRequiredMedia) TableName() string { return "media" }

func (legacyPositiveSeasonMetadataItem) TableName() string { return "metadata_items" }

func (legacyEpisodeTitleMetadataItem) TableName() string { return "metadata_items" }

func (legacyTMDbEpisodeCheckedMetadataItem) TableName() string { return "metadata_items" }

func TestAutoMigratePreservesLegacyTMDbEpisodeCheckedAt(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyTMDbEpisodeCheckedMetadataItem{}); err != nil {
		t.Fatal(err)
	}
	legacyOnly := time.Date(2026, 8, 26, 1, 2, 3, 0, time.UTC)
	legacyConflict := legacyOnly.Add(time.Hour)
	canonicalConflict := legacyOnly.Add(2 * time.Hour)
	rows := []legacyTMDbEpisodeCheckedMetadataItem{
		{ID: "legacy-check-only", Kind: model.MetadataKindSeries, Title: "Legacy", Source: "tmdb", TMDbEpisodeCheckedAt: &legacyOnly},
		{ID: "legacy-check-conflict", Kind: model.MetadataKindSeries, Title: "Conflict", Source: "tmdb", TMDbEpisodeCheckedAt: &legacyConflict},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE metadata_items ADD COLUMN tmdb_episode_checked_at timestamptz`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE metadata_items SET tmdb_episode_checked_at = ? WHERE id = ?`, canonicalConflict, "legacy-check-conflict").Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn("metadata_items", "tm_db_episode_checked_at") {
		t.Fatal("legacy tm_db_episode_checked_at column still exists")
	}
	for id, want := range map[string]time.Time{
		"legacy-check-only":     legacyOnly,
		"legacy-check-conflict": canonicalConflict,
	} {
		var got time.Time
		if err := db.Model(&model.MetadataItem{}).Select("tmdb_episode_checked_at").Where("id = ?", id).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Fatalf("metadata %s checked at = %v, want %v", id, got, want)
		}
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}
	if db.Migrator().HasColumn("metadata_items", "tm_db_episode_checked_at") {
		t.Fatal("repeated migration restored legacy tm_db_episode_checked_at column")
	}
}

func TestAutoMigrateBackfillsAndRemovesLegacyEpisodeTitle(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyEpisodeTitleMetadataItem{}); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&legacyEpisodeTitleMetadataItem{}, "episode_title") {
		t.Fatal("legacy episode_title column was not created")
	}
	seriesID := "legacy-title-series"
	seasonID := "legacy-title-season"
	rows := []legacyEpisodeTitleMetadataItem{
		{ID: seriesID, Kind: model.MetadataKindSeries, Title: "Series", EpisodeTitle: "Wrong series value", Source: "tmdb"},
		{ID: seasonID, Kind: model.MetadataKindSeason, ParentID: &seriesID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"},
		{ID: "legacy-title-episode", Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1, Title: "Series", EpisodeTitle: "  Pilot  ", Source: "tmdb"},
		{ID: "legacy-empty-episode-title", Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 2, Title: "Existing title", EpisodeTitle: "   ", Source: "tmdb"},
		{ID: "legacy-null-episode-title", Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 3, Title: "Null title", Source: "tmdb"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&legacyEpisodeTitleMetadataItem{}).Where("id = ?", "legacy-null-episode-title").UpdateColumn("episode_title", nil).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn(&legacyEpisodeTitleMetadataItem{}, "episode_title") {
		t.Fatal("legacy episode_title column still exists")
	}
	for id, want := range map[string]string{
		seriesID:                     "Series",
		"legacy-title-episode":       "Pilot",
		"legacy-empty-episode-title": "Existing title",
		"legacy-null-episode-title":  "Null title",
	} {
		var title string
		if err := db.Model(&model.MetadataItem{}).Select("title").Where("id = ?", id).Scan(&title).Error; err != nil {
			t.Fatal(err)
		}
		if title != want {
			t.Fatalf("metadata %s title = %q, want %q", id, title, want)
		}
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}
	if db.Migrator().HasColumn(&legacyEpisodeTitleMetadataItem{}, "episode_title") {
		t.Fatal("repeated migration restored legacy episode_title column")
	}
	var title string
	if err := db.Model(&model.MetadataItem{}).Select("title").Where("id = ?", "legacy-title-episode").Scan(&title).Error; err != nil {
		t.Fatal(err)
	}
	if title != "Pilot" {
		t.Fatalf("repeated migration changed episode title to %q", title)
	}
}

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

	if err := AutoMigrate(db); err != nil {
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
