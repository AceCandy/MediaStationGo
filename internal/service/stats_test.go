package service

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestStatsComputeFiltersDisabledLibraries(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.User{})
	repos := repository.New(db)
	enabled := &model.Library{Name: "电影", Path: "/media/movies", Type: "movie", Enabled: true}
	disabled := &model.Library{Name: "停用库", Path: "/media/disabled", Type: "movie", Enabled: false}
	if err := repos.Library.Create(t.Context(), enabled); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), disabled); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Library{}).Where("id = ?", disabled.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	mediaRows := []*model.Media{
		{LibraryID: enabled.ID, Title: "Visible", Path: "/media/movies/a.mkv", SizeBytes: 100},
		{LibraryID: disabled.ID, Title: "Hidden", Path: "/media/disabled/b.mkv", SizeBytes: 900},
	}
	for _, media := range mediaRows {
		if err := repos.Media.Upsert(t.Context(), media); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&[]model.MediaProbeMetadata{
		{MediaID: mediaRows[0].ID, ProbeJSON: "{}", SchemaVersion: 1, SummaryVersion: 1, SizeBytes: 100, ProbedAt: time.Now()},
		{MediaID: mediaRows[1].ID, ProbeJSON: "{}", SchemaVersion: 1, SummaryVersion: 1, SizeBytes: 900, ProbedAt: time.Now()},
	}).Error; err != nil {
		t.Fatal(err)
	}

	snap, err := NewStatsService(zap.NewNop(), repos).Compute(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Libraries != 1 || snap.MediaCount != 1 || snap.TotalSizeBytes != 100 {
		t.Fatalf("stats = libraries=%d media=%d size=%d, want 1/1/100", snap.Libraries, snap.MediaCount, snap.TotalSizeBytes)
	}
}
