package repository

import (
	"context"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

type recordingMetadataSearchBackend struct {
	upserts []MetadataSearchDocument
	deletes []string
}

func (b *recordingMetadataSearchBackend) SearchMetadataIDs(context.Context, string, int, int, MetadataSearchFilter) ([]string, int64, error) {
	return nil, 0, nil
}
func (b *recordingMetadataSearchBackend) PrepareMetadataIndex(context.Context) (string, error) {
	return "metadata-test", nil
}
func (b *recordingMetadataSearchBackend) IndexMetadata(context.Context, string, []MetadataSearchDocument) error {
	return nil
}
func (b *recordingMetadataSearchBackend) ActivateMetadataIndex(context.Context, string) error {
	return nil
}
func (b *recordingMetadataSearchBackend) DiscardMetadataIndex(context.Context, string) error {
	return nil
}
func (b *recordingMetadataSearchBackend) DeleteMetadataFromIndex(context.Context, string, string) error {
	return nil
}
func (b *recordingMetadataSearchBackend) UpsertMetadata(_ context.Context, row MetadataSearchDocument) error {
	b.upserts = append(b.upserts, row)
	return nil
}
func (b *recordingMetadataSearchBackend) DeleteMetadata(_ context.Context, id string) error {
	b.deletes = append(b.deletes, id)
	return nil
}

func TestMetadataSearchCountsPlayableTopLevelWorks(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	library := model.Library{Name: "Search", Path: "/media/search", Type: "mixed", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	movie := createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "search-movie"}, Kind: model.MetadataKindMovie, Title: "Searchable Work", Source: "local"})
	series := createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "search-series"}, Kind: model.MetadataKindSeries, Title: "Searchable Series", Source: "local"})
	seriesID := series.ID
	season := createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "search-season"}, Kind: model.MetadataKindSeason, ParentID: &seriesID, SeasonNum: 1, Title: "Season 1", Source: "local"})
	seasonID := season.ID
	episode := createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "search-episode"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1, Title: "Episode 1", Source: "local"})
	createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "search-empty"}, Kind: model.MetadataKindMovie, Title: "Searchable Empty", Source: "local"})

	rows := []model.Media{
		{Base: model.Base{ID: "movie-1080"}, LibraryID: library.ID, MetadataID: movie.ID, Path: "/media/search/movie-1080.mkv"},
		{Base: model.Base{ID: "movie-4k"}, LibraryID: library.ID, MetadataID: movie.ID, Path: "/media/search/movie-4k.mkv"},
		{Base: model.Base{ID: "episode-media"}, LibraryID: library.ID, MetadataID: episode.ID, Path: "/media/search/episode.mkv"},
		{Base: model.Base{ID: "orphan-media"}, LibraryID: library.ID, Title: "Searchable Orphan", Path: "/media/search/orphan.mkv"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	ids, total, err := repos.MediaView.SearchMetadataIDs(t.Context(), "Searchable", 0, 10, MetadataSearchFilter{
		MediaQueryFilter: MediaQueryFilter{IncludeNSFW: true},
		Kinds:            []string{model.MetadataKindMovie, model.MetadataKindSeries},
		ForcePostgres:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(ids) != 2 {
		t.Fatalf("metadata ids=%#v total=%d, want two logical works", ids, total)
	}
	if ids[0] == "search-empty" || ids[1] == "search-empty" {
		t.Fatalf("metadata without playable Media must not be searchable: %#v", ids)
	}
}

func TestMetadataSearchRefreshesMediaAndMetadataChanges(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	backend := &recordingMetadataSearchBackend{}
	repos.Media.SetSearchBackend(backend)
	library := model.Library{Name: "Search", Path: "/media/search", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	first := createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "metadata-first"}, Kind: model.MetadataKindMovie, Title: "First", Source: "local"})
	second := createTestMetadata(t, repos, model.MetadataItem{Base: model.Base{ID: "metadata-second"}, Kind: model.MetadataKindMovie, Title: "Second", Source: "local"})
	media := model.Media{Base: model.Base{ID: "media-1"}, LibraryID: library.ID, MetadataID: first.ID, Path: "/media/search/movie.mkv", ScrapeStatus: "matched"}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	if len(backend.upserts) == 0 || backend.upserts[len(backend.upserts)-1].ID != first.ID {
		t.Fatalf("new Media did not refresh Metadata: %#v", backend.upserts)
	}
	first.Title = "First Updated"
	if err := repos.Metadata.Update(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if got := backend.upserts[len(backend.upserts)-1]; got.ID != first.ID || got.Title != first.Title {
		t.Fatalf("Metadata update projection = %#v", got)
	}
	media.MetadataID = second.ID
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	if !containsStringValue(backend.deletes, first.ID) || backend.upserts[len(backend.upserts)-1].ID != second.ID {
		t.Fatalf("Media rebind upserts=%#v deletes=%#v", backend.upserts, backend.deletes)
	}
	if err := repos.Media.DeleteByLibrary(t.Context(), library.ID); err != nil {
		t.Fatal(err)
	}
	if !containsStringValue(backend.deletes, second.ID) {
		t.Fatalf("last Media deletion did not remove Metadata document: %#v", backend.deletes)
	}
}

func newMetadataSearchTestRepositories(t *testing.T) *Container {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
