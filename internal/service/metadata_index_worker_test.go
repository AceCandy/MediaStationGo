package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type metadataIndexTestBackend struct {
	repository.MediaSearchSyncBackend
	remove func() error
	upsert func(repository.MetadataSearchDocument) error
}

func (b *metadataIndexTestBackend) UpsertMetadata(_ context.Context, document repository.MetadataSearchDocument) error {
	return b.upsert(document)
}

func (b *metadataIndexTestBackend) DeleteMetadata(context.Context, string) error { return b.remove() }

func TestMetadataIndexWorkerRetriesAndPreservesNewRevision(t *testing.T) {
	db := newServiceTestDB(t, model.AllModels()...)
	repos := repository.New(db)
	cache := NewRuntimeCacheService(nil, nil)
	scraper := &ScraperService{repo: repos, cache: cache}
	ctx := t.Context()
	before := time.Now()
	job := model.MetadataIndexJob{MetadataID: "removed", Revision: 1, NextAt: before.Add(-time.Second)}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	backend := &metadataIndexTestBackend{remove: func() error { return errors.New("index unavailable") }}
	repos.MediaView.SetSearchBackend(backend)
	for _, key := range []string{"media:list:test", "media:emby:test", "stats:test"} {
		cache.SetJSON(ctx, key, "stale", time.Hour)
	}
	if err := scraper.refreshPendingMetadataIndex(ctx); err == nil {
		t.Fatal("failed index refresh was acknowledged")
	}
	var saved model.MetadataIndexJob
	if err := db.First(&saved, "metadata_id=?", job.MetadataID).Error; err != nil || saved.Revision != 1 || saved.NextAt.Before(before.Add(50*time.Second)) {
		t.Fatalf("retry=%+v err=%v", saved, err)
	}
	for _, key := range []string{"media:list:test", "media:emby:test", "stats:test"} {
		var stale string
		if cache.GetJSON(ctx, key, &stale) {
			t.Fatalf("stale cache retained: %s", key)
		}
	}
	if err := db.Model(&job).UpdateColumn("next_at", before).Error; err != nil {
		t.Fatal(err)
	}
	backend.remove = func() error {
		return db.Model(&model.MetadataIndexJob{}).Where("metadata_id=?", job.MetadataID).Updates(map[string]any{"revision": 2, "next_at": before}).Error
	}
	if err := scraper.refreshPendingMetadataIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&saved, "metadata_id=?", job.MetadataID).Error; err != nil || saved.Revision != 2 {
		t.Fatalf("new revision lost: %+v %v", saved, err)
	}
	backend.remove = func() error { return nil }
	if err := scraper.refreshPendingMetadataIndex(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.MetadataIndexJob{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("completed job retained: %d %v", count, err)
	}
}

func TestMetadataIndexWorkerBatchesSeriesAndPreservesRetries(t *testing.T) {
	db := newServiceTestDB(t, model.AllModels()...)
	repos := repository.New(db)
	scraper := &ScraperService{repo: repos}
	series := createServiceSourceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "批量更新", Source: "tmdb"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, Source: "tmdb"})
	library := model.Library{Name: "test", Type: "tv", Path: "/test"}
	if err := db.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Second)
	jobs := []model.MetadataIndexJob{{MetadataID: "removed", Revision: 1, NextAt: before}}
	for i := 1; i <= 3; i++ {
		episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: i, Source: "tmdb"})
		media := model.Media{MetadataID: episode.ID, LibraryID: library.ID, Path: fmt.Sprintf("/test/%d.mkv", i)}
		if err := db.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, model.MetadataIndexJob{MetadataID: episode.ID, Revision: 1, NextAt: before})
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	writes := 0
	backend := &metadataIndexTestBackend{
		upsert: func(document repository.MetadataSearchDocument) error {
			if document.ID != series.ID || document.Title != "批量更新" {
				t.Fatalf("unexpected document: %+v", document)
			}
			writes++
			return nil
		},
		remove: func() error { return errors.New("index unavailable") },
	}
	repos.MediaView.SetSearchBackend(backend)
	if err := scraper.refreshPendingMetadataIndex(t.Context()); err == nil {
		t.Fatal("batch failure was acknowledged")
	}
	if writes != 1 {
		t.Fatalf("series refreshed %d times, want 1", writes)
	}
	var pending []model.MetadataIndexJob
	if err := db.Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if len(pending) != len(jobs) {
		t.Fatalf("failed jobs lost: %+v", pending)
	}
	for _, job := range pending {
		if job.Revision != 1 || job.NextAt.Before(before.Add(50*time.Second)) {
			t.Fatalf("invalid retry: %+v", job)
		}
	}
	if err := db.Model(&model.MetadataIndexJob{}).Where("1=1").UpdateColumn("next_at", before).Error; err != nil {
		t.Fatal(err)
	}
	writes = 0
	backend.remove = func() error { return nil }
	backend.upsert = func(document repository.MetadataSearchDocument) error {
		writes++
		return db.Model(&model.MetadataIndexJob{}).Where("metadata_id=?", jobs[1].MetadataID).Updates(map[string]any{"revision": 2, "next_at": before}).Error
	}
	if err := scraper.refreshPendingMetadataIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending = nil
	if err := db.Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if writes != 1 || len(pending) != 1 || pending[0].MetadataID != jobs[1].MetadataID || pending[0].Revision != 2 {
		t.Fatalf("batch completion lost new revision: writes=%d pending=%+v", writes, pending)
	}
}
