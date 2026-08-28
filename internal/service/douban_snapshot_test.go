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
	provider := NewDoubanProvider(&config.Config{}, zap.NewNop())
	requests := 0
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		body := `{"subject":{"title":"中文标题","summary":"中文简介","year":"1997","rating":9.4,"languages":["汉语","英语"],"tmdb_id":603}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider
	metadata := model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "English title", Rating: 8.1, Source: "tmdb",
	}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{
		{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "603"},
		{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"},
	}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"subject":{"title":"旧标题"}}`)
	previousFetchedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), metadata.ID, "douban", raw, previousFetchedAt); err != nil {
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
	if updated.Rating != 8.1 || updated.Source != "tmdb" || !result.SnapshotSaved || requests != 1 {
		t.Fatalf("existing fields or snapshot changed: metadata=%#v result=%#v", updated, result)
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot == nil || !snapshot.FetchedAt.After(previousFetchedAt) || !strings.Contains(snapshot.Payload, `"中文简介"`) {
		t.Fatalf("refreshed snapshot = %#v, err = %v", snapshot, err)
	}
}

func TestDoubanMovieEnrichmentFailureDoesNotAdvanceSnapshotCooldown(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	provider := NewDoubanProvider(&config.Config{}, zap.NewNop())
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
	})}
	scraper.douban = provider
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "English title", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: "1295644"}}); err != nil {
		t.Fatal(err)
	}
	fetchedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), metadata.ID, "douban", []byte(`{"subject":{}}`), fetchedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := scraper.enrichMovieFromDouban(t.Context(), metadata.ID); err == nil {
		t.Fatal("expected douban request failure")
	}
	snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), metadata.ID, "douban")
	if err != nil || snapshot == nil || !snapshot.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("snapshot cooldown changed after failure: %#v, err = %v", snapshot, err)
	}
}

func TestDoubanMovieEnrichmentMetricsDistinguishUpdatedAndUnchanged(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	provider := NewDoubanProvider(&config.Config{}, zap.NewNop())
	provider.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		title, summary := "已有中文标题", "已有简介"
		if req.URL.Query().Get("subject_id") == "2" {
			title, summary = "补齐中文标题", "补齐简介"
		}
		body := `{"subject":{"title":"` + title + `","summary":"` + summary + `"}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	scraper.douban = provider
	for _, item := range []struct {
		title, overview, doubanID string
	}{
		{title: "已有中文标题", overview: "已有简介", doubanID: "1"},
		{title: "English", doubanID: "2"},
	} {
		metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: item.title, Overview: item.overview, Source: "tmdb"}
		if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{Provider: "douban", EntityKind: model.MetadataKindMovie, ExternalID: item.doubanID}}); err != nil {
			t.Fatal(err)
		}
	}
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	scraper.SetTaskTracker(tasks)
	previousDelay := doubanMovieEnrichmentDelay
	doubanMovieEnrichmentDelay = 0
	defer func() { doubanMovieEnrichmentDelay = previousDelay }()

	if err := scraper.runDoubanMovieEnrichment(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	snapshot := tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Metrics["updated"] != 1 || snapshot.Recent[0].Metrics["unchanged"] != 1 {
		t.Fatalf("douban enrichment metrics = %#v", snapshot.Recent)
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
