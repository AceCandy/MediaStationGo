package repository

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTMDbRecheckInventoryIssuePreservesCompleteTargetsAndLeases(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	series := model.MetadataItem{Kind: "series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 20, Overview: "本地简介", ReleaseDate: time.Now().UTC().AddDate(0, 0, -100).Format(time.DateOnly)}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{MetadataID: episode.ID, Path: "/test/missing.mkv"}).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "test", StorageKey: "test", MimeType: "image/png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataArtwork{MetadataID: episode.ID, AssetID: asset.ID, ArtworkType: "still"}).Error; err != nil {
		t.Fatal(err)
	}
	record := func() {
		t.Helper()
		if err := repo.RecordTMDbInventoryMissing(t.Context(), episode.ID, series.ID, "42", 1, 20); err != nil {
			t.Fatal(err)
		}
	}
	read := func() model.TMDbRecheckJob {
		t.Helper()
		var job model.TMDbRecheckJob
		if err := db.First(&job, "metadata_id=?", episode.ID).Error; err != nil {
			t.Fatal(err)
		}
		return job
	}
	for _, raw := range []string{`{"id":10,"season_number":1}`, `{"id":10,"season_number":1,"episodes":[{"episode_number":20}]}`} {
		if err := repo.UpsertProviderSnapshot(t.Context(), season.ID, "tmdb", []byte(raw), time.Now()); err != nil {
			t.Fatal(err)
		}
		record()
		var count int64
		if err := db.Model(&model.TMDbRecheckJob{}).Where("status='not_found'").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("invalid/present inventory marked missing: %d %v", count, err)
		}
	}
	if err := repo.UpsertProviderSnapshot(t.Context(), season.ID, "tmdb", []byte(`{"id":10,"season_number":1,"episodes":[]}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := db.Create(&model.TMDbRecheckJob{MetadataID: episode.ID, Status: "running", LeaseToken: "other-owner", LeaseUntil: &future, DueAt: &future}).Error; err != nil {
		t.Fatal(err)
	}
	record()
	if got := read(); got.Status != "running" || got.LeaseToken != "other-owner" {
		t.Fatalf("active lease replaced: %+v", got)
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", episode.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	record()
	first := read()
	if first.Status != "not_found" || first.NotFoundIdentity == "" || first.DueAt == nil {
		t.Fatalf("missing issue: %+v", first)
	}
	if first.DueAt.Before(before.Add(10*24*time.Hour)) || first.DueAt.After(time.Now().Add(10*24*time.Hour)) {
		t.Fatalf("inventory cooldown ignored air date: %+v", first)
	}
	record()
	if got := read(); !got.DueAt.Equal(*first.DueAt) {
		t.Fatal("cooldown extended")
	}
	for {
		more, err := repo.ExpandTMDbRecheckChange(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if got := read(); got.Status != "not_found" || !got.DueAt.Equal(*first.DueAt) {
		t.Fatalf("local complete metadata erased issue: %+v", got)
	}
	if err := repo.RecordTMDbInventoryMissing(t.Context(), episode.ID, series.ID, "999", 1, 20); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.NotFoundIdentity != first.NotFoundIdentity || !got.DueAt.Equal(*first.DueAt) {
		t.Fatal("stale identity replaced issue")
	}
}
