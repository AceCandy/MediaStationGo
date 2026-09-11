package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbMetadataRecheckPageBoundsConcurrencyAndCancellation(t *testing.T) {
	for _, cancelRunning := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelRunning), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			started := make(chan struct{}, 7)
			release := make(chan struct{})
			var requests, active, peak atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				n := active.Add(1)
				defer active.Add(-1)
				for previous := peak.Load(); n > previous && !peak.CompareAndSwap(previous, n); previous = peak.Load() {
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
				http.NotFound(w, r)
			}))
			defer upstream.Close()
			cfg := &config.Config{}
			cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
			s := &ScraperService{tmdb: NewTMDbProvider(cfg, zap.NewNop(), nil)}
			page := make([]repository.TMDbMetadataRecheckCandidate, 7)
			for i := range page {
				page[i] = repository.TMDbMetadataRecheckCandidate{MetadataID: fmt.Sprint(i), Kind: model.MetadataKindSeason, SeasonNum: i, SeriesTMDbID: "42"}
			}
			metrics := map[string]int64{}
			done := make(chan struct{})
			go func() {
				s.recheckTMDbMetadataPage(ctx, page, time.Now().UTC(), metrics, nil)
				close(done)
			}()
			for range 3 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("three concurrent requests did not start")
				}
			}
			if cancelRunning {
				cancel()
			}
			close(release)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("workers did not finish")
			}
			want := int64(7)
			if cancelRunning {
				want = 3
			}
			if peak.Load() != 3 || int64(requests.Load()) != want || metrics["scanned"] != want || metrics["requests"] != want || metrics["failed"] != want {
				t.Fatalf("peak=%d requests=%d metrics=%v want=%d", peak.Load(), requests.Load(), metrics, want)
			}
		})
	}
}

func TestTMDbEpisodeRecheckUpdatesReleaseDateAndDetectsCandidates(t *testing.T) {
	item := &model.MetadataItem{Title: "Episode 1", Overview: "Old", ReleaseDate: "2025-01-01", Rating: 1, Year: 2025}
	episode := &TMDbEpisodeDetails{Name: "Pilot", Overview: "New", AirDate: "2026-08-26", AirYear: 2026, Rating: 8}
	updates, _ := tmdbEpisodeMetadataUpdates(nil, episode, 0)
	changed := changedTMDbMetadataFields(item, updates)
	applyTMDbMetadataUpdates(item, updates)
	if item.Title != "Pilot" || item.Overview != "New" || item.ReleaseDate != "2026-08-26" || item.Year != 2026 || item.Rating != 8 {
		t.Fatalf("updated episode = %#v", item)
	}
	if len(changed) != 5 {
		t.Fatalf("changed fields = %#v", changed)
	}
	for _, kind := range []string{model.MetadataKindSeason, model.MetadataKindEpisode} {
		for _, title := range []string{"", "第 1 季", "特别篇", "Season 1", "第 1 集", "Episode 1", "Pilot"} {
			candidate := repository.TMDbMetadataRecheckCandidate{Kind: kind, Title: title, Overview: "Overview", ReleaseDate: "2026-08-26", TMDbID: "42"}
			if tmdbMetadataCandidateNeedsRecheck(candidate) {
				t.Fatalf("title alone triggered recheck: %s %q", kind, title)
			}
			missing := missingTMDbMetadataFields(&model.MetadataItem{Kind: kind, Title: title, Overview: candidate.Overview, ReleaseDate: candidate.ReleaseDate}, true, model.ArtworkTypeStill)
			if len(missing) != 0 {
				t.Fatalf("title counted as incomplete: %v", missing)
			}
			for _, field := range []string{"overview", "date", "artwork", "identifier", "snapshot"} {
				gap := candidate
				switch field {
				case "overview":
					gap.Overview = ""
				case "date":
					gap.ReleaseDate = ""
				case "artwork":
					gap.ArtworkMissing = true
				case "identifier":
					gap.TMDbID = ""
				case "snapshot":
					gap.SnapshotMissing = true
				}
				want := kind == model.MetadataKindSeason || (field != "identifier" && field != "snapshot")
				if tmdbMetadataCandidateNeedsRecheck(gap) != want {
					t.Fatalf("%s %s gap selected incorrectly", kind, field)
				}
			}
		}
	}
}

func TestTMDbMetadataRecheckRepairsSeasonsAndEpisodes(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{})
	if err := db.AutoMigrate(&model.TMDbRecheckJob{}, &model.TMDbRecheckSeasonLease{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckScan{}, &model.TMDbRecheckAssetChange{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureTMDbRecheckTriggers(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42"})
	var seasons, episodes []*model.MetadataItem
	var media []model.Media
	for i := 0; i < 4; i++ {
		season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: i,
			Title: "季名称", Overview: "保留的季简介", ReleaseDate: "2025-01-01", Year: 2025, Source: "tmdb"})
		if i == 0 || i == 2 {
			if err := db.Model(season).Update("overview", "").Error; err != nil {
				t.Fatal(err)
			}
		}
		if i != 0 {
			if err := db.Create(&model.MetadataIdentifier{MetadataID: season.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: fmt.Sprint(200 + i)}).Error; err != nil {
				t.Fatal(err)
			}
			createServiceTestArtwork(t, db, season.ID, model.ArtworkTypePoster, fmt.Sprintf("season-poster-%d", i))
		}
		if i == 3 {
			if err := db.Model(season).Update("title", "Season 3").Error; err != nil {
				t.Fatal(err)
			}
			if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), season.ID, "tmdb", []byte(`{"id":203}`), time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1,
			Title: "Episode 1", Overview: "集简介", ReleaseDate: "2025-01-01", Source: "tmdb"})
		if i == 3 {
			if err := db.Model(episode).Update("overview", "").Error; err != nil {
				t.Fatal(err)
			}
		}
		createServiceTestArtwork(t, db, episode.ID, model.ArtworkTypeStill, fmt.Sprintf("episode-still-%d", i))
		media = append(media, model.Media{MetadataID: episode.ID, Path: fmt.Sprintf("/test/s%d.mkv", i)})
		seasons, episodes = append(seasons, season), append(episodes, episode)
	}
	media = append(media, model.Media{MetadataID: episodes[0].ID, Path: "/test/s0-second-version.mkv"})
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	var beforeMedia []model.Media
	if err := db.Order("id").Find(&beforeMedia).Error; err != nil {
		t.Fatal(err)
	}
	var mode, requests, imageRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/tv/42/season/3" {
			_, _ = io.WriteString(w, `{"id":203,"season_number":3,"episodes":[{"id":901,"season_number":3,"episode_number":1,"name":"补全的单集","overview":"新的集简介","air_date":"2026-09-07"}]}`)
			return
		}
		seasonNum := map[string]int{"/tv/42/season/0": 0, "/tv/42/season/1": 1, "/tv/42/season/2": 2}
		number, ok := seasonNum[r.URL.Path]
		if !ok {
			t.Errorf("unexpected TMDb request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if number == 2 && mode.Load() == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		returnedNumber := number
		if mode.Load() == 2 {
			returnedNumber++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 200 + number, "season_number": returnedNumber, "name": "补全的季", "overview": "",
			"air_date": "2026-09-07", "vote_average": 8, "poster_path": "/poster.png",
			"episodes": []any{map[string]any{"id": 999, "season_number": number, "episode_number": 99, "name": "不能创建的目录集", "overview": "目录集简介"}},
			"credits":  map[string]any{"cast": []any{map[string]any{"id": 101, "name": "季演员", "character": "角色"}}, "crew": []any{}},
		})
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey, cfg.Secrets.TMDbAPIProxy = "test-key", upstream.URL
	cfg.Secrets.TMDbImageProxy = "https://images.example.test"
	cfg.App.DataDir = t.TempDir()
	cfg.Cache.CacheDir = filepath.Join(cfg.App.DataDir, "cache")
	s := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, nil)
	imageData := testArtworkPNG(t, 4, 3)
	images := NewImageProxy(cfg, zap.NewNop())
	images.client = &http.Client{Transport: imageRoundTripFunc(func(*http.Request) (*http.Response, error) {
		imageRequests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(imageData))}, nil
	})}
	s.SetArtworkStore(NewArtworkStore(cfg, repos.Artwork, images))
	s.tasks = NewTaskTrackerService(nil, nil)
	s.tasks.ConfigurePersistence(nil, t.TempDir())
	if err := s.runTMDbEpisodeMetadataRecheck(t.Context(), TaskTriggerManual); err == nil {
		t.Fatal("one failed season must make the task fail")
	}
	if requests.Load() != 4 || imageRequests.Load() != 1 {
		t.Fatalf("requests=%d images=%d; want three seasons, one episode and one missing poster", requests.Load(), imageRequests.Load())
	}
	metrics := s.tasks.Snapshot().Recent[0].Metrics
	if metrics["scan_files"] == 0 || metrics["change_batches"] == 0 {
		t.Fatalf("initialization progress missing: %v", metrics)
	}
	progressLog, err := s.tasks.ReadDefinitionLog("tmdb_episode_metadata_recheck", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"图片资产归并完成", "文件核对结束，本次", "季集变更归并完成", "尚未开始请求 TMDb", "本次处理登记", "新到期重试留待下轮", "本轮领取结束", "条元数据", "剩余到期 0 条", "详情请求（耗时", "阶段累计耗时", "阶段耗时", "累计耗时"} {
		if !strings.Contains(progressLog.Content, message) {
			t.Fatalf("progress missing %q: %s", message, progressLog.Content)
		}
	}
	if metrics["season_checked"] != 2 || metrics["episode_checked"] != 1 || metrics["failed"] != 1 || metrics["details_failed"] != 1 || metrics["remaining"] != 0 {
		t.Fatalf("task metrics = %v", metrics)
	}
	for _, number := range []int{0, 1} {
		stored, err := repos.Metadata.FindByID(t.Context(), seasons[number].ID)
		if err != nil || stored.Title != "补全的季" || stored.ReleaseDate != "2026-09-07" || stored.Rating != 8 || stored.Year != 2026 || stored.TMDbSeasonCheckedAt == nil || stored.TMDbEpisodeCheckedAt != nil {
			t.Fatalf("season %d was not repaired: %+v, %v", number, stored, err)
		}
		if number == 1 && stored.Overview != "保留的季简介" {
			t.Fatal("empty upstream overview cleared existing information")
		}
		assertServiceTestTMDbSnapshot(t, repos, stored.ID)
		identified, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindSeason, fmt.Sprint(200+number))
		if err != nil || identified == nil || identified.ID != stored.ID {
			t.Fatal("season identity did not preserve the original metadata", err)
		}
	}
	poster, err := repos.Artwork.FindSelection(t.Context(), seasons[0].ID, model.ArtworkTypePoster)
	if err != nil || poster == nil {
		t.Fatal("season poster not saved", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.App.DataDir, "artwork", poster.StorageKey)); err != nil {
		t.Fatal("season poster bytes missing", err)
	}
	preservedPoster, err := repos.Artwork.FindSelection(t.Context(), seasons[1].ID, model.ArtworkTypePoster)
	if err != nil || preservedPoster == nil || preservedPoster.ID != "season-poster-1" {
		t.Fatal("existing poster was replaced", err)
	}
	var credits int64
	if err := db.Model(&model.MetadataCredit{}).Where("metadata_id = ?", seasons[0].ID).Count(&credits).Error; err != nil || credits != 1 {
		t.Fatalf("season credits=%d, err=%v", credits, err)
	}
	repairedEpisode, err := repos.Metadata.FindByID(t.Context(), episodes[3].ID)
	if err != nil || repairedEpisode.Title != "补全的单集" || repairedEpisode.TMDbEpisodeCheckedAt == nil || repairedEpisode.TMDbSeasonCheckedAt != nil {
		t.Fatal("episode repair regressed", err)
	}
	failedSeason, err := repos.Metadata.FindByID(t.Context(), seasons[2].ID)
	if err != nil || failedSeason.TMDbSeasonCheckedAt != nil {
		t.Fatal("failed request advanced season cooldown", err)
	}
	mode.Store(1)
	if err := s.runTMDbEpisodeMetadataRecheck(t.Context(), TaskTriggerScheduled); err != nil || requests.Load() != 4 {
		t.Fatalf("retry backoff bypassed: %d %v", requests.Load(), err)
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", seasons[2].ID).Update("due_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.runTMDbEpisodeMetadataRecheck(t.Context(), TaskTriggerScheduled); err != nil || requests.Load() != 5 {
		t.Fatalf("retry must request only the failed season: requests=%d, err=%v", requests.Load(), err)
	}
	if err := s.runTMDbEpisodeMetadataRecheck(t.Context(), TaskTriggerManual); err != nil || requests.Load() != 5 {
		t.Fatalf("manual run bypassed cooldown: requests=%d, err=%v", requests.Load(), err)
	}
	var afterMedia []model.Media
	if err := db.Order("id").Find(&afterMedia).Error; err != nil || !reflect.DeepEqual(beforeMedia, afterMedia) {
		t.Fatal("media associations or file facts changed", err)
	}
	var count int64
	if err := db.Model(&model.MetadataItem{}).Count(&count).Error; err != nil || count != 9 {
		t.Fatalf("repair must not create inventory: metadata count=%d, err=%v", count, err)
	}
	old := time.Now().UTC().Add(-73 * time.Hour)
	if err := db.Model(seasons[0]).Update("tmdb_season_checked_at", old).Error; err != nil {
		t.Fatal(err)
	}
	candidates, err := repos.Metadata.ListTMDbSeasonMetadataRecheckAfter(t.Context(), "", time.Now().UTC().Add(-72*time.Hour), 200)
	if err != nil || len(candidates) != 1 || candidates[0].MetadataID != seasons[0].ID {
		t.Fatalf("remaining gaps must return after cooldown: %v, %v", candidates, err)
	}
	mode.Store(2)
	if _, err := s.recheckTMDbMetadata(t.Context(), candidates[0], time.Now().UTC(), map[string]int64{}); !errors.Is(err, ErrTMDbRefreshIdentity) {
		t.Fatalf("mismatched season number error = %v", err)
	}
	mode.Store(1)
	wrongID := candidates[0]
	wrongID.TMDbID = "999"
	if _, err := s.recheckTMDbMetadata(t.Context(), wrongID, time.Now().UTC(), map[string]int64{}); !errors.Is(err, ErrTMDbRefreshIdentity) {
		t.Fatalf("mismatched season ID error = %v", err)
	}
	if err := db.Exec(`CREATE FUNCTION reject_recheck_snapshot() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'snapshot write failed'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER reject_recheck_snapshot BEFORE INSERT OR UPDATE ON metadata_provider_snapshots FOR EACH ROW EXECUTE FUNCTION reject_recheck_snapshot()`).Error; err != nil {
		t.Fatal(err)
	}
	details, err := s.recheckTMDbMetadata(t.Context(), candidates[0], time.Now().UTC(), map[string]int64{})
	if err == nil || strings.Contains(strings.Join(details, " "), upstream.URL) || strings.Contains(strings.Join(details, " "), cfg.Secrets.TMDbAPIKey) {
		t.Fatalf("snapshot failure should be retryable with sanitized details: %v, %v", details, err)
	}
	stored, err := repos.Metadata.FindByID(t.Context(), seasons[0].ID)
	if err != nil || stored.TMDbSeasonCheckedAt == nil || stored.TMDbSeasonCheckedAt.After(old.Add(time.Second)) {
		t.Fatal("failed persistence advanced cooldown", err)
	}
}
