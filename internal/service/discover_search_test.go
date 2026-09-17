package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestDiscoverSearchTMDbPaginationAndIdentity(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/search/multi" || r.URL.Query().Get("query") != "测试" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("include_adult") != "false" {
			t.Errorf("unexpected search request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_pages":3,"results":[{"id":10,"media_type":"movie","title":"电影","release_date":"2026-09-01","poster_path":"/a.jpg"},{"id":10,"media_type":"tv","name":"剧集"},{"id":10,"media_type":"movie","title":"重复"},{"id":11,"media_type":"person","name":"演员"},{"id":12,"media_type":"movie","title":"成人","adult":true}]}`))
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
	d := NewDiscoverService(zap.NewNop(), NewTMDbProvider(cfg, zap.NewNop(), nil))
	items, next, err := d.SearchTMDb(t.Context(), "测试", "multi", 2)
	if err != nil || !next || len(items) != 2 || items[0].MediaType != "movie" || items[1].MediaType != "tv" || items[0].Year != 2026 || calls != 1 {
		t.Fatalf("items=%#v next=%v err=%v calls=%d", items, next, err, calls)
	}
	for _, input := range []struct {
		query, kind string
		page        int
	}{{"", "multi", 1}, {"test", "person", 1}, {"test", "tv", 501}} {
		if _, _, err := d.SearchTMDb(t.Context(), input.query, input.kind, input.page); err == nil {
			t.Fatal("invalid search accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid input reached upstream")
	}
}

func TestDiscoverSearchQueueSkipsExistingAndDeduplicates(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	item := &model.MetadataItem{Kind: model.MetadataKindMovie, Title: "完整资料", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), item, []model.MetadataIdentifier{{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "10"}}); err != nil {
		t.Fatal(err)
	}
	items := []ExternalMediaResult{{Source: "tmdb", MediaType: "movie", TMDbID: 10, Title: "摘要"}, {Source: "tmdb", MediaType: "tv", TMDbID: 10, Title: "电视剧"}}
	for range 2 {
		if err := s.QueueMissingSearchMetadata(t.Context(), items); err != nil {
			t.Fatal(err)
		}
	}
	var jobs []model.CatalogHydrationJob
	if err := repos.DB.Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].EntityKind != model.MetadataKindSeries {
		t.Fatalf("jobs=%#v", jobs)
	}
	stored, err := repos.Metadata.FindByID(t.Context(), item.ID)
	if err != nil || stored.Title != "完整资料" {
		t.Fatal("search overwrote existing metadata")
	}
}
