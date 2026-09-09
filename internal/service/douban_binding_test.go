package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestDoubanBindingImmediateAndForced(t *testing.T) {
	for _, tc := range []struct {
		local, remote string
		force         bool
	}{
		{"movie", "movie", false}, {"series", "tv", false}, {"series", "movie", true}, {"movie", "tv", true},
	} {
		t.Run(tc.local+"_"+tc.remote, func(t *testing.T) {
			scraper, repos, closeServer := newTestScraper(t)
			defer closeServer()
			item := model.MetadataItem{Kind: tc.local, Title: "Original", Overview: "已有简介", Source: "tmdb", NSFW: true}
			if err := repos.Metadata.Create(t.Context(), &item, []model.MetadataIdentifier{{Provider: "douban", EntityKind: tc.local, ExternalID: "99"}, {Provider: "tmdb", EntityKind: tc.local, ExternalID: "12345"}}); err != nil {
				t.Fatal(err)
			}
			var children []model.MetadataItem
			if tc.local == "series" {
				season := model.MetadataItem{Kind: "season", ParentID: &item.ID, SeasonNum: 1, Title: "Season"}
				if err := repos.Metadata.Create(t.Context(), &season, nil); err != nil {
					t.Fatal(err)
				}
				ep := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1, Title: "Episode"}
				if err := repos.Metadata.Create(t.Context(), &ep, nil); err != nil {
					t.Fatal(err)
				}
				if err := repos.DB.Where("id IN ?", []string{season.ID, ep.ID}).Order("id").Find(&children).Error; err != nil {
					t.Fatal(err)
				}
			}
			provider := NewDoubanProvider(nil)
			seen := map[string]int{}
			failDetails := false
			provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				seen[req.URL.Path]++
				if req.URL.Path != "/rexxar/api/v2/subject/10508914" && req.URL.Path != "/rexxar/api/v2/"+tc.remote+"/10508914" {
					t.Fatalf("wrong endpoint %s", req.URL.Path)
				}
				status := http.StatusOK
				body := fmt.Sprintf(`{"id":"10508914","type":%q,"title":"豆瓣中文名","intro":"豆瓣简介","rating":{"value":8.2},"pic":{"large":"https://img.test/poster.jpg"}}`, tc.remote)
				if failDetails && !strings.Contains(req.URL.Path, "/subject/") {
					status, body = http.StatusServiceUnavailable, `unavailable`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			scraper.douban = provider
			req := DoubanBindingRequest{DoubanID: "10508914", MediaType: tc.remote}
			if tc.force {
				if _, err := scraper.BindDouban(t.Context(), item.ID, req); !errors.Is(err, ErrDoubanBindingConflict) {
					t.Fatalf("unconfirmed mismatch = %v", err)
				}
				if got, _ := repos.Metadata.FindByIdentifier(t.Context(), "douban", tc.local, "99"); got == nil {
					t.Fatal("mismatch changed binding")
				}
			}
			req.Force = tc.force
			failDetails = true
			if _, err := scraper.BindDouban(t.Context(), item.ID, req); err == nil {
				t.Fatal("provider failure accepted")
			}
			if got, _ := repos.Metadata.FindByIdentifier(t.Context(), "douban", tc.local, "99"); got == nil {
				t.Fatal("provider failure changed binding")
			}
			failDetails = false
			// 最后一步快照写入失败，也必须回滚绑定、字段和图片关系。
			if err := repos.DB.Callback().Create().Before("gorm:create").Register("fail_douban_snapshot", func(tx *gorm.DB) {
				if tx.Statement.Table == "metadata_provider_snapshots" {
					tx.AddError(errors.New("snapshot failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := scraper.BindDouban(t.Context(), item.ID, req); err == nil {
				t.Fatal("snapshot failure accepted")
			}
			if err := repos.DB.Callback().Create().Remove("fail_douban_snapshot"); err != nil {
				t.Fatal(err)
			}
			if got, _ := repos.Metadata.FindByIdentifier(t.Context(), "douban", tc.local, "99"); got == nil || got.Title != "Original" {
				t.Fatal("transaction did not roll back")
			}
			if poster, err := repos.Artwork.FindSelection(t.Context(), item.ID, "poster"); err != nil || poster != nil {
				t.Fatal("failed binding left artwork selection")
			}
			if degraded, err := scraper.BindDouban(t.Context(), item.ID, req); err != nil || degraded {
				t.Fatalf("binding = %v, %v", degraded, err)
			}
			got, err := repos.Metadata.FindByID(t.Context(), item.ID)
			if err != nil || got.Title != "豆瓣中文名" || got.Overview != "已有简介" || got.Rating != 8.2 || got.Kind != tc.local || got.Source != "tmdb" || !got.NSFW {
				t.Fatalf("bound metadata = %#v, %v", got, err)
			}
			ids, err := repos.Metadata.ListIdentifiers(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			providerKind := "movie"
			if tc.remote == "tv" {
				providerKind = "series"
			}
			if id, ok := uniqueIdentifier(ids, "tmdb", tc.local); !ok || id != "12345" {
				t.Fatal("TMDb changed")
			}
			for _, id := range ids {
				if id.Provider == "douban" && (id.ExternalID != "10508914" || id.DoubanEntityKind != providerKind) {
					t.Fatalf("binding record = %#v", id)
				}
			}
			if poster, err := repos.Artwork.FindSelection(t.Context(), item.ID, "poster"); err != nil || poster == nil {
				t.Fatal("poster not immediately saved")
			}
			if snap, err := repos.Metadata.FindProviderSnapshot(t.Context(), item.ID, "douban"); err != nil || snap == nil {
				t.Fatal("snapshot not immediately saved")
			}
			if len(children) > 0 {
				var after []model.MetadataItem
				if err := repos.DB.Where("id IN ?", []string{children[0].ID, children[1].ID}).Order("id").Find(&after).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(children, after) {
					t.Fatal("children changed")
				}
			}
			// 后台入口重新读取持久化例外，而不是沿用手动请求参数。
			seen = map[string]int{}
			if _, err := scraper.enrichMovieFromDoubanMobile(t.Context(), item.ID); err != nil {
				t.Fatal(err)
			}
			if seen["/rexxar/api/v2/"+tc.remote+"/10508914"] != 1 || len(seen) != 1 {
				t.Fatalf("background endpoint = %v", seen)
			}
		})
	}
}

func TestDoubanBindingSearchUsesSubjectType(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	item := model.MetadataItem{Kind: "series", Title: "Game"}
	if err := repos.Metadata.Create(t.Context(), &item, nil); err != nil {
		t.Fatal(err)
	}
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<script>window.__DATA__ = {"total":3,"items":[{"id":1,"title":"TV","tpl_name":"search_subject"},{"id":2,"title":"Movie","tpl_name":"search_subject"},{"id":3,"title":"Unknown","tpl_name":"search_subject"}]};</script>`
		switch req.URL.Path {
		case "/movie/subject_search":
		case "/rexxar/api/v2/subject/1":
			body = `{"id":"1","type":"tv","is_tv":true,"title":"TV"}`
		case "/rexxar/api/v2/subject/2":
			body = `{"id":"2","type":"movie","is_tv":false,"title":"Movie"}`
		case "/rexxar/api/v2/subject/3":
			body = `{"id":"3","title":"Unknown"}`
		default:
			t.Fatalf("unexpected %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider
	items, err := scraper.SearchDoubanBinding(t.Context(), item.ID, "Game")
	if err != nil || len(items) != 3 || items[0].MediaType != "tv" || items[1].MediaType != "movie" || items[2].MediaType != "" {
		t.Fatalf("candidates = %#v, %v", items, err)
	}
}

func TestDoubanBackgroundRejectsResponseAfterRebind(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	item := model.MetadataItem{Kind: "movie", Title: "Original"}
	if err := repos.Metadata.Create(t.Context(), &item, []model.MetadataIdentifier{{Provider: "douban", EntityKind: "movie", ExternalID: "1"}}); err != nil {
		t.Fatal(err)
	}
	provider := NewDoubanProvider(nil)
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := repos.Metadata.ReplaceIdentifier(t.Context(), item.ID, "douban", "movie", "2"); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"title":"旧条目的中文名","intro":"stale"}`)), Request: req}, nil
	})}
	scraper.douban = provider
	result, err := scraper.enrichMovieFromDoubanMobile(t.Context(), item.ID)
	if err != nil || !result.Skipped {
		t.Fatalf("stale result = %#v, %v", result, err)
	}
	got, _ := repos.Metadata.FindByID(t.Context(), item.ID)
	if got.Title != "Original" {
		t.Fatal("stale response overwrote metadata")
	}
	if snapshot, _ := repos.Metadata.FindProviderSnapshot(t.Context(), item.ID, "douban"); snapshot != nil {
		t.Fatal("stale snapshot persisted")
	}
}

func TestDoubanBindingRejectsUnknownAndChangedType(t *testing.T) {
	for _, raw := range []string{`{"id":"2","type":"movie"}`, `{"id":"1"}`, `{"id":"1","type":"movie","is_tv":true}`} {
		if _, err := doubanBindingPayloadKind([]byte(raw), "1"); !errors.Is(err, ErrDoubanBindingInvalid) {
			t.Fatalf("accepted %s", raw)
		}
	}
}
