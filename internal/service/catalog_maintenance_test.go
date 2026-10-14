package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestCatalogMaintenanceCandidatesAndRatingIsolation(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"被改变的名称","intro":"新正文","rating":{"value":9.1}}`), nil
	})
	ctx := t.Context()
	old := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	raw := `{"id":"100","type":"movie","title":"完整电影","intro":"原简介","pic":{"large":"https://example.com/poster.jpg"},"rating":{"value":8.2}}`
	if err := repos.Douban.Save(ctx, "100", "movie", []byte(raw), model.DoubanQualityComplete, old); err != nil {
		t.Fatal(err)
	}
	before, _ := repos.Douban.Find(ctx, "100", "movie")
	rows, err := repos.Douban.MaintenanceCandidates(ctx, "movie", false, old.Add(time.Hour), time.Now(), 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("complete selected: %v %v", rows, err)
	}
	rows, err = repos.Douban.MaintenanceCandidates(ctx, "series", true, time.Now(), time.Now(), 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("kind leaked: %v %v", rows, err)
	}
	if saved, err := provider.refreshRating(ctx, "100", "movie"); err != nil || !saved {
		t.Fatalf("rating: %v %v", saved, err)
	}
	after, _ := repos.Douban.Find(ctx, "100", "movie")
	if after.Rating != 9.1 || !reflect.DeepEqual(before.Payload, after.Payload) || !reflect.DeepEqual(before.FetchedAt, after.FetchedAt) || after.RatingCheckedAt == nil || !after.RatingCheckedAt.After(old) {
		t.Fatalf("rating isolation: %+v", after)
	}
	if saved, err := repos.Douban.SaveRating(ctx, before, []byte(`{"rating":1}`), time.Now()); err != nil || saved {
		t.Fatalf("stale rating accepted: %v %v", saved, err)
	}
	if saved, err := repos.Douban.SaveRating(ctx, after, []byte(`{"rating":0}`), time.Now()); err != nil || !saved {
		t.Fatalf("empty rating observation: %v %v", saved, err)
	}
	after, _ = repos.Douban.Find(ctx, "100", "movie")
	if after.Rating != 9.1 {
		t.Fatal("zero erased rating")
	}
	for _, row := range []model.TMDbSubject{
		{Kind: "movie", TMDbID: 1, Quality: 2, Title: "完整电影", Overview: "简介", Fields: `{"poster_path":"/p.jpg"}`},
		{Kind: "movie", TMDbID: 2, Quality: 1, Title: "摘要已有中文名称", Overview: "简介", Fields: `{"poster_path":"/p.jpg"}`},
		{Kind: "series", TMDbID: 3, Title: "完整剧集", Overview: "简介", Fields: `{"poster_path":"/p.jpg"}`, Status: "Returning Series"},
	} {
		if err := repos.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	tmdbRows, err := repos.TMDb.MaintenanceCandidates(ctx, "movie", false, 100)
	if err != nil || len(tmdbRows) != 1 || tmdbRows[0].TMDbID != 2 {
		t.Fatalf("tmdb completeness: %+v %v", tmdbRows, err)
	}
	tmdbRows, err = repos.TMDb.MaintenanceCandidates(ctx, "", true, 100)
	if err != nil || len(tmdbRows) != 1 || tmdbRows[0].TMDbID != 3 {
		t.Fatalf("complete active inventory: %+v %v", tmdbRows, err)
	}
}

func TestTMDbSourceRecheckBatchAndSelfUpdate(t *testing.T) {
	calls := 0
	center, provider := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.Contains(r.URL.Path, "/episode/") {
			t.Errorf("single episode request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":200,"external_ids":{},"credits":{"cast":[],"crew":[]},"translations":{"translations":[]},"videos":{"results":[]},"season_number":1,"name":"第一季","overview":"季简介","poster_path":"/p.jpg","episodes":[{"id":201,"season_number":1,"episode_number":1,"name":"新的开始","overview":"集简介","air_date":"2025-01-01","still_path":"/s.jpg"},{"id":202,"season_number":1,"episode_number":2,"name":"再次出发","overview":"集简介","air_date":"2025-01-02","still_path":"/s.jpg"}]}`)
	})
	db := center.repo.DB
	if err := db.AutoMigrate(&model.Media{}, &model.MetadataIndexJob{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckSeasonLease{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckScan{}, &model.TMDbRecheckAssetChange{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureTMDbRecheckTriggers(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureMetadataIndexTriggers(db); err != nil {
		t.Fatal(err)
	}
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "本地剧集"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "42"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1})
	var ids []string
	for i := 1; i <= 2; i++ {
		episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: i, Title: "本地旧标题"})
		ids = append(ids, episode.ID)
		if err := db.Create(&model.Media{MetadataID: episode.ID, Path: fmt.Sprintf("/test/e%d.mkv", i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	s := &ScraperService{repo: center.repo, tmdb: provider}
	budget := &tmdbBudget{limit: 10}
	ctx := context.WithValue(t.Context(), tmdbBudgetKey{}, budget)
	ctx = context.WithValue(ctx, tmdbAutomaticKey{}, true)
	metrics := map[string]int64{}
	if err := s.runTMDbSourceRechecks(ctx, "episode", 10, metrics, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || metrics["updated"] != 2 {
		t.Fatalf("batch calls=%d metrics=%v", calls, metrics)
	}
	for _, id := range ids {
		var job model.TMDbRecheckJob
		if err := db.First(&job, "metadata_id=?", id).Error; err != nil || job.Status != "done" || job.Attempts != 0 {
			t.Fatalf("source self conflict: %+v %v", job, err)
		}
		var item model.MetadataItem
		if err := db.First(&item, "id=?", id).Error; err != nil || item.Title != "" {
			t.Fatalf("source mutated local title: %+v %v", item, err)
		}
	}
	if err := s.runTMDbSourceRechecks(ctx, "episode", 10, map[string]int64{}, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("complete sources refreshed: %d", calls)
	}
}

func TestCatalogMaintenanceMissingCredentialsIsNotSuccess(t *testing.T) {
	center, provider := tmdbCenterTest(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	provider.cfg.Secrets.TMDbAPIKey = ""
	row := model.TMDbSubject{Kind: "movie", TMDbID: 1, Quality: 2, Title: "缺资料电影"}
	if err := center.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	metrics := map[string]int64{}
	if err := center.maintainSubjects(t.Context(), catalogMaintenanceTask{kind: "movie"}, 100, metrics); err == nil {
		t.Fatal("missing credentials reported success")
	}
	if metrics["updated"] != 0 || metrics["failed"] != 1 {
		t.Fatal(metrics)
	}
	if err := center.repo.DB.First(&row, "id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.NextMaintenanceAt == nil || row.NextMaintenanceAt.After(time.Now().Add(6*time.Minute)) {
		t.Fatalf("long cooldown: %+v", row.NextMaintenanceAt)
	}
}

func TestSourceImagesRetryDoesNotRefreshDetails(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(*http.Request) (*http.Response, error) {
		t.Error("image task requested detail")
		return nil, errors.New("unexpected detail")
	})
	calls := 0
	store := newDoubanPosterTestStore(t, repos, func(req *http.Request) (*http.Response, error) { calls++; return posterResponse(req, 503, nil), nil })
	old := time.Now().UTC().Add(-time.Hour)
	subject := saveDoubanPosterSubject(t, repos, "https://img1.doubanio.com/view/photo/l/public/failure.png", old)
	s := &SchedulerService{repo: repos, scraper: &ScraperService{repo: repos, douban: provider, artwork: store}}
	metrics := map[string]int64{}
	if err := s.maintainSourceImages(t.Context(), TaskKindDoubanImages, metrics); err == nil {
		t.Fatal("failure hidden")
	}
	after, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || after.PosterRetryAt == nil || !after.PosterRetryAt.After(time.Now()) || !after.FetchedAt.Equal(*subject.FetchedAt) {
		t.Fatalf("image failure touched details: %+v %v", after, err)
	}
	beforeCalls := calls
	if err := s.maintainSourceImages(t.Context(), TaskKindDoubanImages, map[string]int64{}); err != nil || calls != beforeCalls {
		t.Fatalf("retry ignored: %d %d %v", beforeCalls, calls, err)
	}
}

func TestRetiredCatalogMaintenanceCannotRun(t *testing.T) {
	for _, key := range []string{TaskKindTMDbDataRefresh, TaskKindDoubanDataRefresh, TaskDefinitionTMDbEpisodeMetadataRecheck, TaskDefinitionTMDbArtworkLocalRepair, TaskDefinitionTMDbArtworkMissingRecheck, TaskDefinitionDoubanArtworkLocalRepair, TaskDefinitionDoubanEnrichment, TaskDefinitionSeriesLocalCorrection} {
		if _, ok := TaskDefinitionSchedulerJob(key); ok {
			t.Fatalf("retired task runnable: %s", key)
		}
		defs, err := NewTaskTrackerService(nil, nil).Definitions(nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, def := range defs {
			if def.Key == key {
				t.Fatalf("retired task displayed: %s", key)
			}
		}
	}
}
