package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func hongGuoRankTestPage(label, id string) string {
	return fmt.Sprintf(`<html><body><ol aria-label=%q><li><article><img src="https://example.invalid/rank"><h2 id="rank-title-%s">榜单摘要</h2></article></li></ol><nav aria-label="榜单分页"></nav></body></html>`, label, id)
}

// 仅验证发现任务；事件刷新由独立的唤醒测试覆盖。
func runHongGuoDiscoveryOnly(ctx context.Context, s *HongGuoService) (refreshQueued bool, err error) {
	s.runMu.Lock()
	defer func() {
		s.mu.Lock()
		refreshQueued = s.refreshRequested
		s.refreshRequested = false
		s.mu.Unlock()
		s.runMu.Unlock()
	}()
	err = s.Run(ctx, TaskKindHongGuoSync, "")
	return
}

func TestHongGuoDiscoveryScansUntilCategoryEnd(t *testing.T) {
	for _, tc := range []struct {
		name          string
		start         int
		repeat        bool
		partial       bool
		notFoundPage  int
		previousItems int
		wantRequests  int
		wantRows      int64
		wantNext      int
		wantError     string
	}{
		{name: "past_20_pages_in_all_categories", start: 1, wantRequests: 82, wantRows: 1800, wantNext: 1},
		{name: "partial_page_is_completion", start: 1, partial: true, wantRequests: 7, wantRows: 24, wantNext: 1},
		{name: "saved_next_page_404_is_completion", start: 14, notFoundPage: 14, previousItems: 8, wantRequests: 8, wantRows: 8, wantNext: 1},
		{name: "middle_404_is_failure", start: 14, notFoundPage: 14, previousItems: 24, wantRequests: 2, wantRows: 0, wantNext: 14, wantError: "HTTP 404"},
		{name: "first_page_404_is_failure", start: 1, notFoundPage: 1, wantRequests: 1, wantRows: 0, wantNext: 1, wantError: "HTTP 404"},
		{name: "repeated_page_is_not_completion", start: 1, repeat: true, wantRequests: 2, wantRows: 24, wantNext: 2, wantError: "分页未推进"},
		{name: "limit_is_not_completion", start: hongguo.MaxCategoryPage, wantRequests: 1, wantRows: 24, wantNext: hongguo.MaxCategoryPage, wantError: "安全上限"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			repos := repository.New(db)
			if err := repos.HongGuo.SaveSyncState(ctx, model.HongGuoSyncState{Category: hongguo.Categories[0], NextPage: tc.start}); err != nil {
				t.Fatal(err)
			}
			s := NewHongGuoService(repos, NewTaskTrackerService(zap.NewNop(), nil), nil, t.TempDir())
			t.Cleanup(s.Wait)
			requests := 0
			requestPaths := []string{}
			s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				requestPaths = append(requestPaths, r.URL.RequestURI())
				page := 1
				if value := r.URL.Query().Get("page"); value != "" {
					page, _ = strconv.Atoi(value)
				}
				categoryIndex := -1
				for i, category := range hongguo.Categories {
					if r.URL.Path == "/category/"+category {
						categoryIndex = i
					}
				}
				if categoryIndex < 0 {
					for _, rank := range hongguo.Ranks {
						if r.URL.Path == "/rank/"+rank.Key {
							idPage := 1
							if tc.notFoundPage > 1 {
								idPage = tc.notFoundPage - 1
							}
							body := hongGuoRankTestPage(rank.Label, fmt.Sprintf("1%05d00", idPage))
							return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
						}
					}
					t.Fatalf("unexpected request: %s", r.URL.Path)
				}
				if categoryIndex == 0 && page == tc.notFoundPage {
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("private")), Header: make(http.Header), Request: r}, nil
				}
				itemCount := 0
				if tc.notFoundPage == 0 && (page <= 25 || tc.start == hongguo.MaxCategoryPage) {
					itemCount = hongguo.CategoryPageSize
				}
				if categoryIndex == 0 && tc.notFoundPage > 1 && page == tc.notFoundPage-1 {
					itemCount = tc.previousItems
				}
				if tc.partial {
					itemCount = 8
				}
				items := make([]string, 0, itemCount)
				if itemCount > 0 {
					idPage := page
					if tc.repeat {
						idPage = 1
					}
					for item := 0; item < itemCount; item++ {
						items = append(items, fmt.Sprintf(`{"series_id":"%d%05d%02d","series_name":"摘要"}`, categoryIndex+1, idPage, item))
					}
				}
				body := `_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[` + strings.Join(items, ",") + `]}}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			_, err = runHongGuoDiscoveryOnly(ctx, s)
			if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("result: %v", err)
			}
			if requests != tc.wantRequests {
				t.Fatalf("requests=%d want=%d", requests, tc.wantRequests)
			}
			if tc.notFoundPage > 1 && (len(requestPaths) < 2 || requestPaths[0] != "/category/real-drama?page=14" || requestPaths[1] != "/category/real-drama?page=13") {
				t.Fatalf("404 was not verified against the preceding page: %v", requestPaths)
			}
			var rows int64
			if err := db.Model(&model.HongGuoDiscovery{}).Count(&rows).Error; err != nil || rows != tc.wantRows {
				t.Fatalf("rows=%d %v", rows, err)
			}
			state, err := repos.HongGuo.SyncState(ctx, hongguo.Categories[0])
			if err != nil || state.NextPage != tc.wantNext {
				t.Fatalf("checkpoint=%+v %v", state, err)
			}
			page, err := s.tasks.ListSystem(model.TaskSystemHongGuo, 1, 1)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("task history: %+v %v", page, err)
			}
			wantStatus := TaskStatusCompleted
			if tc.wantError != "" {
				wantStatus = TaskStatusFailed
			}
			if page.Items[0].Status != wantStatus {
				t.Fatalf("task status=%s", page.Items[0].Status)
			}
		})
	}
}

func TestHongGuoDiscoveryDefersDetailsAndResumes(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repos := repository.New(db)
	s := NewHongGuoService(repos, NewTaskTrackerService(zap.NewNop(), nil), nil, t.TempDir())
	t.Cleanup(s.Wait)
	var items []string
	for i := 1; i <= 24; i++ {
		items = append(items, fmt.Sprintf(`{"video_data":{"series_id":"%d","series_title":"列表摘要","episode_cnt":1}}`, 95000+i))
	}
	paths := []string{}
	details := map[string]int{}
	failPage := true
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.RequestURI())
		body := `_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[]}}}`
		if r.URL.Path == "/detail" {
			id := r.URL.Query().Get("series_id")
			details[id]++
			body = fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":%q,"series_name":"完整详情","episode_cnt":2}}}}`, id)
		} else if r.URL.RequestURI() == "/category/real-drama" {
			body = `_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[` + strings.Join(items, ",") + `]}}}`
		} else if failPage && r.URL.RequestURI() == "/category/real-drama?page=2" {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
		} else {
			for _, rank := range hongguo.Ranks {
				if r.URL.Path == "/rank/"+rank.Key {
					body = hongGuoRankTestPage(rank.Label, "95001")
				}
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	queued, err := runHongGuoDiscoveryOnly(ctx, s)
	if err == nil {
		t.Fatal("failed page accepted")
	}
	if !queued {
		t.Fatal("persisted discovery page did not request automatic refresh")
	}
	failedTask, err := s.tasks.ListSystem(model.TaskSystemHongGuo, 1, 1)
	if err != nil || len(failedTask.Items) != 1 || !strings.Contains(failedTask.Items[0].Message, "任务失败") || !strings.Contains(failedTask.Items[0].Error, "real-drama 第 2 页（/category/real-drama?page=2）") {
		t.Fatalf("failed task lacks request context: %+v %v", failedTask, err)
	}
	if len(paths) != 2 || len(details) != 0 {
		t.Fatalf("discovery fetched details: %v", paths)
	}
	var n int64
	if err := db.Model(&model.HongGuoWork{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("summary became canonical: %d %v", n, err)
	}
	pending, err := repos.HongGuo.PendingDiscoveries(ctx, "", time.Now())
	if err != nil || len(pending) != 24 || pending[0].Title != "列表摘要" {
		t.Fatalf("pending=%d %v", len(pending), err)
	}
	failPage = false
	paths = nil
	// 新服务读取数据库检查点，不依赖上次执行的内存状态。
	restarted := NewHongGuoService(repos, s.tasks, nil, t.TempDir())
	restarted.client = s.client
	t.Cleanup(restarted.Wait)
	if _, err := runHongGuoDiscoveryOnly(ctx, restarted); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 7 || paths[0] != "/category/real-drama?page=2" {
		t.Fatalf("did not resume: %v", paths)
	}
	// 超过一批，验证刷新任务会分页补齐，而不是一天只能入库 100 部。
	extra := []hongguo.Work{}
	for i := 25; i <= 105; i++ {
		extra = append(extra, hongguo.Work{SourceID: fmt.Sprint(95000 + i), Title: "摘要"})
	}
	if err := repos.HongGuo.SaveDiscoveryPage(ctx, extra, model.HongGuoSyncState{Category: "ai-drama", NextPage: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil {
		t.Fatal(err)
	}
	if len(details) != 105 {
		t.Fatalf("only hydrated %d works", len(details))
	}
	for id, calls := range details {
		if calls != 1 {
			t.Fatalf("repeated %s: %d", id, calls)
		}
	}
	pending, err = repos.HongGuo.PendingDiscoveries(ctx, "", time.Now())
	if err != nil || len(pending) != 0 {
		t.Fatalf("completed remained pending: %v %v", pending, err)
	}
	if _, err := runHongGuoDiscoveryOnly(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil {
		t.Fatal(err)
	}
	for id, calls := range details {
		if calls != 1 {
			t.Fatalf("discovery bypassed cooldown %s: %d", id, calls)
		}
	}
	work, err := repos.HongGuo.FindBySourceID(ctx, "95001")
	if err != nil || work.Title != "完整详情" || work.EpisodeCount != 2 {
		t.Fatalf("summary overwrote details: %+v %v", work, err)
	}
	if err := s.Run(ctx, TaskKindHongGuoRefresh, "95001"); err != nil || details["95001"] != 2 {
		t.Fatalf("manual ID blocked by cooldown: %v", err)
	}
}

func TestHongGuoDiscoveryIncrementalStopsAtSavedBoundary(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repos := repository.New(db)
	if err := repos.HongGuo.SaveSyncState(ctx, model.HongGuoSyncState{Category: "real-drama", NextPage: 1, AfterID: "100"}); err != nil {
		t.Fatal(err)
	}
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	s := NewHongGuoService(repos, tasks, nil, t.TempDir())
	t.Cleanup(s.Wait)
	requests := []string{}
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.RequestURI())
		for _, rank := range hongguo.Ranks {
			if r.URL.Path == "/rank/"+rank.Key {
				body := hongGuoRankTestPage(rank.Label, "200")
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			}
		}
		page := r.URL.Query().Get("page")
		items := []string{}
		if page == "" {
			items = []string{`{"series_id":"200"}`, `{"series_id":"100"}`}
		}
		body := `_ROUTER_DATA={"loaderData":{"category_page":{"recommendList":[` + strings.Join(items, ",") + `]}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	if _, err := runHongGuoDiscoveryOnly(ctx, s); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 7 || requests[0] != "/category/real-drama" {
		t.Fatalf("requests=%v", requests)
	}
	log, err := tasks.ReadDefinitionLog(TaskKindHongGuoSync, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, notice := range []string{
		"ℹ️ 红果分类 real-drama 增量扫描至第 1 页，已追平上次检查点",
		"ℹ️ 红果分类 comic-drama 第 1 页共 2 项，少于每页 24 项，目录已到末页并重置检查点",
		"ℹ️ 红果分类 ai-drama 第 1 页共 2 项，少于每页 24 项，目录已到末页并重置检查点",
		"ℹ️ 红果热播榜已按官网名次更新，共 1 项",
		"ℹ️ 真人剧热播榜已按官网名次更新，共 1 项",
		"ℹ️ AI剧热播榜已按官网名次更新，共 1 项",
		"ℹ️ 漫剧热播榜已按官网名次更新，共 1 项",
	} {
		if count := strings.Count(log.Content, notice); count != 1 {
			t.Fatalf("notice %q count=%d log=%q", notice, count, log.Content)
		}
	}
	var count int64
	if err := db.Model(&model.HongGuoDiscovery{}).Where("source_id = ?", "200").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("new discovery count=%d err=%v", count, err)
	}
	if _, err := runHongGuoDiscoveryOnly(ctx, s); err != nil {
		t.Fatal(err)
	}
	page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Metrics["new"] != 0 || page.Items[0].Metrics["processed"] != 0 || !strings.Contains(page.Items[0].Message, "本次新增 0 项") {
		t.Fatalf("repeat discovery counted old rank or category entries: %+v %v", page, err)
	}
}
