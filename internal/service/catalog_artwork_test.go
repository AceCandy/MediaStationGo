package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestCatalogArtworkMetadataFirstRetryAndRestart(t *testing.T) {
	s, repos, cleanup := newTestScraper(t)
	defer cleanup()
	s.SetPeopleImageStore(NewPeopleImageStore(s.cfg, repos.Person, s.images))
	var detailsCalls, imageCalls, profileCalls atomic.Int32
	s.tmdb.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		detailsCalls.Add(1)
		body := `{"id":12345,"name":"测试剧集","seasons":[{"id":501,"season_number":1}],"credits":{"cast":[{"id":99,"name":"演员","profile_path":"/actor.jpg"}]},"poster_path":"/poster.jpg"}`
		if strings.Contains(req.URL.Path, "/season/") {
			body = `{"id":501,"season_number":1,"name":"第一季","episodes":[{"id":601,"season_number":1,"episode_number":1,"name":"第一集","overview":"剧情一","still_path":"/e1.jpg"},{"id":602,"season_number":1,"episode_number":2,"name":"第二集","overview":"剧情二","still_path":"/e2.jpg"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
	})}
	imageData := testArtworkPNG(t, 4, 3)
	started := make(chan struct{}, 1)
	var block, fail atomic.Bool
	block.Store(true)
	fail.Store(true)
	s.images.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		imageCalls.Add(1)
		if strings.HasSuffix(req.URL.Path, "/actor.jpg") {
			profileCalls.Add(1)
		}
		if block.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		status := http.StatusOK
		if fail.Load() && strings.HasSuffix(req.URL.Path, "/e1.jpg") {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(imageData)), Request: req}, nil
	})}
	if err := s.QueueCatalogHydrationContext(t.Context(), []ExternalMediaResult{{Source: "tmdb", MediaType: "tv", TMDbID: 12345}}); err != nil {
		t.Fatal(err)
	}
	processCatalogStage(t, s, model.CatalogJobStageRoot)
	if imageCalls.Load() != 0 || detailsCalls.Load() != 2 {
		t.Fatalf("metadata waited for images or repeated details: images=%d details=%d", imageCalls.Load(), detailsCalls.Load())
	}
	var items []model.MetadataItem
	if err := repos.DB.Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("metadata count = %d", len(items))
	}
	for _, item := range items {
		if item.CatalogMetadataHydratedAt == nil || item.CatalogHydratedAt == nil || item.CatalogArtworkDueAt == nil || item.CatalogArtworkHydratedAt != nil {
			t.Fatalf("incorrect handoff: %+v", item)
		}
	}
	var job model.CatalogHydrationJob
	if err := repos.DB.First(&job).Error; err != nil || job.Status != model.CatalogJobStatusCompleted {
		t.Fatalf("catalog job did not complete: %s %v", job.Status, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	s.StartCatalogArtworkWorker(ctx)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("image worker did not start")
	}
	if err := s.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerManual); !errors.Is(err, ErrSchedulerJobAlreadyRunning) {
		t.Fatalf("concurrent image pass: %v", err)
	}
	cancel()
	s.WaitCatalogHydrationWorker()
	block.Store(false)
	// 新实例没有运行内唤醒信号，仍应从数据库恢复首次图片。
	restarted := NewScraperService(s.cfg, s.log, repos, s.tmdb, nil, nil, nil, s.hub).SetArtworkStore(s.artwork).SetPeopleImageStore(s.people)
	if err := restarted.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerEvent); err == nil {
		t.Fatal("expected one image failure")
	}
	var failed model.MetadataItem
	if err := repos.DB.Where("catalog_artwork_due_at IS NOT NULL").First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.EpisodeNum != 1 || failed.CatalogArtworkAttempts != 1 || failed.CatalogArtworkHydratedAt != nil || !failed.CatalogArtworkDueAt.After(time.Now()) {
		t.Fatalf("failed image checkpoint: %+v", failed)
	}
	var pending int64
	if err := repos.DB.Model(&model.MetadataItem{}).Where("catalog_artwork_due_at IS NOT NULL").Count(&pending).Error; err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	before := imageCalls.Load()
	if err := restarted.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerEvent); err != nil || imageCalls.Load() != before {
		t.Fatalf("cooldown ignored: %v", err)
	}
	fail.Store(false)
	if err := repos.DB.Model(&failed).UpdateColumn("catalog_artwork_due_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := restarted.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerEvent); err != nil {
		t.Fatal(err)
	}
	if imageCalls.Load() != before+1 || detailsCalls.Load() != 2 || profileCalls.Load() != 1 {
		t.Fatalf("unnecessary downloads: images=%d/%d details=%d profiles=%d", imageCalls.Load(), before, detailsCalls.Load(), profileCalls.Load())
	}
	if next, err := repos.Metadata.NextCatalogArtworkAt(t.Context()); err != nil || next != nil {
		t.Fatalf("pending after success: %v %v", next, err)
	}
	asset, err := repos.Artwork.FindSelection(t.Context(), failed.ID, model.ArtworkTypeStill)
	if err != nil || asset == nil {
		t.Fatalf("missing local still: %v", err)
	}
	path, err := s.artwork.pathForStorageKey(asset.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if available, err := localArtworkFileAvailable(path); !available || err != nil {
		t.Fatalf("local still not available: %v", err)
	}
	var person model.Person
	if err := repos.DB.First(&person).Error; err != nil || !s.people.hasUsableImage(person.ProfileImageKey) {
		t.Fatalf("avatar not localized: %v", err)
	}
	var noImage model.MetadataArtworkRecheck
	if err := repos.DB.Where("metadata_id = ? AND artwork_type = ?", personMetadataID(t, repos, person.ID), model.ArtworkTypeBackdrop).First(&noImage).Error; err != nil {
		t.Fatalf("explicit no-image not handed off: %v", err)
	}
	// 清空运行内头像缓存后，仍凭持久化的来源/key 对复用成功文件。
	restarted.SetPeopleImageStore(NewPeopleImageStore(s.cfg, repos.Person, s.images))
	before = imageCalls.Load()
	if err := restarted.downloadCatalogProfiles(t.Context(), noImage.MetadataID, map[string]int64{}); err != nil || imageCalls.Load() != before {
		t.Fatalf("restart redownloaded a completed avatar: %v", err)
	}
}

func personMetadataID(t *testing.T, repos *repository.Container, personID string) string {
	t.Helper()
	var credit model.MetadataCredit
	if err := repos.DB.Where("person_id = ?", personID).First(&credit).Error; err != nil {
		t.Fatal(err)
	}
	return credit.MetadataID
}

func TestCatalogArtworkRejectsStaleSnapshotAndPreservesManualSelection(t *testing.T) {
	s, repos, cleanup := newTestScraper(t)
	defer cleanup()
	now := time.Now().UTC()
	item := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "电影", Source: "tmdb", CatalogMetadataHydratedAt: &now}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "movie", ExternalID: "42"})
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(`{"id":42,"poster_path":"/old.jpg"}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.queueCatalogArtwork(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	item, _ = repos.Metadata.FindByID(t.Context(), item.ID)
	snapshot, _ := repos.Metadata.FindProviderSnapshot(t.Context(), item.ID, "tmdb")
	asset, err := s.artwork.prepareAsset(item.ID, model.ArtworkTypePoster, testArtworkPNG(t, 4, 3))
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(`{"id":42,"poster_path":"/new.jpg"}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Metadata.SaveCatalogArtworkAsset(t.Context(), *item, snapshot, "poster", "https://images.example.test/old.jpg", asset); !errors.Is(err, repository.ErrCatalogArtworkChanged) {
		t.Fatalf("stale image accepted: %v", err)
	}
	if err := repos.Metadata.CompleteCatalogArtwork(t.Context(), *item, snapshot, nil); !errors.Is(err, repository.ErrCatalogArtworkChanged) {
		t.Fatalf("stale completion accepted: %v", err)
	}
	manual, err := s.artwork.prepareAsset(item.ID, model.ArtworkTypePoster, testArtworkPNG(t, 7, 5))
	if err != nil {
		t.Fatal(err)
	}
	manual, err = repos.Artwork.SaveSelection(t.Context(), item.ID, "poster", "manual", "manual.png", manual)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ = repos.Metadata.FindProviderSnapshot(t.Context(), item.ID, "tmdb")
	if selected, err := repos.Metadata.SaveCatalogArtworkAsset(t.Context(), *item, snapshot, "poster", "https://images.example.test/new.jpg", asset); err != nil || selected {
		t.Fatalf("manual selection replaced: %v %v", selected, err)
	}
	selected, err := repos.Artwork.FindSelection(t.Context(), item.ID, "poster")
	if err != nil || selected.ID != manual.ID {
		t.Fatal("manual image changed")
	}
}

func TestCatalogArtworkMergeAndIdleWake(t *testing.T) {
	s, repos, cleanup := newTestScraper(t)
	defer cleanup()
	now := time.Now().UTC()
	source := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "来源", Source: "tmdb", CatalogMetadataHydratedAt: &now}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "movie", ExternalID: "42"})
	target := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "保留资料", Source: "tmdb", CatalogMetadataHydratedAt: &now})
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), source.ID, "tmdb", []byte(`{"id":42}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.queueCatalogArtwork(t.Context(), source.ID); err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.Merge(t.Context(), source.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	target, err := repos.Metadata.FindByID(t.Context(), target.ID)
	if err != nil || target.CatalogArtworkDueAt == nil {
		t.Fatalf("merge lost artwork handoff: %+v %v", target, err)
	}
	if err := s.runTMDbArtworkLocalRepair(t.Context(), TaskTriggerEvent); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer func() { cancel(); s.WaitCatalogHydrationWorker() }()
	s.tasks = NewTaskTrackerService(nil, nil)
	s.StartCatalogArtworkWorker(ctx)
	item := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "新增资料", Source: "tmdb", CatalogMetadataHydratedAt: &now}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "movie", ExternalID: "43"})
	if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(`{"id":43}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.queueCatalogArtwork(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := repos.Metadata.FindByID(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.CatalogArtworkHydratedAt != nil && current.CatalogArtworkDueAt == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("idle image worker did not consume newly committed handoff")
}
