package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestDoubanGetMatchByIDPreservesRawJSON(t *testing.T) {
	provider := NewDoubanProvider(&config.Config{}, zap.NewNop())
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/j/subject_abstract" || req.URL.Query().Get("subject_id") != "1295644" {
			t.Fatalf("request = %s", req.URL.String())
		}
		body := `{"subject":{"title":"豆瓣详情","tmdb_id":603,"rating":9.4},"future_field":{"kept":true}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}

	match, err := provider.GetMatchByID(t.Context(), "1295644")
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Source != "douban" || match.DoubanID != "1295644" || match.TMDbID != 603 {
		t.Fatalf("match = %#v", match)
	}
	if !strings.Contains(string(match.RawJSON), `"future_field":{"kept":true}`) {
		t.Fatalf("raw json = %s", match.RawJSON)
	}
}

func TestDoubanProviderMatchPersistsSnapshotWithoutFusingDetailFields(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(&config.Config{}, zap.NewNop())
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"subject":{"title":"详情标题","tmdb_id":603,"rating":9.4},"future_field":{"kept":true}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider

	media := model.Media{Title: "扫描标题", Path: "/movie.mkv"}
	lib := model.Library{Type: "movie"}
	match := &Match{Source: "douban", MediaType: "movie", DoubanID: "1295644", Title: "已选择标题", Rating: 7.2}
	persisted, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Target.Title != "已选择标题" || persisted.Target.Rating != 7.2 {
		t.Fatalf("canonical fields were fused: %#v", persisted.Target)
	}
	if match.TMDbID != 603 {
		t.Fatalf("tmdb crosswalk = %d", match.TMDbID)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), persisted.Target.ID, "douban")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || !strings.Contains(snapshot.Payload, `"future_field":{"kept":true}`) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := repos.DB.Model(&model.MetadataProviderSnapshot{}).
		Where("metadata_id = ? AND provider = ?", persisted.Target.ID, "douban").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("snapshot count = %d, err = %v", count, err)
	}
}

func TestDoubanDetailFailureKeepsAcceptedMatchWithoutSnapshot(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(&config.Config{}, zap.NewNop())
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
	})}
	scraper.douban = provider

	media := model.Media{Title: "扫描标题", Path: "/movie.mkv"}
	lib := model.Library{Type: "movie"}
	match := &Match{Source: "douban", MediaType: "movie", DoubanID: "1295644", Title: "已选择标题", Rating: 7.2}
	persisted, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Target.Title != "已选择标题" {
		t.Fatalf("canonical title = %q", persisted.Target.Title)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), persisted.Target.ID, "douban")
	if err != nil || snapshot != nil {
		t.Fatalf("snapshot = %#v, err = %v", snapshot, err)
	}
}

func TestDoubanMovieEnrichmentOnlyFillsMissingFields(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	scraper.douban = NewDoubanProvider(&config.Config{}, zap.NewNop())
	metadata := model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "English title", Rating: 8.1, Source: "tmdb",
	}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "603"},
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"},
	}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"subject":{"title":"中文标题","summary":"中文简介","year":"1997","rating":9.4,"languages":["汉语","英语"],"tmdb_id":603}}`)
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), metadata.ID, "douban", raw, time.Now()); err != nil {
		t.Fatal(err)
	}
	result, err := scraper.enrichMovieFromDouban(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "中文标题" || updated.OriginalName != "English title" || updated.Overview != "中文简介" || updated.Year != 1997 {
		t.Fatalf("enriched metadata = %#v", updated)
	}
	if updated.Rating != 8.1 || updated.Source != "tmdb" || result.SnapshotSaved {
		t.Fatalf("existing fields or snapshot changed: metadata=%#v result=%#v", updated, result)
	}
}

func TestUniqueIdentifierRejectsAmbiguousDoubanMovieIDs(t *testing.T) {
	identifiers := []model.MetadataIdentifier{
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1"},
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "2"},
	}
	if value, ok := uniqueIdentifier(identifiers, "douban", model.MetadataKindMovie); ok || value != "2" {
		t.Fatalf("unique identifier = %q, %v", value, ok)
	}
}
