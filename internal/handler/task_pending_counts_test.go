package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestTaskPendingCountsCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.MetadataItem{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckAssetChange{}); err != nil {
		t.Fatal(err)
	}
	seriesID, seasonID := "series", "season"
	for _, row := range []any{
		&model.Library{Base: model.Base{ID: "library"}, Name: "测试库", Type: "tv"},
		&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seriesID}, Kind: "series"},
		&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: "season", ParentID: &seriesID, SeasonNum: 1},
		&model.MetadataItem{PermanentBase: model.PermanentBase{ID: "episode"}, Kind: "episode", ParentID: &seasonID, EpisodeNum: 1},
		&model.MetadataItem{PermanentBase: model.PermanentBase{ID: "orphan"}, Kind: "episode", ParentID: &seasonID, EpisodeNum: 2},
		&model.TMDbRecheckJob{MetadataID: "episode", Status: "pending"},
		&model.TMDbRecheckJob{MetadataID: "orphan", Status: "pending"},
		&model.Media{LibraryID: "library", MetadataID: "episode", Path: "/test/one.strm", ScrapeStatus: "error"},
		&model.Media{LibraryID: "library", Path: "/test/two.strm", ScrapeStatus: "no_match"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.New(db)
	svc := &service.Container{Repo: repo, Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repo)}
	router := gin.New()
	registerAuthedStatsDiscoveryAndAIRoutes(router.Group("/api", middleware.AuthRequired("pending-counts-test")), svc)
	request := func(role, suffix string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/pending-counts"+suffix, nil)
		if role != "" {
			req.Header.Set("Authorization", "Bearer "+signedProbeRoleToken(t, "pending-counts-test", role))
		}
		router.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		role, suffix string
		status       int
	}{{"", "", 401}, {"user", "", 403}, {"admin", "?refresh=invalid", 400}} {
		if rec := request(tc.role, tc.suffix); rec.Code != tc.status {
			t.Fatalf("status=%d, want %d", rec.Code, tc.status)
		}
	}
	decode := func(rec *httptest.ResponseRecorder) taskPendingCounts {
		t.Helper()
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out taskPendingCounts
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	var queries atomic.Int32
	// 任务页数量统计不依赖变更明细表。
	if err := db.Migrator().DropTable(&model.TMDbRecheckChange{}, &model.TMDbRecheckAssetChange{}); err != nil {
		t.Fatal(err)
	}
	count := func(tx *gorm.DB) {
		if !tx.DryRun {
			queries.Add(1)
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("count_pending_queries", count); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register("count_pending_rows", count); err != nil {
		t.Fatal(err)
	}
	first := decode(request("admin", ""))
	if first.Rechecks != 1 || first.ScrapeIssues != 2 || first.UpdatedAt.IsZero() {
		t.Fatalf("first=%+v", first)
	}
	queryCount := queries.Load()
	if queryCount != 2 {
		t.Fatalf("cold load queries=%d, want 2 (pending rechecks and scrape count)", queryCount)
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id = ?", "episode").Update("status", "done").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Media{}).Where("library_id = ?", "library").Update("scrape_status", "matched").Error; err != nil {
		t.Fatal(err)
	}
	if got := decode(request("admin", "")); got != first || queries.Load() != queryCount {
		t.Fatalf("cache miss: %+v, queries=%d", got, queries.Load())
	}

	started, release := make(chan struct{}), make(chan struct{})
	var block atomic.Bool
	block.Store(true)
	if err := db.Callback().Row().Before("gorm:row").Register("block_pending_refresh", func(*gorm.DB) {
		if block.CompareAndSwap(true, false) {
			close(started)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	refreshed := make(chan *httptest.ResponseRecorder, 1)
	go func() { refreshed <- request("admin", "?refresh=1") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	cached := make(chan *httptest.ResponseRecorder, 1)
	go func() { cached <- request("admin", "") }()
	select {
	case rec := <-cached:
		if got := decode(rec); got != first {
			t.Fatalf("refresh replaced cache early: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cache read waited for refresh")
	}
	close(release)
	var next taskPendingCounts
	select {
	case rec := <-refreshed:
		next = decode(rec)
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if next.Rechecks != 0 || next.ScrapeIssues != 0 || !next.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("refresh=%+v", next)
	}
	if err := db.Migrator().DropTable(&model.TMDbRecheckJob{}); err != nil {
		t.Fatal(err)
	}
	if rec := request("admin", "?refresh=1"); rec.Code != 500 {
		t.Fatalf("failed refresh status=%d", rec.Code)
	}
	if got := decode(request("admin", "")); got != next {
		t.Fatalf("failure replaced cache: %+v", got)
	}
}
