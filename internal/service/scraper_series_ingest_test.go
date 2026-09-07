package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSeriesInventoryBindsOwnEpisodesAndReusesSnapshot(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.AutoMigrate(&model.MediaProbeMetadata{}, &model.MetadataArtworkRecheck{}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var season int
		if _, err := fmt.Sscanf(r.URL.Path, "/tv/12345/season/%d", &season); err != nil || strings.Contains(r.URL.Path, "/episode/") {
			t.Error(r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"id":%d,"season_number":%d,"name":"季名","overview":"季简介","episodes":[{"id":%d,"episode_number":1,"name":"第一集","overview":"集简介","runtime":24},{"id":%d,"episode_number":2,"name":"第二集"}]}`, 200+season, season, 1000+season*10+1, 1000+season*10+2)
	}))
	defer upstream.Close()
	s.tmdb.cfg.Secrets.TMDbAPIProxy = upstream.URL
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "剧", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &series, catalogIdentifiers(model.MetadataKindSeries, 12345, TMDbExternalIDs{})); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "剧库", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	coords := [][2]int{{1, 1}, {1, 2}, {1, 1}, {2, 1}, {-1, 1}, {1, 9}, {1, 2}}
	rows := make([]model.Media, len(coords))
	group := scrapeCandidateGroup{}
	for i, c := range coords {
		rows[i] = model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, fmt.Sprintf("%d.mkv", i)), SeasonNum: c[0], EpisodeNum: c[1], TMDbID: 12345, SeriesID: "directory-hint", ScrapeStatus: "running"}
		if i == 6 {
			rows[i].TMDbID = 999
		}
		if err := repos.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
		group.MediaIDs = append(group.MediaIDs, rows[i].ID)
	}
	fresh := rows[0]
	fresh.MetadataID = series.ID
	fresh.ScrapeStatus = "matched"
	fresh.TheTVDBID = "477657"
	if err := repos.DB.Model(&model.Media{}).Where("id = ANY(?)", &group.MediaIDs).
		Update("lookup_thetvdb_id", "477126").Error; err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if err := s.syncScrapeSeriesGroup(t.Context(), group, &fresh, series.ID); err == nil {
			t.Fatal("want invalid coordinates and provider conflict errors")
		}
		for i := range rows {
			got, err := repos.Media.FindByID(t.Context(), rows[i].ID)
			if err != nil {
				t.Fatal(err)
			}
			if i == 4 || i == 6 {
				if got.MetadataID != "" || got.ScrapeStatus != "error" {
					t.Fatalf("invalid file bound: %+v", got)
				}
				continue
			}
			ep, err := repos.Metadata.FindEpisode(t.Context(), series.ID, coords[i][0], coords[i][1])
			if err != nil || ep == nil {
				t.Fatalf("episode: %v %v", ep, err)
			}
			if got.MetadataID != ep.ID || got.ScrapeStatus != "matched" {
				t.Fatalf("file %d linked to %s, want %s", i, got.MetadataID, ep.ID)
			}
			if got.SeriesID != "directory-hint" {
				t.Fatal("binding changed scan grouping hint")
			}
			if got.TheTVDBID != "477657" {
				t.Fatalf("file %d retained stale TVDB ID %q", i, got.TheTVDBID)
			}
			if ep.CatalogMetadataHydratedAt != nil || ep.CatalogHydratedAt != nil {
				t.Fatal("inventory incorrectly marked complete")
			}
			if coords[i][1] == 1 && (ep.Overview != "集简介" || ep.RuntimeSec != 1440) {
				t.Fatalf("missing basic fields: %+v", ep)
			}
		}
	}
	if requests.Load() != 3 {
		t.Fatalf("requests=%d, want two seasons plus one refresh for missing episode 9", requests.Load())
	}
	validGroup := group
	validGroup.MediaIDs = group.MediaIDs[:4]
	if err := s.syncScrapeSeriesGroup(t.Context(), validGroup, &fresh, series.ID); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatal("complete inventory was not reused")
	}
	season, err := repos.Metadata.FindSeason(t.Context(), series.ID, 1)
	if err != nil || season.Overview != "季简介" {
		t.Fatalf("season details: %+v %v", season, err)
	}
	ep, err := repos.Metadata.FindEpisode(t.Context(), series.ID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Model(ep).Updates(map[string]any{"overview": "完整简介", "catalog_metadata_hydrated_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ingestSeasonInventory(t.Context(), season, 12345); err != nil {
		t.Fatal(err)
	}
	ep, err = repos.Metadata.FindEpisode(t.Context(), series.ID, 1, 1)
	if err != nil || ep.Overview != "完整简介" {
		t.Fatal("summary overwrote full details", err)
	}
	if err := repos.DB.Model(season).Update("catalog_hydrated_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Model(&model.CatalogHydrationJob{}).Where("external_id = ?", "12345").Updates(map[string]any{"status": model.CatalogJobStatusCompleted, "attempts": 8}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.syncScrapeSeriesGroup(t.Context(), validGroup, &fresh, series.ID); err != nil {
		t.Fatal(err)
	}
	season, err = repos.Metadata.FindSeason(t.Context(), series.ID, 1)
	if err != nil || season.CatalogHydratedAt != nil {
		t.Fatal("incomplete children remained hidden by season checkpoint", err)
	}
	var job model.CatalogHydrationJob
	if err := repos.DB.Where("external_id = ?", "12345").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CatalogJobStatusPending || job.Attempts != 0 {
		t.Fatal("completed catalog job was not resumed")
	}
	group.Representative = rows[0]
	if err := repos.DB.Model(&model.Media{}).Where("id = ?", rows[0].ID).Updates(map[string]any{"scrape_status": "error", "scrape_error": "failed"}).Error; err != nil {
		t.Fatal(err)
	}
	other, err := repos.Media.FindByID(t.Context(), rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.syncScrapeCandidateGroup(t.Context(), group); err != nil {
		t.Fatal(err)
	}
	after, err := repos.Media.FindByID(t.Context(), rows[1].ID)
	if err != nil || after.MetadataID != other.MetadataID {
		t.Fatal("failed representative overwrote sibling attachment", err)
	}
}

func TestSeriesInventoryMissingEpisodesBindAndRecover(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.AutoMigrate(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	var published atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tv/12345/season/1" {
			fmt.Fprint(w, `{"id":101,"season_number":1,"episodes":[{"id":119,"episode_number":19,"name":"第十九集"}]}`)
			return
		}
		if r.URL.Path == "/tv/12345/season/1/episode/20" && published.Load() {
			fmt.Fprint(w, `{"id":120,"name":"新的开始","overview":"单集简介","air_date":"2026-09-06"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	s.tmdb.cfg.Secrets.TMDbAPIProxy = upstream.URL
	series := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "剧", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"})
	lib := model.Library{Name: "剧库", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	group := scrapeCandidateGroup{}
	for _, number := range []int{20, 21} {
		row := model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, fmt.Sprintf("S01E%d.strm", number)), SeasonNum: 1, EpisodeNum: number, ScrapeStatus: "error"}
		if err := repos.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		group.MediaIDs = append(group.MediaIDs, row.ID)
	}
	mediaService := NewMediaService(s.cfg, s.log, repos)
	var placeholderID string
	for round := 0; round < 2; round++ {
		if err := s.syncScrapeSeriesGroup(t.Context(), group, &model.Media{TMDbID: 12345}, series.ID); err != nil {
			t.Fatal(err)
		}
		for i, id := range group.MediaIDs {
			detail, err := mediaService.GetMedia(t.Context(), id)
			if err != nil || detail == nil {
				t.Fatalf("placeholder detail: %v", err)
			}
			if detail.ScrapeStatus != "matched" || detail.EpisodeNum != 20+i || detail.SeriesTMDbID != 12345 || detail.TMDbID != 0 || detail.TMDbSnapshot || detail.TMDbStatus != providerStatusMissing || detail.Overview != "" {
				t.Fatalf("unexpected placeholder: %+v", detail)
			}
			if i == 0 {
				if round > 0 && placeholderID != detail.MetadataID {
					t.Fatal("retry duplicated placeholder")
				}
				placeholderID = detail.MetadataID
			}
		}
	}
	candidates, err := repos.Metadata.ListTMDbEpisodeMetadataRecheckAfter(t.Context(), "", time.Now().UTC(), 200)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("ID-less placeholders must be eligible for recheck: %d, %v", len(candidates), err)
	}
	candidate := repository.TMDbEpisodeMetadataRecheckCandidate{MetadataID: placeholderID, SeriesTMDbID: "12345", SeasonNum: 1, EpisodeNum: 20, StillMissing: true}
	if _, err := s.recheckTMDbEpisodeMetadata(t.Context(), candidate, time.Now().UTC(), map[string]int64{}); err == nil {
		t.Fatal("missing upstream episode must remain retryable")
	}
	item, err := repos.Metadata.FindByID(t.Context(), placeholderID)
	if err != nil || item.TMDbEpisodeCheckedAt != nil {
		t.Fatal("failed recheck advanced checkpoint", err)
	}
	published.Store(true)
	if _, err := s.recheckTMDbEpisodeMetadata(t.Context(), candidate, time.Now().UTC(), map[string]int64{}); err != nil {
		t.Fatal(err)
	}
	detail, err := mediaService.GetMedia(t.Context(), group.MediaIDs[0])
	if err != nil || detail == nil {
		t.Fatal("recovered detail missing", err)
	}
	if detail.MetadataID != placeholderID || detail.TMDbID != 120 || !detail.TMDbSnapshot || detail.TMDbStatus != providerStatusPartial || detail.Title != "新的开始" || detail.ScrapeStatus != "matched" {
		t.Fatalf("placeholder did not recover in place: %+v", detail)
	}
}

func TestSeriesInventoryOnlySpecialsTolerateNotFound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		season int
		status int
	}{
		{"specials_not_found", 0, http.StatusNotFound},
		{"regular_not_found", 1, http.StatusNotFound},
		{"specials_unauthorized", 0, http.StatusUnauthorized},
		{"specials_server_error", 0, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repos, closeUpstream := newTestScraper(t)
			defer closeUpstream()
			if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
				t.Fatal(err)
			}
			var published atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != fmt.Sprintf("/tv/12345/season/%d", tc.season) {
					t.Error("unexpected endpoint", r.URL.Path)
				}
				if published.Load() {
					fmt.Fprint(w, `{"id":100,"season_number":0,"episodes":[{"id":101,"episode_number":1,"name":"特别篇上"},{"id":102,"episode_number":2,"name":"特别篇下"}]}`)
					return
				}
				w.WriteHeader(tc.status)
			}))
			defer upstream.Close()
			s.tmdb.cfg.Secrets.TMDbAPIProxy = upstream.URL
			series := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "剧", Source: "tmdb"},
				model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"})
			lib := model.Library{Name: "剧库", Path: t.TempDir(), Type: "tv", Enabled: true}
			if err := repos.Library.Create(t.Context(), &lib); err != nil {
				t.Fatal(err)
			}
			group := scrapeCandidateGroup{}
			for number := 1; number <= 2; number++ {
				row := model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, fmt.Sprintf("S%02dE%02d.strm", tc.season, number)), SeasonNum: tc.season, EpisodeNum: number, TMDbID: 12345, ScrapeStatus: "error"}
				if err := repos.DB.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				group.MediaIDs = append(group.MediaIDs, row.ID)
			}
			wantSuccess := tc.season == 0 && tc.status == http.StatusNotFound
			ids := make([]string, 2)
			for round := 0; round < 3; round++ {
				published.Store(wantSuccess && round == 2)
				err := s.syncScrapeSeriesGroup(t.Context(), group, &model.Media{TMDbID: 12345}, series.ID)
				if wantSuccess && err != nil || !wantSuccess && !isTMDbHTTPStatus(err, tc.status) {
					t.Fatalf("unexpected scrape result: %v", err)
				}
				for i, id := range group.MediaIDs {
					row, err := repos.Media.FindByID(t.Context(), id)
					if err != nil {
						t.Fatal(err)
					}
					if !wantSuccess {
						if row.ScrapeStatus != "error" || row.MetadataID != "" || row.ScrapeError == "" {
							t.Fatal("failed season must remain unresolved")
						}
						continue
					}
					ep, err := repos.Metadata.FindEpisode(t.Context(), series.ID, 0, i+1)
					if err != nil || ep == nil || row.MetadataID != ep.ID || row.ScrapeStatus != "matched" || row.ScrapeError != "" {
						t.Fatalf("specials did not bind: %v", err)
					}
					if round > 0 && ep.ID != ids[i] {
						t.Fatal("retry replaced the episode identity")
					}
					ids[i] = ep.ID
					identifiers, err := repos.Metadata.ListIdentifiers(t.Context(), ep.ID)
					if err != nil || round < 2 && len(identifiers) != 0 || round == 2 && len(identifiers) != 1 {
						t.Fatal("unexpected episode provider identity", err)
					}
					for _, metadataID := range []string{ep.ID, *ep.ParentID} {
						item, err := repos.Metadata.FindByID(t.Context(), metadataID)
						if err != nil || item.CatalogMetadataHydratedAt != nil || item.CatalogHydratedAt != nil || item.CatalogArtworkHydratedAt != nil {
							t.Fatal("inventory incorrectly marked complete", err)
						}
						if round < 2 {
							snapshot, err := repos.Metadata.FindProviderSnapshot(t.Context(), metadataID, "tmdb")
							if err != nil || snapshot != nil {
								t.Fatal("404 must not create a snapshot", err)
							}
						}
					}
				}
			}
		})
	}
}

func TestMediaScrapeRecordsWaitingBeforeLock(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	if err := repos.DB.AutoMigrate(&model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	s.tasks = NewTaskTrackerService(nil, nil)
	s.tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	lib := model.Library{Name: "剧库", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	row := model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, "ep.mkv"), ScrapeStatus: "pending", ScrapeTrigger: TaskTriggerManual}
	if err := repos.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.scrapeRunMu.Lock()
	done := make(chan error, 1)
	go func() { _, err := s.processNextMediaScrape(ctx); done <- err }()
	found := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := repos.DB.Model(&model.TaskExecution{}).Where("stage = ?", "waiting").Count(&count).Error; err != nil {
			break
		}
		if count == 1 {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	s.scrapeRunMu.Unlock()
	if err := <-done; err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if !found {
		t.Fatal("waiting execution was not persisted before the lock")
	}
	got, err := repos.Media.FindByID(t.Context(), row.ID)
	if err != nil || got.ScrapeStatus != "pending" {
		t.Fatal("cancel did not restore pending work", err)
	}
}

func TestCatalogYieldReleasesLockAndHandlesCancellation(t *testing.T) {
	s, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	lib := model.Library{Name: "剧库", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	row := model.Media{LibraryID: lib.ID, Path: filepath.Join(lib.Path, "ep.mkv"), ScrapeStatus: "pending"}
	if err := repos.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, cancelled := range []bool{false, true} {
		if err := repos.DB.Model(&row).Update("scrape_status", "pending").Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		s.scrapeRunMu.Lock()
		done := make(chan error, 1)
		go func() {
			s.scrapeRunMu.RLock()
			defer s.scrapeRunMu.RUnlock()
			if cancelled {
				cancel()
				done <- nil
				return
			}
			err := repos.DB.Model(&row).Update("scrape_status", "matched").Error
			s.wakeCatalogHydration()
			done <- err
		}()
		err := s.yieldCatalogToMedia(ctx, nil)
		s.scrapeRunMu.Unlock()
		if cancelled && err != context.Canceled || !cancelled && err != nil {
			t.Fatalf("yield cancelled=%v: %v", cancelled, err)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
