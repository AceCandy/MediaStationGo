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

func TestTMDbCenterDiscoverySearchOnlySummaries(t *testing.T) {
	calls := 0
	s, p := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"results":[{"id":10,"media_type":"movie","title":"电影"},{"id":10,"media_type":"tv","name":"电视剧"}],"total_pages":1}`))
	})
	d := NewDiscoverService(zap.NewNop(), p)
	for range 2 {
		items, _, e := d.SearchTMDb(t.Context(), "测试", "multi", 1)
		if e != nil || len(items) != 2 {
			t.Fatal(e)
		}
	}
	if calls != 1 {
		t.Fatalf("query not reused: %d", calls)
	}
	for _, table := range []any{&model.MetadataItem{}, &model.CatalogHydrationJob{}} {
		var n int64
		if e := s.repo.DB.Model(table).Count(&n).Error; e != nil || n != 0 {
			t.Fatalf("search expanded local catalog %d %v", n, e)
		}
	}
	rows, n, e := s.repo.TMDb.Subjects(t.Context(), "", "", "", -1, 1, 30)
	if e != nil || n != 2 || rows[0].Quality != 1 {
		t.Fatalf("summaries %d %v", n, e)
	}
}
