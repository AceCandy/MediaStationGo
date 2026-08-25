package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestCatalogHydrationQueuePersistsUniqueValidJobs(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	items := []ExternalMediaResult{
		{Source: "tmdb", MediaType: "movie", TMDbID: 10},
		{Source: "TMDB", MediaType: "movie", TMDbID: 10},
		{Source: "tmdb", MediaType: "tv", TMDbID: 10},
		{Source: "douban", MediaType: "movie", TMDbID: 20},
		{Source: "tmdb", MediaType: "movie", TMDbID: 0},
		{Source: "tmdb", MediaType: "person", TMDbID: 30},
	}
	if err := scraper.QueueCatalogHydrationContext(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	if err := scraper.QueueCatalogHydrationContext(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	var jobs []model.CatalogHydrationJob
	if err := repos.DB.Order("entity_kind").Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].ExternalID != "10" || jobs[1].ExternalID != "10" {
		t.Fatalf("jobs = %#v, want one movie and one series job", jobs)
	}
}

func TestCatalogHydrationWorkerStopsOnCancel(t *testing.T) {
	scraper, _, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	ctx, cancel := context.WithCancel(t.Context())
	scraper.StartCatalogHydrationWorker(ctx)
	cancel()
	scraper.WaitCatalogHydrationWorker()
}

func TestCatalogHydrationRecoveryAndBoundedRootPriority(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	for id := 1; id <= catalogRootBurst+1; id++ {
		if err := repos.Metadata.EnqueueCatalogJob(t.Context(), "tmdb", model.MetadataKindMovie, strconv.Itoa(id)); err != nil {
			t.Fatal(err)
		}
	}
	seasonJob := model.CatalogHydrationJob{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "99", Status: model.CatalogJobStatusPending, Stage: model.CatalogJobStageSeasons}
	if err := repos.DB.Create(&seasonJob).Error; err != nil {
		t.Fatal(err)
	}
	rootStreak := 0
	for i := 0; i < catalogRootBurst; i++ {
		job, err := scraper.claimNextCatalogJob(t.Context(), &rootStreak)
		if err != nil || job == nil || job.Stage != model.CatalogJobStageRoot {
			t.Fatalf("root claim %d = %#v, err = %v", i, job, err)
		}
	}
	job, err := scraper.claimNextCatalogJob(t.Context(), &rootStreak)
	if err != nil || job == nil || job.ID != seasonJob.ID {
		t.Fatalf("bounded priority did not select season job: %#v, err = %v", job, err)
	}
	if err := repos.Metadata.RecoverCatalogJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	var recovered model.CatalogHydrationJob
	if err := repos.DB.First(&recovered, "id = ?", seasonJob.ID).Error; err != nil {
		t.Fatal(err)
	}
	if recovered.Status != model.CatalogJobStatusRetry || recovered.NextAttemptAt == nil {
		t.Fatalf("recovered job = %#v", recovered)
	}
	claimed, err := repos.Metadata.ClaimCatalogJob(t.Context(), model.CatalogJobStageSeasons, time.Now().UTC().Add(time.Second))
	if err != nil || claimed == nil || claimed.ID != seasonJob.ID {
		t.Fatalf("recovered job was not claimable: %#v, err = %v", claimed, err)
	}
}

func TestCatalogHydrationDoesNotSkipLegacyRootOnlySeries(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	legacyCompletedAt := time.Now().UTC().Add(-time.Hour)
	series := model.MetadataItem{
		Kind: model.MetadataKindSeries, Title: "旧版剧集", Source: "tmdb",
		CatalogHydratedAt: &legacyCompletedAt,
	}
	if err := repos.Metadata.Create(t.Context(), &series, []model.MetadataIdentifier{{
		Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "54321",
	}}); err != nil {
		t.Fatal(err)
	}
	scraper.tmdb.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch req.URL.Path {
		case "/tv/54321":
			body = `{"id":54321,"name":"旧版剧集","seasons":[{"id":700,"season_number":1,"name":"第一季"}]}`
		case "/tv/54321/season/1":
			body = `{"id":700,"season_number":1,"name":"第一季","episodes":[]}`
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	if err := scraper.QueueCatalogHydrationContext(t.Context(), []ExternalMediaResult{{Source: "tmdb", MediaType: "tv", TMDbID: 54321}}); err != nil {
		t.Fatal(err)
	}
	processCatalogStage(t, scraper, model.CatalogJobStageRoot)

	seriesAfter, err := repos.Metadata.FindByID(t.Context(), series.ID)
	if err != nil || seriesAfter == nil {
		t.Fatalf("legacy series = %#v, err = %v", seriesAfter, err)
	}
	if seriesAfter.CatalogMetadataHydratedAt == nil || seriesAfter.CatalogArtworkHydratedAt == nil || seriesAfter.CatalogHydratedAt == nil {
		t.Fatalf("legacy series own checkpoints = %#v", seriesAfter)
	}
	var seasons []model.MetadataItem
	if err := repos.DB.Where("parent_id = ? AND kind = ?", series.ID, model.MetadataKindSeason).Find(&seasons).Error; err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 1 || seasons[0].SeasonNum != 1 {
		t.Fatalf("legacy series seasons = %#v, want season one shell", seasons)
	}
	var snapshot model.MetadataProviderSnapshot
	if err := repos.DB.Where("metadata_id = ? AND provider = ?", series.ID, "tmdb").First(&snapshot).Error; err != nil {
		t.Fatalf("legacy series snapshot: %v", err)
	}
}

func TestCatalogHydrationPersistsCompleteSeriesTree(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	scraper.tmdb.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch req.URL.Path {
		case "/tv/12345":
			body = `{"id":12345,"name":"测试剧集","overview":"剧集简介","poster_path":"/series-poster.jpg","backdrop_path":"/series-backdrop.jpg","first_air_date":"2024-01-01","vote_average":8.5,"seasons":[{"id":500,"season_number":0,"name":"特别篇","overview":"清单特别篇简介"},{"id":501,"season_number":1,"name":"第一季","overview":"清单第一季简介"}]}`
		case "/tv/12345/season/0":
			body = `{"id":500,"season_number":0,"name":"特别篇","overview":"特别篇简介","poster_path":"/s0.jpg","episodes":[{"id":600,"episode_number":1,"name":"特别集"}]}`
		case "/tv/12345/season/1":
			body = `{"id":501,"season_number":1,"name":"第 1 季","overview":"","poster_path":"/s1.jpg","translations":{"translations":[{"iso_3166_1":"US","iso_639_1":"en","data":{"name":"The First Mission","overview":"Season one overview"}}]},"episodes":[{"id":601,"episode_number":1,"name":"第 1 集"},{"id":602,"episode_number":2,"name":"第二集"}]}`
		case "/tv/12345/season/0/episode/1":
			body = `{"id":600,"name":"特别集","overview":"特别集简介","still_path":"/e600.jpg","air_date":"2024-01-01","runtime":12,"vote_average":7.1}`
		case "/tv/12345/season/1/episode/1":
			body = `{"id":601,"name":"第 1 集","overview":"","still_path":"/e601.jpg","air_date":"2024-01-08","runtime":25,"vote_average":8.1,"translations":{"translations":[{"iso_3166_1":"US","iso_639_1":"en","data":{"name":"Inside S1 E1","overview":"Episode one overview"}}]}}`
		case "/tv/12345/season/1/episode/2":
			body = `{"id":602,"name":"第二集","overview":"第二集简介","still_path":"/e602.jpg","air_date":"2024-01-15","runtime":26,"vote_average":8.2}`
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	if err := scraper.QueueCatalogHydrationContext(t.Context(), []ExternalMediaResult{{Source: "tmdb", MediaType: "tv", TMDbID: 12345}}); err != nil {
		t.Fatal(err)
	}
	processCatalogStage(t, scraper, model.CatalogJobStageRoot)
	rootSeries, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, "12345")
	if err != nil || rootSeries == nil {
		t.Fatalf("root series = %#v, err = %v", rootSeries, err)
	}
	var shells []model.MetadataItem
	if err := repos.DB.Where("parent_id = ? AND kind = ?", rootSeries.ID, model.MetadataKindSeason).Order("season_num").Find(&shells).Error; err != nil {
		t.Fatal(err)
	}
	series, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, "12345")
	if err != nil || series == nil {
		t.Fatalf("series = %#v, err = %v", series, err)
	}
	if series.CatalogMetadataHydratedAt == nil || series.CatalogArtworkHydratedAt == nil || series.CatalogHydratedAt == nil {
		t.Fatalf("series checkpoints = %#v", series)
	}
	var job model.CatalogHydrationJob
	if err := repos.DB.Where("provider = ? AND entity_kind = ? AND external_id = ?", "tmdb", model.MetadataKindSeries, "12345").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CatalogJobStatusCompleted {
		t.Fatalf("series job status = %q, want completed without an intermediate requeue", job.Status)
	}
	var seasons []model.MetadataItem
	if err := repos.DB.Where("parent_id = ? AND kind = ?", series.ID, model.MetadataKindSeason).Order("season_num").Find(&seasons).Error; err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 || seasons[0].SeasonNum != 0 {
		t.Fatalf("seasons = %#v, want season zero and season one", seasons)
	}
	if seasons[0].Overview != "特别篇简介" || seasons[1].Title != "The First Mission" || seasons[1].Overview != "Season one overview" {
		t.Fatalf("season localization = %#v", seasons[1])
	}
	var episodes []model.MetadataItem
	if err := repos.DB.Where("kind = ?", model.MetadataKindEpisode).Order("episode_num").Find(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 3 {
		t.Fatalf("episode count = %d, want 3", len(episodes))
	}
	for _, episode := range episodes {
		if episode.CatalogHydratedAt == nil || episode.RuntimeSec <= 0 || episode.Title == "" || episode.Title == series.Title {
			t.Fatalf("incomplete episode = %#v", episode)
		}
		if id, findErr := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindEpisode, catalogExternalIDForEpisode(episode)); findErr != nil || id == nil || id.ID != episode.ID {
			t.Fatalf("episode tmdb identity missing for %#v: %#v %v", episode, id, findErr)
		}
		asset, findErr := repos.Artwork.FindSelection(t.Context(), episode.ID, model.ArtworkTypeStill)
		if findErr != nil || asset == nil {
			t.Fatalf("episode still missing for %s: %#v %v", episode.ID, asset, findErr)
		}
	}
	wantEpisodeOverviews := map[string]string{
		"特别集":          "特别集简介",
		"Inside S1 E1": "Episode one overview",
		"第二集":          "第二集简介",
	}
	for _, episode := range episodes {
		if episode.Overview != wantEpisodeOverviews[episode.Title] {
			t.Fatalf("episode own overview = %#v", episode)
		}
	}
	var snapshots []model.MetadataProviderSnapshot
	if err := repos.DB.Find(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 6 {
		t.Fatalf("snapshot count = %d, want 6", len(snapshots))
	}
	for _, snapshot := range snapshots {
		if !json.Valid([]byte(snapshot.Payload)) {
			t.Fatalf("invalid snapshot for %s", snapshot.MetadataID)
		}
	}
	if job.CompletedAt == nil {
		t.Fatalf("job = %#v, want completed_at", job)
	}
	var mediaCount int64
	if err := repos.DB.Model(&model.Media{}).Count(&mediaCount).Error; err != nil || mediaCount != 0 {
		t.Fatalf("media count = %d, err = %v", mediaCount, err)
	}
}

func TestLocalizeTMDbCatalogSnapshotsBackfillsProviderOwnedChildren(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "测试剧集", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "第 1 季", Source: "tmdb"}
	if err := repos.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "第 1 集", Source: "tmdb"}
	manualEpisode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "手工标题", Source: "manual"}
	if err := repos.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&manualEpisode).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seasonPayload := json.RawMessage(`{"name":"第 1 季","overview":"","season_number":1,"translations":{"translations":[{"iso_3166_1":"US","iso_639_1":"en","data":{"name":"First Contact","overview":"Season overview"}}]}}`)
	episodePayload := json.RawMessage(`{"name":"第 1 集","overview":"","episode_number":1,"translations":{"translations":[{"iso_3166_1":"US","iso_639_1":"en","data":{"name":"Arrival","overview":"Episode overview"}}]}}`)
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), season.ID, "tmdb", seasonPayload, now); err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), episode.ID, "tmdb", episodePayload, now); err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), manualEpisode.ID, "tmdb", episodePayload, now); err != nil {
		t.Fatal(err)
	}

	if err := scraper.localizeTMDbCatalogSnapshots(t.Context()); err != nil {
		t.Fatal(err)
	}
	seasonAfter, _ := repos.Metadata.FindByID(t.Context(), season.ID)
	episodeAfter, _ := repos.Metadata.FindByID(t.Context(), episode.ID)
	manualAfter, _ := repos.Metadata.FindByID(t.Context(), manualEpisode.ID)
	if seasonAfter == nil || seasonAfter.Title != "First Contact" || seasonAfter.Overview != "Season overview" {
		t.Fatalf("season backfill = %#v", seasonAfter)
	}
	if episodeAfter == nil || episodeAfter.Title != "Arrival" || episodeAfter.Overview != "Episode overview" {
		t.Fatalf("episode backfill = %#v", episodeAfter)
	}
	if manualAfter == nil || manualAfter.Title != "手工标题" {
		t.Fatalf("manual metadata was overwritten: %#v", manualAfter)
	}

	updatedAt := episodeAfter.UpdatedAt
	if err := scraper.localizeTMDbCatalogSnapshots(t.Context()); err != nil {
		t.Fatal(err)
	}
	episodeAfter, _ = repos.Metadata.FindByID(t.Context(), episode.ID)
	if episodeAfter == nil || !episodeAfter.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("idempotent backfill changed updated_at: before=%v after=%#v", updatedAt, episodeAfter)
	}
}

func TestLocalizeTMDbCatalogSnapshotsSkipsBadItemAndContinuesNextPage(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	series := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "backfill-series"}, Kind: model.MetadataKindSeries, Title: "Backfill Series", Source: "tmdb"}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "backfill-season"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}

	items := make([]model.MetadataItem, 0, catalogSnapshotLocalizationBatchSize+1)
	snapshots := make([]model.MetadataProviderSnapshot, 0, catalogSnapshotLocalizationBatchSize+1)
	now := time.Now().UTC()
	for i := 0; i <= catalogSnapshotLocalizationBatchSize; i++ {
		suffix := strconv.Itoa(1000 + i)
		episodeNum := i + 1
		metadataID := "backfill-episode-" + suffix
		items = append(items, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: metadataID}, Kind: model.MetadataKindEpisode, ParentID: &season.ID,
			EpisodeNum: episodeNum, Title: "第 " + strconv.Itoa(episodeNum) + " 集", Source: "tmdb",
		})
		payload := `{"name":"Episode ` + strconv.Itoa(episodeNum) + `","translations":{"translations":[{"iso_3166_1":"US","iso_639_1":"en","data":{"name":"Localized ` + strconv.Itoa(episodeNum) + `"}}]}}`
		if i == 0 {
			payload = `{"name":123}`
		}
		snapshots = append(snapshots, model.MetadataProviderSnapshot{
			PermanentBase: model.PermanentBase{ID: "backfill-snapshot-" + suffix}, MetadataID: metadataID,
			Provider: "tmdb", Payload: payload, FetchedAt: now,
		})
	}
	if err := repos.DB.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&snapshots).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.localizeTMDbCatalogSnapshots(t.Context()); err != nil {
		t.Fatal(err)
	}
	badAfter, _ := repos.Metadata.FindByID(t.Context(), "backfill-episode-1000")
	lastAfter, _ := repos.Metadata.FindByID(t.Context(), "backfill-episode-1200")
	if badAfter == nil || badAfter.Title != "第 1 集" {
		t.Fatalf("bad snapshot metadata changed: %#v", badAfter)
	}
	if lastAfter == nil || lastAfter.Title != "Localized 201" {
		t.Fatalf("backfill did not continue to the next page: %#v", lastAfter)
	}
}

func TestPersistCatalogArtworkRetriesAfterImageNegativeCache(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	item := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Retry Artwork", Source: "tmdb"}
	if err := repos.DB.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	source := "https://images.example.test/images/retry-poster.png"
	_, _, failPath := scraper.images.remoteImageCachePathsForValidated(source)
	if err := os.MkdirAll(filepath.Dir(failPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}
	imageData := testArtworkPNG(t, 2, 2)
	var calls atomic.Int32
	scraper.images.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(imageData)),
			Request:    req,
		}, nil
	})}

	if err := scraper.persistCatalogArtwork(t.Context(), item.ID, map[string]string{model.ArtworkTypePoster: source}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 after clearing negative cache", got)
	}
	asset, err := repos.Artwork.FindSelection(t.Context(), item.ID, model.ArtworkTypePoster)
	if err != nil || asset == nil {
		t.Fatalf("catalog poster selection = %#v, err = %v", asset, err)
	}
}

func processCatalogStage(t *testing.T, scraper *ScraperService, stage string) {
	t.Helper()
	job, err := scraper.repo.Metadata.ClaimCatalogJob(t.Context(), stage, time.Now().UTC())
	if err != nil || job == nil {
		t.Fatalf("claim %s job = %#v, err = %v", stage, job, err)
	}
	if err := scraper.processCatalogJob(t.Context(), job); err != nil {
		t.Fatalf("process %s job: %v", stage, err)
	}
}

func catalogExternalIDForEpisode(episode model.MetadataItem) string {
	switch episode.Title {
	case "特别集":
		return "600"
	case "Inside S1 E1":
		return "601"
	default:
		return "602"
	}
}

func TestSanitizeCatalogErrorRemovesQuery(t *testing.T) {
	err := sanitizeCatalogError(errors.New("download https://example.test/image.jpg?token=secret&x=1 failed"))
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "?") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("sanitized error leaked query: %v", err)
	}
}
