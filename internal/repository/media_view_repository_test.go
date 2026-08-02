package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaViewFiltersSortsAndPaginatesBySharedMetadata(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}

	publicMetadata := model.MetadataItem{
		Base: model.Base{ID: "metadata-public"}, Kind: model.MetadataKindMovie,
		Title: "Shared Public", ReleaseDate: "2025-01-01", Year: 2025, Source: "tmdb",
	}
	adultMetadata := model.MetadataItem{
		Base: model.Base{ID: "metadata-adult"}, Kind: model.MetadataKindMovie,
		Title: "Shared Adult", ReleaseDate: "2026-01-01", Year: 2026, NSFW: true, Source: "tmdb",
	}
	if err := repos.DB.Create(&[]model.MetadataItem{publicMetadata, adultMetadata}).Error; err != nil {
		t.Fatal(err)
	}
	media := []model.Media{
		{Base: model.Base{ID: "media-public"}, LibraryID: library.ID, MetadataID: publicMetadata.ID, Title: "Raw Public", Path: "/media/movies/public.mkv"},
		{Base: model.Base{ID: "media-adult"}, LibraryID: library.ID, MetadataID: adultMetadata.ID, Title: "Raw Adult", Path: "/media/movies/adult.mkv"},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	rows, total, err := repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 0, 10, MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != "media-public" || rows[0].Title != "Shared Public" {
		t.Fatalf("public view total=%d rows=%#v", total, rows)
	}

	rows, total, err = repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 0, 1, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 1 || rows[0].ID != "media-adult" || rows[0].Year != 2026 {
		t.Fatalf("first page total=%d rows=%#v", total, rows)
	}
	rows, total, err = repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 1, 1, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 1 || rows[0].ID != "media-public" {
		t.Fatalf("second page total=%d rows=%#v", total, rows)
	}

	rows, total, err = repos.MediaView.SearchFilteredPage(t.Context(), "Shared Adult", 0, 10, MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("hidden NSFW search total=%d rows=%#v", total, rows)
	}
}

func TestMediaViewProjectsEpisodeArtworkAndParentIdentifiers(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}

	series := model.MetadataItem{
		Base: model.Base{ID: "metadata-series"}, Kind: model.MetadataKindSeries,
		Title: "Shared Series", Source: "tmdb",
	}
	seriesID := series.ID
	season := model.MetadataItem{
		Base: model.Base{ID: "metadata-season-1"}, Kind: model.MetadataKindSeason,
		ParentID: &seriesID, SeasonNum: 1, Title: "Season 1", Source: "tmdb",
	}
	seasonID := season.ID
	episodes := []model.MetadataItem{
		{Base: model.Base{ID: "metadata-episode-1"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1, Title: series.Title, EpisodeTitle: "Pilot", Source: "tmdb"},
		{Base: model.Base{ID: "metadata-episode-2"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 2, Title: series.Title, EpisodeTitle: "Second", Source: "tmdb"},
	}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{Base: model.Base{ID: "identifier-tmdb"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "123"},
		{Base: model.Base{ID: "identifier-tmdb-alias"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "456"},
		{Base: model.Base{ID: "identifier-tmdb-movie"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "999"},
		{Base: model.Base{ID: "identifier-douban"}, MetadataID: series.ID, Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "db-123"},
	}
	if err := repos.DB.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}

	assets := []model.ArtworkAsset{
		{Base: model.Base{ID: "asset-poster"}, SHA256: strings.Repeat("1", 64), StorageKey: "poster.jpg", MimeType: "image/jpeg"},
		{Base: model.Base{ID: "asset-backdrop"}, SHA256: strings.Repeat("2", 64), StorageKey: "backdrop.jpg", MimeType: "image/jpeg"},
		{Base: model.Base{ID: "asset-still"}, SHA256: strings.Repeat("3", 64), StorageKey: "still.jpg", MimeType: "image/jpeg"},
	}
	if err := repos.DB.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	artworks := []model.MetadataArtwork{
		{MetadataID: series.ID, AssetID: "asset-poster", ArtworkType: model.ArtworkTypePoster},
		{MetadataID: series.ID, AssetID: "asset-backdrop", ArtworkType: model.ArtworkTypeBackdrop},
		{MetadataID: episodes[0].ID, AssetID: "asset-still", ArtworkType: model.ArtworkTypeStill},
	}
	if err := repos.DB.Create(&artworks).Error; err != nil {
		t.Fatal(err)
	}

	media := []model.Media{
		{Base: model.Base{ID: "media-episode-1"}, LibraryID: library.ID, MetadataID: episodes[0].ID, Title: "Raw Show", Path: "/media/tv/S01E01.mkv", SeasonNum: 1, EpisodeNum: 1},
		{Base: model.Base{ID: "media-episode-2"}, LibraryID: library.ID, MetadataID: episodes[1].ID, Title: "Raw Show", Path: "/media/tv/S01E02.mkv", SeasonNum: 1, EpisodeNum: 2},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	rows, total, err := repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 0, 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("entity-kind-scoped identifiers must not duplicate media views: total=%d rows=%#v", total, rows)
	}

	first, err := repos.MediaView.FindByID(t.Context(), "media-episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.SeriesID != series.ID || first.SeasonID != season.ID || first.Title != series.Title || first.EpisodeTitle != "Pilot" {
		t.Fatalf("episode projection = %#v", first)
	}
	if first.PosterURL != "/api/artwork/asset-poster" || first.BackdropURL != "/api/artwork/asset-still" {
		t.Fatalf("episode artwork poster=%q backdrop=%q", first.PosterURL, first.BackdropURL)
	}
	if first.TMDbID != 123 || first.DoubanID != "db-123" {
		t.Fatalf("parent identifiers tmdb=%d douban=%q", first.TMDbID, first.DoubanID)
	}

	second, err := repos.MediaView.FindByID(t.Context(), "media-episode-2")
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.PosterURL != "/api/artwork/asset-poster" || second.BackdropURL != "/api/artwork/asset-backdrop" {
		t.Fatalf("inherited episode artwork = %#v", second)
	}

	payload, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"season_id", "title", "nsfw", "poster_url", "tmdb_id"} {
		if count := strings.Count(string(payload), `"`+field+`":`); count != 1 {
			t.Fatalf("JSON field %q count=%d payload=%s", field, count, payload)
		}
	}
}

func newMediaViewTestRepositories(t *testing.T) *Container {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}
