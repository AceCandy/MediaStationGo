package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestDoubanRankingSummaryAndDurableAPISupplement(t *testing.T) {
	var calls atomic.Int32
	p, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body string
		switch req.URL.Path {
		case "/j/search_subjects":
			body = `{"subjects":[{"id":"100","title":"榜单摘要","rate":"7.1","cover":"https://img3.doubanio.com/view/photo/l/public/p.jpg"},{"id":"100","title":"重复项"},{"id":"invalid","title":"错误项"}]}`
		case "/rexxar/api/v2/movie/100":
			body = `{"id":"100","type":"movie","title":"来源标题","intro":"来源简介","rating":8.2}`
		case "/rexxar/api/v2/movie/100/celebrities":
			body = `{"directors":[{"id":"3","name":"导演","character":"导演"}],"actors":[{"id":"4","name":"演员","character":"饰 主角","avatar":{"large":"https://img3.doubanio.com/view/personage/m/public/person.jpg"}}],"total":87}`
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
		}
		return doubanCacheTestResponse(req, 200, body), nil
	})
	p.tasks = NewTaskTrackerService(nil, nil)
	p.tasks.ConfigurePersistence(nil, t.TempDir())
	items, err := p.Discover(t.Context(), "douban_hot_movie")
	if err != nil || len(items) != 1 || calls.Load() != 1 {
		t.Fatalf("ranking %v %v", items, err)
	}
	row, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || row == nil || row.Payload != nil || row.Summary == nil || row.FetchedAt != nil {
		t.Fatalf("summary masquerades as complete: %+v %v", row, err)
	}
	oldRich := `{"fetched_at":"2026-01-01T00:00:00Z","imdb_id":"tt123","writers":["编剧"],"awards":[{"event":"旧奖项"}],"credits":[{"name":"演员","type":"Actor","source_url":"https://img3.doubanio.com/view/personage/m/public/person.jpg","image_key":"catalogs/douban/people/existing.jpg"}]}`
	if err := repos.DB.Model(row).UpdateColumn("rich_payload", oldRich).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		detail, err := p.EnsureSubject(t.Context(), "100", "movie", "viewer")
		if err != nil || !detail.RefreshPending || detail.Title != "榜单摘要" {
			t.Fatalf("ensure %+v %v", detail, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("ensure performs upstream request")
	}
	// 模拟重启丢失内存；补全只依赖持久业务状态。
	restarted := NewDoubanProvider(nil)
	restarted.setDataRepository(repos, nil)
	restarted.client, restarted.dataDelay, restarted.tasks = p.client, 0, p.tasks
	if err := restarted.runSupplements(t.Context()); err != nil {
		t.Fatal(err)
	}
	detail, err := restarted.SubjectDetail(t.Context(), row.ID)
	if err != nil || detail.RefreshPending || detail.Title != "来源标题" || len(detail.Credits) != 2 || detail.Credits[1].Role != "饰 主角" || calls.Load() != 3 {
		t.Fatalf("API result %+v calls=%d err=%v", detail, calls.Load(), err)
	}
	if err := restarted.runSupplements(t.Context()); err != nil || calls.Load() != 3 {
		t.Fatal("settled work repeated")
	}
	if _, err := restarted.EnsureSubject(t.Context(), "100", "movie", "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.runSupplements(t.Context()); err != nil || calls.Load() != 3 {
		t.Fatal("complete source was forced")
	}
	if _, err := p.Discover(t.Context(), "douban_hot_movie"); err != nil || calls.Load() != 3 {
		t.Fatal("query cache was lost")
	}
	row, _ = repos.Douban.Find(t.Context(), "100", "movie")
	if row.Rating != 8.2 || row.Quality != model.DoubanQualityComplete || row.CreditsFetchedAt == nil {
		t.Fatal("summary overwrote complete fields")
	}
	rich := doubanRichPayload(row)
	if rich.IMDbID != "tt123" || len(rich.Awards) != 1 || len(rich.Writers) != 1 || rich.Credits[1].ImageKey == "" {
		t.Fatalf("rich fields lost: %+v", rich)
	}
	history, err := p.tasks.DefinitionHistory(TaskKindDoubanDataSupplement, 1, 10)
	if err != nil || history.Total != 1 || history.Items[0].System != model.TaskSystemDouban || history.Items[0].Metrics["updated"] != 1 {
		t.Fatalf("missing task progress %+v %v", history, err)
	}
}

func TestDoubanSupplementRetryLeaseAndQuota(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	p, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if strings.HasSuffix(req.URL.Path, "/celebrities") {
			if fail.Load() {
				return doubanCacheTestResponse(req, 503, "unavailable"), nil
			}
			return doubanCacheTestResponse(req, 200, `{"directors":[],"actors":[]}`), nil
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","title":"完整基础","rating":8}`), nil
	})
	detail, err := p.EnsureSubject(t.Context(), "100", "movie", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.runSupplements(t.Context()); err == nil {
		t.Fatal("failed credits reported success")
	}
	row, _ := repos.Douban.FindSubjectByID(t.Context(), detail.ID)
	if !row.SupplementPending || row.SupplementRetryAt == nil || row.Payload == nil || row.CreditsFetchedAt != nil {
		t.Fatalf("retry intent lost %+v", row)
	}
	if err = p.runSupplements(t.Context()); err != nil || calls.Load() != 2 {
		t.Fatal("backoff bypassed")
	}
	fail.Store(false)
	if err = repos.DB.Model(row).UpdateColumn("supplement_retry_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	first, err := repos.Douban.ClaimSupplement(t.Context(), row.ID)
	if err != nil || first == nil {
		t.Fatal("claim failed")
	}
	if duplicate, _ := repos.Douban.ClaimSupplement(t.Context(), row.ID); duplicate != nil {
		t.Fatal("active lease claimed twice")
	}
	if err = repos.DB.Model(row).UpdateColumn("supplement_lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	second, err := repos.Douban.ClaimSupplement(t.Context(), row.ID)
	if err != nil || second == nil || second.SupplementToken == first.SupplementToken {
		t.Fatal("expired lease not recoverable")
	}
	if err = repos.Douban.SettleSupplement(t.Context(), first, true, nil); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Douban.FindSubjectByID(t.Context(), row.ID)
	if !current.SupplementPending || current.SupplementToken != second.SupplementToken {
		t.Fatal("stale lease settled new work")
	}
	if err = repos.Douban.SettleSupplement(t.Context(), second, false, nil); err != nil {
		t.Fatal(err)
	}
	if err = p.runSupplements(t.Context()); err != nil || calls.Load() != 3 {
		t.Fatalf("base freshness lost %d %v", calls.Load(), err)
	}
	for index := range 50 {
		_, err := repos.Douban.QueueSupplement(t.Context(), strconv.Itoa(index+200), "movie", "quota-user")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.EnsureSubject(t.Context(), "9999", "movie", "quota-user"); !errors.Is(err, repository.ErrDoubanQueueLimit) {
		t.Fatalf("quota %v", err)
	}
	if rejected, _ := repos.Douban.Find(t.Context(), "9999", "movie"); rejected != nil {
		t.Fatal("quota rejection persisted placeholder")
	}
	if err := repos.DB.Model(&model.DoubanSubject{}).Where("douban_id IN ?", []string{"200", "201", "202", "203", "204", "205", "206", "207", "208", "209", "210", "211", "212", "213", "214", "215", "216", "217", "218", "219"}).UpdateColumn("supplement_retry_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	due, err := repos.Douban.DueSupplements(t.Context(), 20)
	if err != nil || len(due) != 20 {
		t.Fatalf("backoff blocked later work %d %v", len(due), err)
	}
	for _, row := range due {
		if row.SupplementRetryAt != nil {
			t.Fatal("backoff row occupied batch")
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.runSupplements(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown %v", err)
	}
}

func TestDoubanSupplementCancellationKeepsIntentAndTaskState(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/celebrities") {
			cancel()
			return nil, context.Canceled
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","title":"基础"}`), nil
	})
	p.tasks = NewTaskTrackerService(nil, nil)
	p.tasks.ConfigurePersistence(nil, t.TempDir())
	detail, err := p.EnsureSubject(t.Context(), "100", "movie", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.runSupplements(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	row, err := repos.Douban.FindSubjectByID(t.Context(), detail.ID)
	if err != nil || !row.SupplementPending || row.SupplementLeaseUntil != nil || row.SupplementRetryAt != nil || row.SupplementToken != "" {
		t.Fatalf("cancel lost pending intent %+v %v", row, err)
	}
	history, err := p.tasks.DefinitionHistory(TaskKindDoubanDataSupplement, 1, 10)
	if err != nil || len(history.Items) != 1 || history.Items[0].Status != TaskStatusInterrupted || history.Items[0].Stage != "interrupted" {
		t.Fatalf("cancel misreported %+v %v", history, err)
	}
}
