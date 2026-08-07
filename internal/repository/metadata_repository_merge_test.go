package repository

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func newMetadataMergeTestRepository(t *testing.T) *Container {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.ArtworkAsset{}, &model.MetadataArtwork{},
		&model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{},
		&model.Media{}, &model.PlaybackHistory{}, &model.Favorite{}, &model.Playlist{}, &model.PlaylistItem{},
	); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestUpsertCanonicalExplicitMergeMovesMovieReferences(t *testing.T) {
	repos := newMetadataMergeTestRepository(t)
	source := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Local", Source: "local"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "0100"})
	target := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Canonical", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "200"})
	media := []model.Media{
		{Base: model.Base{ID: "media-source"}, MetadataID: source.ID, Title: "Local", Path: "/source.mkv"},
		{Base: model.Base{ID: "media-target"}, MetadataID: target.ID, Title: "Canonical", Path: "/target.mkv"},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	playlist := model.Playlist{UserID: "user", Name: "List"}
	if err := repos.DB.Create(&playlist).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repos.DB.Create(&[]model.Favorite{
		{UserID: "user", MetadataID: source.ID, MediaID: media[0].ID},
		{UserID: "user", MetadataID: target.ID, MediaID: media[1].ID},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&[]model.PlaylistItem{
		{PlaylistID: playlist.ID, MetadataID: source.ID, MediaID: media[0].ID, Position: 1},
		{PlaylistID: playlist.ID, MetadataID: target.ID, MediaID: media[1].ID, Position: 2},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&[]model.PlaybackHistory{
		{UserID: "user", MetadataID: source.ID, MediaID: media[0].ID, PositionMs: 200, WatchedAt: now},
		{UserID: "user", MetadataID: target.ID, MediaID: media[1].ID, PositionMs: 100, WatchedAt: now.Add(-time.Hour)},
	}).Error; err != nil {
		t.Fatal(err)
	}

	saved, err := repos.Metadata.UpsertCanonicalWithMerge(t.Context(),
		&model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Douban", Source: "douban"},
		[]model.MetadataIdentifier{
			{Provider: "TMDB", EntityKind: "MOVIE", ExternalID: "0200"},
			{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "100"},
		}, source.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != target.ID || saved.Title != "Canonical" {
		t.Fatalf("saved metadata = %#v", saved)
	}
	var sourceCount int64
	if err := repos.DB.Unscoped().Model(&model.MetadataItem{}).Where("id = ?", source.ID).Count(&sourceCount).Error; err != nil || sourceCount != 0 {
		t.Fatalf("source metadata remains: count=%d err=%v", sourceCount, err)
	}
	for _, table := range []any{&model.Favorite{}, &model.PlaylistItem{}, &model.PlaybackHistory{}} {
		var count int64
		if err := repos.DB.Model(table).Where("metadata_id = ?", target.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%T target rows=%d err=%v", table, count, err)
		}
	}
	var sourceMedia model.Media
	if err := repos.DB.First(&sourceMedia, "id = ?", media[0].ID).Error; err != nil || sourceMedia.MetadataID != target.ID {
		t.Fatalf("source media not rebound: %#v err=%v", sourceMedia, err)
	}
	var history model.PlaybackHistory
	if err := repos.DB.First(&history, "metadata_id = ?", target.ID).Error; err != nil || history.PositionMs != 200 {
		t.Fatalf("newer history not preserved: %#v err=%v", history, err)
	}
}

func TestUpsertCanonicalReplacesUnownedProviderIdentifier(t *testing.T) {
	repos := newMetadataMergeTestRepository(t)
	series := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "local"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "220269"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "42"})

	saved, err := repos.Metadata.UpsertCanonical(t.Context(),
		&model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"},
		[]model.MetadataIdentifier{{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "296753"}},
		series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != series.ID {
		t.Fatalf("metadata id = %q, want %q", saved.ID, series.ID)
	}
	var identifiers []model.MetadataIdentifier
	if err := repos.DB.Where("metadata_id = ?", series.ID).Order("provider").Find(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 2 || identifiers[0].Provider != "douban" || identifiers[0].ExternalID != "42" ||
		identifiers[1].Provider != "tmdb" || identifiers[1].ExternalID != "296753" {
		t.Fatalf("identifiers = %#v", identifiers)
	}
}

func TestExplicitSeriesMergeRecursivelyMergesSeasonAndEpisode(t *testing.T) {
	repos := newMetadataMergeTestRepository(t)
	sourceSeries := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Local Show", Source: "local"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "300"})
	targetSeries := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Canonical Show", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "400"})
	sourceSeason := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &sourceSeries.ID, SeasonNum: 1, Title: "Season 1", Source: "local"})
	targetSeason := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &targetSeries.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"})
	sourceEpisode := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &sourceSeason.ID, EpisodeNum: 1, Title: "Local Show", Source: "local"})
	targetEpisode := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &targetSeason.ID, EpisodeNum: 1, Title: "Canonical Show", Source: "tmdb"})
	media := []model.Media{
		{MetadataID: sourceEpisode.ID, SeriesID: sourceSeries.ID, Title: "Local Show", Path: "/local-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1},
		{MetadataID: targetEpisode.ID, SeriesID: targetSeries.ID, Title: "Canonical Show", Path: "/canonical-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	saved, err := repos.Metadata.UpsertCanonicalWithMerge(t.Context(),
		&model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Mapped Show", Source: "douban"},
		[]model.MetadataIdentifier{
			{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "400"},
			{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "300"},
		}, sourceSeries.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != targetSeries.ID {
		t.Fatalf("target series = %q, want %q", saved.ID, targetSeries.ID)
	}
	var seasons, episodes, versions int64
	if err := repos.DB.Model(&model.MetadataItem{}).Where("kind = ? AND parent_id = ?", model.MetadataKindSeason, targetSeries.ID).Count(&seasons).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Model(&model.MetadataItem{}).Where("kind = ? AND parent_id = ?", model.MetadataKindEpisode, targetSeason.ID).Count(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Model(&model.Media{}).Where("metadata_id = ?", targetEpisode.ID).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if seasons != 1 || episodes != 1 || versions != 2 {
		t.Fatalf("merged graph seasons=%d episodes=%d versions=%d", seasons, episodes, versions)
	}
}
