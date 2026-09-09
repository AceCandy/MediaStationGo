package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

type recordingMetadataSearchBackend struct {
	upserts   []MetadataSearchDocument
	deletes   []string
	indexed   []MetadataSearchDocument
	activated bool
	discarded bool
	onIndex   func()
}

func (b *recordingMetadataSearchBackend) SearchMetadataIDs(context.Context, string, int, int, MetadataSearchFilter) ([]string, int64, error) {
	return nil, 0, nil
}
func (b *recordingMetadataSearchBackend) PrepareMetadataIndex(context.Context) (string, error) {
	return "metadata-test", nil
}
func (b *recordingMetadataSearchBackend) IndexMetadata(_ context.Context, _ string, documents []MetadataSearchDocument) error {
	b.indexed = append(b.indexed, documents...)
	if b.onIndex != nil {
		b.onIndex()
	}
	return nil
}
func (b *recordingMetadataSearchBackend) ActivateMetadataIndex(context.Context, string) error {
	b.activated = true
	return nil
}
func (b *recordingMetadataSearchBackend) DiscardMetadataIndex(context.Context, string) error {
	b.discarded = true
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

func searchPlayableWorksFixture(t *testing.T) *Container {
	t.Helper()
	repos := newMetadataSearchTestRepositories(t)
	library := model.Library{Name: "Search", Path: "/media/search", Type: "mixed", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	movie := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "search-movie"}, Kind: model.MetadataKindMovie, Title: "Searchable Work", Source: "local"})
	series := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "search-series"}, Kind: model.MetadataKindSeries, Title: "Searchable Series", Source: "local"})
	seriesID := series.ID
	season := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "search-season"}, Kind: model.MetadataKindSeason, ParentID: &seriesID, SeasonNum: 1, Title: "Season 1", Source: "local"})
	seasonID := season.ID
	episode := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "search-episode"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1, Title: "Episode 1", Source: "local"})
	createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "search-empty"}, Kind: model.MetadataKindMovie, Title: "Searchable Empty", Source: "local"})

	rows := []model.Media{
		{PermanentBase: model.PermanentBase{ID: "movie-1080"}, LibraryID: library.ID, MetadataID: movie.ID, Path: "/media/search/movie-1080.mkv"},
		{PermanentBase: model.PermanentBase{ID: "movie-4k"}, LibraryID: library.ID, MetadataID: movie.ID, Path: "/media/search/movie-4k.mkv"},
		{PermanentBase: model.PermanentBase{ID: "episode-media"}, LibraryID: library.ID, MetadataID: episode.ID, Path: "/media/search/episode.mkv"},
		{PermanentBase: model.PermanentBase{ID: "orphan-media"}, LibraryID: library.ID, Title: "Searchable Orphan", Path: "/media/search/orphan.mkv"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return repos
}

func TestMetadataSearchCountsPlayableTopLevelWorks(t *testing.T) {
	repos := searchPlayableWorksFixture(t)
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

func TestMetadataSearchBackfillSkipsEmptyCandidates(t *testing.T) {
	repos := searchPlayableWorksFixture(t)
	backend := &recordingMetadataSearchBackend{}
	repos.MediaView.SetSearchBackend(backend)
	indexed, err := repos.MediaView.BackfillSearchIndex(t.Context(), 1, 0)
	if err != nil || indexed != 2 || len(backend.indexed) != 2 || !backend.activated {
		t.Fatalf("backfill did not skip empty first batch and continue: indexed=%d documents=%v err=%v", indexed, backend.indexed, err)
	}
	if backend.indexed[0].ID != "search-movie" || backend.indexed[1].ID != "search-series" {
		t.Fatalf("unexpected backfill documents: %v", backend.indexed)
	}
}

func TestMetadataSearchDocumentsWithManyUnplayableEpisodes(t *testing.T) {
	repos := searchPlayableWorksFixture(t)
	ids := []string{"search-movie", "search-series", "search-empty"}
	var librarySQL string
	if err := repos.DB.Callback().Row().After("gorm:row").Register("test:search-library-query", func(db *gorm.DB) {
		if sql := db.Statement.SQL.String(); strings.Contains(sql, "AS episode_media") {
			librarySQL = sql
		}
	}); err != nil {
		t.Fatal(err)
	}
	want, err := repos.MediaView.metadataSearchDocuments(t.Context(), ids)
	if err != nil || len(want) != 2 || strings.Contains(librarySQL, "OFFSET 0") {
		t.Fatalf("small batch: documents=%v sql=%s err=%v", want, librarySQL, err)
	}
	seasonID := "search-season"
	episodes := make([]model.MetadataItem, 1025)
	for i := range episodes {
		episodes[i] = model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: fmt.Sprintf("empty-episode-%04d", i)},
			Kind:          model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: i + 2,
			Title: "Empty episode", Source: "local",
		}
	}
	if err := repos.DB.CreateInBatches(&episodes, 500).Error; err != nil {
		t.Fatal(err)
	}
	got, err := repos.MediaView.metadataSearchDocuments(t.Context(), ids)
	if err != nil || !reflect.DeepEqual(got, want) || !strings.Contains(librarySQL, "OFFSET 0") {
		t.Fatalf("sparse series changed documents: got=%v want=%v sql=%s err=%v", got, want, librarySQL, err)
	}
}

func TestMetadataSearchBackfillPauseAndCancellation(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	for _, id := range []string{"empty-a", "empty-b"} {
		createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: model.MetadataKindMovie, Title: id, Source: "local"})
	}
	ids, err := repos.MediaView.metadataSearchDocumentIDs(t.Context(), "", 1)
	if err != nil || !reflect.DeepEqual(ids, []string{"empty-a"}) {
		t.Fatalf("candidate page=%v err=%v", ids, err)
	}
	var batches []time.Time
	backend := &recordingMetadataSearchBackend{onIndex: func() { batches = append(batches, time.Now()) }}
	repos.MediaView.SetSearchBackend(backend)
	const pause = 10 * time.Millisecond
	if total, err := repos.MediaView.BackfillSearchIndex(t.Context(), 1, pause); err != nil || total != 0 || !backend.activated {
		t.Fatalf("empty-document rebuild: total=%d err=%v", total, err)
	}
	if len(batches) != 2 || batches[1].Sub(batches[0]) < pause {
		t.Fatalf("batch pause missing: %v", batches)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend = &recordingMetadataSearchBackend{onIndex: cancel}
	repos.MediaView.SetSearchBackend(backend)
	started := time.Now()
	_, err = repos.MediaView.BackfillSearchIndex(ctx, 1, time.Hour)
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second || !backend.discarded || backend.activated || repos.MediaView.searchRebuild {
		t.Fatalf("cancel did not discard unfinished rebuild promptly: err=%v backend=%+v", err, backend)
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
	first := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "metadata-first"}, Kind: model.MetadataKindMovie, Title: "First", Source: "local"})
	second := createTestMetadata(t, repos, model.MetadataItem{PermanentBase: model.PermanentBase{ID: "metadata-second"}, Kind: model.MetadataKindMovie, Title: "Second", Source: "local"})
	media := model.Media{PermanentBase: model.PermanentBase{ID: "media-1"}, LibraryID: library.ID, MetadataID: first.ID, Path: "/media/search/movie.mkv", ScrapeStatus: "matched"}
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
