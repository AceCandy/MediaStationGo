package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
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

func TestCatalogHydrationPersistsCompleteSeriesTree(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	scraper.tmdb.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch req.URL.Path {
		case "/tv/12345":
			body = `{"id":12345,"name":"测试剧集","overview":"剧集简介","poster_path":"/series-poster.jpg","backdrop_path":"/series-backdrop.jpg","first_air_date":"2024-01-01","vote_average":8.5,"seasons":[{"id":500,"season_number":0,"name":"特别篇"},{"id":501,"season_number":1,"name":"第一季"}]}`
		case "/tv/12345/season/0":
			body = `{"id":500,"season_number":0,"name":"特别篇","overview":"特别篇简介","poster_path":"/s0.jpg","episodes":[{"id":600,"episode_number":1,"name":"特别集"}]}`
		case "/tv/12345/season/1":
			body = `{"id":501,"season_number":1,"name":"第一季","overview":"第一季简介","poster_path":"/s1.jpg","episodes":[{"id":601,"episode_number":1,"name":"第一集"},{"id":602,"episode_number":2,"name":"第二集"}]}`
		case "/tv/12345/season/0/episode/1":
			body = `{"id":600,"name":"特别集","overview":"特别集简介","still_path":"/e600.jpg","air_date":"2024-01-01","runtime":12,"vote_average":7.1}`
		case "/tv/12345/season/1/episode/1":
			body = `{"id":601,"name":"第一集","overview":"第一集简介","still_path":"/e601.jpg","air_date":"2024-01-08","runtime":25,"vote_average":8.1}`
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
	processCatalogStage(t, scraper, model.CatalogJobStageSeasons)
	processCatalogStage(t, scraper, model.CatalogJobStageSeasons)
	processCatalogStage(t, scraper, model.CatalogJobStageSeasons)

	series, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeries, "12345")
	if err != nil || series == nil {
		t.Fatalf("series = %#v, err = %v", series, err)
	}
	if series.CatalogMetadataHydratedAt == nil || series.CatalogArtworkHydratedAt == nil || series.CatalogHydratedAt == nil {
		t.Fatalf("series checkpoints = %#v", series)
	}
	var seasons []model.MetadataItem
	if err := repos.DB.Where("parent_id = ? AND kind = ?", series.ID, model.MetadataKindSeason).Order("season_num").Find(&seasons).Error; err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 || seasons[0].SeasonNum != 0 {
		t.Fatalf("seasons = %#v, want season zero and season one", seasons)
	}
	var episodes []model.MetadataItem
	if err := repos.DB.Where("kind = ?", model.MetadataKindEpisode).Order("episode_num").Find(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 3 {
		t.Fatalf("episode count = %d, want 3", len(episodes))
	}
	for _, episode := range episodes {
		if episode.CatalogHydratedAt == nil || episode.RuntimeSec <= 0 || episode.EpisodeTitle == "" {
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
	var job model.CatalogHydrationJob
	if err := repos.DB.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CatalogJobStatusCompleted || job.CompletedAt == nil {
		t.Fatalf("job = %#v, want completed", job)
	}
	var mediaCount int64
	if err := repos.DB.Model(&model.Media{}).Count(&mediaCount).Error; err != nil || mediaCount != 0 {
		t.Fatalf("media count = %d, err = %v", mediaCount, err)
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
	switch episode.EpisodeTitle {
	case "特别集":
		return "600"
	case "第一集":
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
