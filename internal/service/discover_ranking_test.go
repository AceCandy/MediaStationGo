package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func TestDiscoverRankingPersistenceFailureAndPagination(t *testing.T) {
	db := newServiceTestDB(t, &model.DiscoverRankingPage{})
	repo := repository.New(db)
	d := NewDiscoverService(zap.NewNop(), nil)
	d.rankingRepo = repo.DiscoverRanking
	key := "tmdb_popular_movie"
	calls := 0
	load := func(context.Context) ([]ExternalMediaResult, error) {
		calls++
		return []ExternalMediaResult{{Title: fmt.Sprint(calls), TMDbID: calls}}, nil
	}
	_, first, err := d.RankingPage(t.Context(), key, 1, true, load)
	if err != nil || first.IsZero() {
		t.Fatal(first, err)
	}
	if _, _, err := d.RankingPage(t.Context(), key, 2, false, load); err != nil {
		t.Fatal(err)
	}
	restarted := NewDiscoverService(zap.NewNop(), nil)
	restarted.rankingRepo = repo.DiscoverRanking
	items, at, err := restarted.RankingPage(t.Context(), key, 1, false, load)
	if err != nil || !at.Equal(first) || calls != 2 || items[0].Title != "1" {
		t.Fatal("restart lost snapshot", items, at, calls, err)
	}
	failed := func(context.Context) ([]ExternalMediaResult, error) { return nil, errors.New("upstream unavailable") }
	items, at, err = restarted.RankingPage(t.Context(), key, 1, true, failed)
	if err == nil || !at.Equal(first) || items[0].Title != "1" {
		t.Fatal("failure changed snapshot", items, at, err)
	}
	empty := func(context.Context) ([]ExternalMediaResult, error) { return nil, nil }
	items, at, err = restarted.RankingPage(t.Context(), key, 1, true, empty)
	if err == nil || !at.Equal(first) || len(items) != 1 {
		t.Fatal("empty first page discarded prior snapshot")
	}
	if items, _, err = restarted.RankingPage(t.Context(), key, 3, false, empty); err != nil || len(items) != 0 {
		t.Fatal("empty continuation rejected")
	}
	if _, _, err = restarted.RankingPage(t.Context(), key, 1, true, load); err != nil {
		t.Fatal(err)
	}
	if row, err := repo.DiscoverRanking.Load(t.Context(), key, 2); err != nil || row != nil {
		t.Fatal("old continuation retained", row, err)
	}
	if _, _, err := d.RankingPage(t.Context(), key, 2, false, load); err != nil || calls != 4 {
		t.Fatal("deleted persistent continuation resurrected from memory", calls, err)
	}
	// A late older write cannot replace the newest first page.
	if err := repo.DiscoverRanking.Save(t.Context(), model.DiscoverRankingPage{Key: key, Page: 1, Payload: `[]`, FetchedAt: first.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.DiscoverRanking.Load(t.Context(), key, 1)
	if err != nil || row == nil || !row.FetchedAt.After(first) {
		t.Fatal("older response won", row, err)
	}
}

func TestDiscoverRankingCanceledLockWait(t *testing.T) {
	d := NewDiscoverService(zap.NewNop(), nil)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = d.RankingPage(t.Context(), "tmdb_popular_movie", 1, true, func(context.Context) ([]ExternalMediaResult, error) {
			close(started)
			<-release
			return []ExternalMediaResult{{Title: "one"}}, nil
		})
	}()
	<-started
	defer func() { close(release); <-done }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, _, err := d.RankingPage(ctx, "tmdb_popular_tv", 1, true, func(context.Context) ([]ExternalMediaResult, error) {
			t.Error("canceled waiter called provider")
			return nil, nil
		})
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lock ignored cancellation")
	}
}

func TestDiscoverRankTasksKeepSummariesForPeriodicMaintenance(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	center, provider := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("page") != "1" {
			t.Error("rank task traversed extra pages")
		}
		if fail.Load() && r.URL.Path == "/movie/popular" {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":10,"title":"电影摘要","name":"剧集摘要","poster_path":"/a.jpg"},{"id":10,"title":"重复作品","name":"重复作品","poster_path":"/a.jpg"}]}`)
	})
	d := NewDiscoverService(zap.NewNop(), provider)
	d.rankingRepo = center.repo.DiscoverRanking
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	tracker.ConfigurePersistence(nil, t.TempDir())
	scheduler := &SchedulerService{discover: d, tasks: tracker}
	for range 2 {
		if err := scheduler.runDiscoverRanks(t.Context(), TaskKindTMDbRank, "TMDb 排行榜刷新", "tmdb"); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 16 {
		t.Fatal("expected one request per ranking", calls.Load())
	}
	var count int64
	if err := center.repo.DB.Model(&model.TMDbSubject{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("summary identity not deduplicated", count, err)
	}
	for _, kind := range []string{"movie", "series"} {
		candidates, err := center.repo.TMDb.MaintenanceCandidates(t.Context(), kind, false, 100)
		if err != nil || len(candidates) != 1 || candidates[0].Quality != 1 {
			t.Fatal("summary missing from maintenance", kind, candidates, err)
		}
	}
	for _, entry := range []struct {
		table any
		where string
	}{{&model.MetadataItem{}, "1=1"}, {&model.TMDbRefreshJob{}, "explicit_pending = true"}} {
		if err := center.repo.DB.Model(entry.table).Where(entry.where).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("ranking created local metadata or explicit supplement", count, err)
		}
	}
	before, _ := center.repo.DiscoverRanking.Load(t.Context(), "tmdb_popular_movie", 1)
	fail.Store(true)
	if err := scheduler.runDiscoverRanks(t.Context(), TaskKindTMDbRank, "TMDb 排行榜刷新", "tmdb"); err == nil {
		t.Fatal("partial failure counted as success")
	}
	after, _ := center.repo.DiscoverRanking.Load(t.Context(), "tmdb_popular_movie", 1)
	if !before.FetchedAt.Equal(after.FetchedAt) {
		t.Fatal("failure advanced success time")
	}
	history, err := tracker.DefinitionHistory(TaskKindTMDbRank, 1, 10)
	if err != nil || history.Total != 3 || history.Items[0].Status != TaskStatusFailed {
		t.Fatal("rank task history incorrect", history, err)
	}
}

func TestDoubanRankSummariesArePeriodicCandidates(t *testing.T) {
	p, repo := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/j/search_subjects" {
			t.Errorf("unexpected detail or image request: %s", req.URL.Path)
		}
		return doubanCacheTestResponse(req, 200, `{"subjects":[{"id":"100","title":"榜单作品","rate":"8.0","cover":"https://img9.doubanio.com/view/photo/l/public/p100.jpg"}]}`), nil
	})
	if err := repo.DB.AutoMigrate(&model.DiscoverRankingPage{}); err != nil {
		t.Fatal(err)
	}
	discover := NewDiscoverService(zap.NewNop(), nil)
	discover.rankingRepo = repo.DiscoverRanking
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	scheduler := &SchedulerService{discover: discover, tasks: tasks, scraper: &ScraperService{douban: p}}
	if err := scheduler.runDiscoverRanks(t.Context(), TaskKindDoubanRank, "豆瓣排行榜刷新", "douban"); err != nil {
		t.Fatal(err)
	}
	var snapshots int64
	if err := repo.DB.Model(&model.DiscoverRankingPage{}).Count(&snapshots).Error; err != nil || snapshots != 3 {
		t.Fatal("missing persisted rankings", snapshots, err)
	}
	for _, kind := range []string{"movie", "series"} {
		rows, err := repo.Douban.MaintenanceCandidates(t.Context(), kind, false, time.Now().Add(-24*time.Hour), time.Now(), 100)
		if err != nil || len(rows) != 1 || rows[0].SupplementPending {
			t.Fatal("placeholder not selected by periodic maintenance", rows, err)
		}
	}
}
