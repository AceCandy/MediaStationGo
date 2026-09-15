package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestNonTMDbIngestionFillsSnapshotWithoutChangingPresentation(t *testing.T) {
	for _, source := range []string{"local", "douban"} {
		for _, kind := range []string{model.MetadataKindMovie, model.MetadataKindSeries} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				db := newServiceTestDB(t, &model.MetadataProviderSnapshot{}, &model.Media{}, &model.Library{})
				repos := repository.New(db)
				var requests atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					wantPath := "/movie/42"
					if kind == model.MetadataKindSeries {
						wantPath = "/tv/42"
					}
					if r.URL.Path != wantPath {
						t.Errorf("unexpected detail path: %s", r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "title": "Remote title", "name": "Remote title", "overview": "Remote overview", "future_field": true})
				}))
				defer upstream.Close()
				cfg := &config.Config{}
				cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
				scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
				mediaType := "movie"
				if kind == model.MetadataKindSeries {
					mediaType = "tv"
				}
				lib := &model.Library{Type: mediaType}
				media := &model.Media{Title: "Original title"}
				for range 2 {
					var persisted *persistedMetadataMatch
					var err error
					if source == "local" {
						persisted, err = scraper.persistLocalMetadata(t.Context(), media, lib, &LocalMetadata{Title: "Original title", Overview: "Original overview", TMDbID: 42, HasNFO: true})
					} else {
						persisted, err = scraper.persistProviderMetadata(t.Context(), media, lib, &Match{Source: "douban", MediaType: mediaType, Title: "Original title", Overview: "Original overview", TMDbID: 42, RawJSON: json.RawMessage(`{"id":"douban-42","title":"Original title"}`)})
					}
					if err != nil {
						t.Fatal(err)
					}
					media.MetadataID = persisted.Target.ID
					item, err := repos.Metadata.FindByID(t.Context(), media.MetadataID)
					if err != nil || item.Source != source || item.Title != "Original title" || item.Overview != "Original overview" {
						t.Fatalf("presentation changed: %#v, %v", item, err)
					}
					assertServiceTestTMDbSnapshot(t, repos, media.MetadataID)
					snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), media.MetadataID, "tmdb")
					if err != nil {
						t.Fatal(err)
					}
					var raw map[string]any
					if err := json.Unmarshal([]byte(snapshot.Payload), &raw); err != nil || raw["future_field"] != true {
						t.Fatalf("raw snapshot not preserved: %v", err)
					}
				}
				if requests.Load() != 1 {
					t.Fatalf("detail requests = %d, want 1", requests.Load())
				}
			})
		}
	}
}

func TestTMDbSnapshotAutofillFailureKeepsIngestionRetryable(t *testing.T) {
	for _, response := range []string{"unavailable", `{"id":99,"title":"Wrong movie"}`, `{"id":`} {
		t.Run(response, func(t *testing.T) {
			db := newServiceTestDB(t, &model.MetadataProviderSnapshot{}, &model.Media{}, &model.Library{})
			repos := repository.New(db)
			var recovered atomic.Bool
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if recovered.Load() {
					_, _ = w.Write([]byte(`{"id":42,"title":"Remote"}`))
				} else {
					if response == "unavailable" {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					_, _ = w.Write([]byte(response))
				}
			}))
			defer upstream.Close()
			cfg := &config.Config{}
			cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
			scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
			media := &model.Media{Title: "Local title"}
			lib := &model.Library{Type: "movie"}
			local := &LocalMetadata{Title: "Local title", HasNFO: true}
			persisted, err := scraper.persistLocalMetadata(t.Context(), media, lib, local)
			if err != nil || requests.Load() != 0 {
				t.Fatalf("ingest without identifier: %v, requests=%d", err, requests.Load())
			}
			media.MetadataID = persisted.Target.ID
			local.TMDbID = 42
			cfg.Secrets.TMDbAPIKey = ""
			if _, err := scraper.persistLocalMetadata(t.Context(), media, lib, local); err != nil || requests.Load() != 0 {
				t.Fatalf("unconfigured TMDB blocked ingestion: %v, requests=%d", err, requests.Load())
			}
			cfg.Secrets.TMDbAPIKey = "test-key"
			if _, err := scraper.persistLocalMetadata(t.Context(), media, lib, local); err != nil {
				t.Fatalf("optional detail failure rejected NFO: %v", err)
			}
			if missing, err := repos.Metadata.CountMissingTMDbSnapshots(t.Context()); err != nil || missing != 1 {
				t.Fatalf("failed detail lost retry candidate: %d, %v", missing, err)
			}
			recovered.Store(true)
			if _, err := scraper.persistLocalMetadata(t.Context(), media, lib, local); err != nil {
				t.Fatal(err)
			}
			assertServiceTestTMDbSnapshot(t, repos, media.MetadataID)
			if requests.Load() != 2 {
				t.Fatalf("retry requests = %d, want 2", requests.Load())
			}
		})
	}
}

func TestTMDbSnapshotAutofillProtectsConcurrentWrites(t *testing.T) {
	for _, change := range []string{"identity", "snapshot"} {
		t.Run(change, func(t *testing.T) {
			db := newServiceTestDB(t, &model.MetadataProviderSnapshot{})
			repos := repository.New(db)
			item := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Local", Source: "local"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "42"})
			started, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-resume:
				case <-r.Context().Done():
					return
				}
				_, _ = w.Write([]byte(`{"id":42,"title":"Old response"}`))
			}))
			defer upstream.Close()
			cfg := &config.Config{}
			cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
			scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
			go func() {
				defer close(done)
				scraper.ensureTMDbSnapshot(t.Context(), item.ID, item.Kind, 42)
			}()
			<-started
			if change == "identity" {
				if err := repos.Metadata.ReplaceIdentifier(t.Context(), item.ID, "tmdb", item.Kind, "99"); err != nil {
					t.Fatal(err)
				}
			} else if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", json.RawMessage(`{"id":42,"title":"New response"}`), time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			close(resume)
			<-done
			snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), item.ID, "tmdb")
			if err != nil {
				t.Fatal(err)
			}
			if change == "identity" {
				if snapshot != nil {
					t.Fatal("stale identity response was persisted")
				}
			} else {
				var raw map[string]any
				if snapshot == nil || json.Unmarshal([]byte(snapshot.Payload), &raw) != nil || raw["title"] != "New response" {
					t.Fatal("concurrent snapshot was overwritten")
				}
			}
		})
	}
}
