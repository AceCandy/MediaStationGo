package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestListMissingCatalogArtworkWithoutMediaTable(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.CatalogHydrationJob{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	series := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000100"}, Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb", CatalogMetadataHydratedAt: &now, CatalogArtworkHydratedAt: &now, CatalogHydratedAt: &now}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000200"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season", Source: "tmdb", CatalogMetadataHydratedAt: &now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000300"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "tmdb", CatalogMetadataHydratedAt: &now}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "100"},
		{MetadataID: season.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: "101"},
		{MetadataID: episode.ID, Provider: "tmdb", EntityKind: model.MetadataKindEpisode, ExternalID: "102"},
	}
	if err := db.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Metadata
	items, err := repo.ListMissingCatalogArtworkAfter(t.Context(), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].MetadataID != season.ID || !items[0].MissingPoster || items[0].MissingBackdrop || items[0].MissingStill ||
		items[1].MetadataID != episode.ID || !items[1].MissingStill || items[1].MissingPoster || items[1].MissingBackdrop {
		t.Fatalf("missing items = %#v", items)
	}
	roots, err := repo.ListMissingCatalogArtworkRootsAfter(t.Context(), "", 20, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].MetadataID != series.ID || roots[0].ExternalID != "100" {
		t.Fatalf("missing roots = %#v", roots)
	}
}

func TestListMissingTMDbSnapshotsWithoutMediaTable(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}); err != nil {
		t.Fatal(err)
	}
	items := []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000100"}, Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000200"}, Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000300"}, Kind: model.MetadataKindSeason, ParentID: &items[1].ID, SeasonNum: 1, Title: "Season", Source: "tmdb"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000400"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "Episode", Source: "tmdb"}
	complete := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000500"}, Kind: model.MetadataKindMovie, Title: "Complete", Source: "tmdb"}
	invalid := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000600"}, Kind: model.MetadataKindMovie, Title: "Invalid", Source: "tmdb"}
	if err := db.Create(&[]model.MetadataItem{episode, complete, invalid}).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{MetadataID: items[0].ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "10"},
		{MetadataID: items[1].ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "20"},
		{MetadataID: season.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: "30"},
		{MetadataID: episode.ID, Provider: "tmdb", EntityKind: model.MetadataKindEpisode, ExternalID: "40"},
		{MetadataID: complete.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "50"},
		{MetadataID: invalid.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "not-a-number"},
	}
	if err := db.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Metadata
	if err := repo.UpsertProviderSnapshot(t.Context(), complete.ID, "tmdb", []byte(`{"id":50}`), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	total, err := repo.CountMissingTMDbSnapshots(t.Context())
	if err != nil || total != 4 {
		t.Fatalf("missing snapshot total = %d, err = %v", total, err)
	}
	first, err := repo.ListMissingTMDbSnapshotsAfter(t.Context(), "", 2)
	if err != nil || len(first) != 2 || first[0].TMDbID != 10 || first[1].TMDbID != 20 {
		t.Fatalf("first page = %#v, err = %v", first, err)
	}
	second, err := repo.ListMissingTMDbSnapshotsAfter(t.Context(), first[1].MetadataID, 2)
	if err != nil || len(second) != 2 || second[0].SeriesTMDbID != 20 || second[0].SeasonNum != 1 || second[1].SeriesTMDbID != 20 || second[1].EpisodeNum != 2 {
		t.Fatalf("second page = %#v, err = %v", second, err)
	}

	if err := repo.ReplaceIdentifierWithSnapshot(t.Context(), items[0].ID, "tmdb", model.MetadataKindMovie, "11", []byte(`{`), time.Now().UTC()); err == nil {
		t.Fatal("invalid snapshot must reject identifier replacement")
	}
	if old, _ := repo.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "10"); old == nil || old.ID != items[0].ID {
		t.Fatalf("old identifier changed after snapshot failure: %#v", old)
	}
	if next, _ := repo.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "11"); next != nil {
		t.Fatalf("new identifier persisted after snapshot failure: %#v", next)
	}
	if err := db.Migrator().DropTable(&model.MetadataProviderSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceIdentifierWithSnapshot(t.Context(), items[0].ID, "tmdb", model.MetadataKindMovie, "12", []byte(`{"id":12}`), time.Now().UTC()); err == nil {
		t.Fatal("snapshot database failure must reject identifier replacement")
	}
	if old, _ := repo.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "10"); old == nil || old.ID != items[0].ID {
		t.Fatalf("old identifier changed after snapshot database failure: %#v", old)
	}
	if next, _ := repo.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "12"); next != nil {
		t.Fatalf("new identifier persisted after snapshot database failure: %#v", next)
	}
}

func TestEnsureCatalogArtworkJobRespectsTerminalState(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.CatalogHydrationJob{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).Metadata
	metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "metadata-1"}, Kind: model.MetadataKindMovie, Title: "Movie", Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	candidate := CatalogArtworkCandidate{MetadataID: metadata.ID, EntityKind: model.MetadataKindMovie, ExternalID: "10"}
	if got, err := repo.EnsureCatalogArtworkJob(t.Context(), candidate, false); err != nil || got != CatalogJobCreated {
		t.Fatalf("create = %q, %v", got, err)
	}
	if err := db.Model(&model.CatalogHydrationJob{}).Where("external_id = ?", "10").Updates(map[string]any{"status": model.CatalogJobStatusCompleted, "attempts": 5}).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := repo.EnsureCatalogArtworkJob(t.Context(), candidate, false); err != nil || got != CatalogJobRequeued {
		t.Fatalf("completed requeue = %q, %v", got, err)
	}
	var job model.CatalogHydrationJob
	if err := db.First(&job, "external_id = ?", "10").Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CatalogJobStatusPending || job.Stage != model.CatalogJobStageRoot || job.Attempts != 0 {
		t.Fatalf("requeued job = %#v", job)
	}
	if err := db.Model(&job).Update("status", model.CatalogJobStatusFailed).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := repo.EnsureCatalogArtworkJob(t.Context(), candidate, false); err != nil || got != CatalogJobUnchanged {
		t.Fatalf("scheduled failed = %q, %v", got, err)
	}
	if got, err := repo.EnsureCatalogArtworkJob(context.Background(), candidate, true); err != nil || got != CatalogJobRequeued {
		t.Fatalf("manual failed = %q, %v", got, err)
	}
	for _, status := range []string{model.CatalogJobStatusPending, model.CatalogJobStatusRunning, model.CatalogJobStatusRetry} {
		next := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
		if err := db.Model(&job).Updates(map[string]any{"status": status, "stage": model.CatalogJobStageSeasons, "attempts": 4, "next_attempt_at": next}).Error; err != nil {
			t.Fatal(err)
		}
		if got, err := repo.EnsureCatalogArtworkJob(t.Context(), candidate, true); err != nil || got != CatalogJobUnchanged {
			t.Fatalf("active %s = %q, %v", status, got, err)
		}
		var unchanged model.CatalogHydrationJob
		if err := db.First(&unchanged, "id = ?", job.ID).Error; err != nil {
			t.Fatal(err)
		}
		if unchanged.Stage != model.CatalogJobStageSeasons || unchanged.Attempts != 4 || unchanged.NextAttemptAt == nil || !unchanged.NextAttemptAt.Equal(next) {
			t.Fatalf("active %s was reset: %#v", status, unchanged)
		}
	}
}

func TestFindIncompleteCatalogChildWaitsForHandoffButNotArtwork(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb", CatalogMetadataHydratedAt: &now, CatalogArtworkHydratedAt: &now, CatalogHydratedAt: &now}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season", Source: "tmdb", CatalogMetadataHydratedAt: &now, CatalogArtworkHydratedAt: &now, CatalogHydratedAt: &now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "tmdb", CatalogMetadataHydratedAt: &now}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	found, err := New(db).Metadata.FindIncompleteCatalogChild(t.Context(), series.ID, model.MetadataKindSeason)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != season.ID {
		t.Fatalf("season with unfinished episode handoff was skipped: %#v", found)
	}
	if err := db.Model(&episode).Updates(map[string]any{"catalog_hydrated_at": now, "catalog_artwork_due_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	found, err = New(db).Metadata.FindIncompleteCatalogChild(t.Context(), series.ID, model.MetadataKindSeason)
	if err != nil || found != nil {
		t.Fatalf("pending image must not block catalog metadata: %#v, %v", found, err)
	}
}

func TestNextCatalogAttemptAtHandlesEmptyAggregate(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CatalogHydrationJob{}); err != nil {
		t.Fatalf("migrate catalog hydration jobs: %v", err)
	}
	repos := New(db)

	assertNext := func(want *time.Time) {
		t.Helper()
		got, err := repos.Metadata.NextCatalogAttemptAt(t.Context())
		if err != nil {
			t.Fatalf("next catalog attempt: %v", err)
		}
		if want == nil {
			if got != nil {
				t.Fatalf("next catalog attempt = %v, want nil", got)
			}
			return
		}
		if got == nil || !got.Equal(*want) {
			t.Fatalf("next catalog attempt = %v, want %v", got, want)
		}
	}

	assertNext(nil)

	jobs := []model.CatalogHydrationJob{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "null-pending", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot},
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "null-retry", Status: model.CatalogJobStatusRetry, Stage: model.CatalogJobStageRoot},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create unscheduled catalog jobs: %v", err)
	}
	assertNext(nil)

	earlier := time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)
	completed := earlier.Add(-time.Hour)
	jobs = []model.CatalogHydrationJob{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "later", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageRoot, NextAttemptAt: &later},
		{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "earlier", Status: model.CatalogJobStatusRetry, Stage: model.CatalogJobStageRoot, NextAttemptAt: &earlier},
		{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "completed", Status: model.CatalogJobStatusCompleted, Stage: model.CatalogJobStageRoot, NextAttemptAt: &completed},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create scheduled catalog jobs: %v", err)
	}
	assertNext(&earlier)
}

func TestListDoubanMovieEnrichmentAfterRefreshesOnlyStaleIncompleteWorks(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.MetadataArtworkCandidate{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	refreshBefore := now.Add(-24 * time.Hour)
	items := []model.MetadataItem{
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000100"}, Kind: model.MetadataKindMovie, Title: "Pending"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000200"}, Kind: model.MetadataKindMovie, Title: "Recent"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000300"}, Kind: model.MetadataKindMovie, Title: "完整标题", Overview: "完整简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000325"}, Kind: model.MetadataKindMovie, Title: "快照缺字段", Overview: "已有简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000350"}, Kind: model.MetadataKindMovie, Title: "旧快照完整标题", Overview: "完整简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000400"}, Kind: model.MetadataKindMovie, Title: "缺海报", Overview: "已有简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000500"}, Kind: model.MetadataKindMovie, Title: "缺简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000600"}, Kind: model.MetadataKindMovie, Title: "English", Overview: "已有简介"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000700"}, Kind: model.MetadataKindSeries, Title: "Series"},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000800"}, Kind: model.MetadataKindMovie, Title: "Ambiguous"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := make([]model.MetadataIdentifier, 0, len(items)+1)
	for i := range items {
		identifiers = append(identifiers, model.MetadataIdentifier{
			MetadataID: items[i].ID, Provider: "douban", EntityKind: items[i].Kind, ExternalID: fmt.Sprintf("%d00", i+1),
		})
	}
	identifiers = append(identifiers, model.MetadataIdentifier{MetadataID: items[9].ID, Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "801"})
	if err := db.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	snapshots := make([]model.MetadataProviderSnapshot, 0, len(items)-1)
	for i := 1; i < len(items)-1; i++ {
		fetchedAt := refreshBefore.Add(-time.Hour)
		payload := `{"title":"移动端详情","intro":"完整简介","cover_url":"https://image.test/poster.jpg"}`
		if i == 1 {
			fetchedAt = now
		}
		if i == 1 || i == 4 {
			payload = `{"subject":{}}`
		}
		if i == 3 {
			payload = `{"title":"移动端缺失详情"}`
		}
		snapshots = append(snapshots, model.MetadataProviderSnapshot{MetadataID: items[i].ID, Provider: "douban", Payload: payload, FetchedAt: fetchedAt})
	}
	if err := db.Create(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, 3, 4, 6, 7} {
		asset := model.ArtworkAsset{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("asset-%d", index)}, SHA256: fmt.Sprintf("sha-%d", index), StorageKey: fmt.Sprintf("test/%d.jpg", index), MimeType: "image/jpeg"}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MetadataArtwork{MetadataID: items[index].ID, AssetID: asset.ID, ArtworkType: model.ArtworkTypePoster, SourceProvider: "douban"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := New(db).Metadata.ListDoubanMovieEnrichmentAfter(t.Context(), "", refreshBefore, 20)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{items[0].ID, items[3].ID, items[4].ID, items[5].ID, items[6].ID, items[7].ID, items[8].ID}
	if len(candidates) != len(wantIDs) {
		t.Fatalf("douban enrichment candidates = %#v", candidates)
	}
	for i := range wantIDs {
		if candidates[i].MetadataID != wantIDs[i] {
			t.Fatalf("douban enrichment candidates = %#v", candidates)
		}
	}
}
