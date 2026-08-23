package service

import (
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestScannerLogsFirstExistingMetadataBindingOnce(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateScraperTestModels(t, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "已有电影", Source: "tmdb"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "27205"}).Error; err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scraper := NewScraperService(&config.Config{}, log, repos, nil, nil, nil, nil, NewHub(log))
	tracker := NewTaskTrackerService(log, nil)
	scraper.SetTaskTracker(tracker)
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, scraper)
	mediaPath := filepath.Join(library.Path, "已有电影.mkv")

	first := &model.Media{LibraryID: library.ID, Title: "已有电影", Path: mediaPath, TMDbID: 27205}
	if err := scanner.upsertLocalScanMedia(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	stored, err := repos.Media.FindByID(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MetadataID != metadata.ID || stored.ScrapeStatus != "matched" {
		t.Fatalf("metadata=%q status=%q, want existing metadata match", stored.MetadataID, stored.ScrapeStatus)
	}
	snapshot := tracker.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Kind != TaskKindScrape || snapshot.Recent[0].Trigger != TaskTriggerEvent || len(snapshot.Recent[0].Details) != 1 || !strings.Contains(snapshot.Recent[0].Details[0], "命中已有元数据") {
		t.Fatalf("task snapshot = %#v", snapshot)
	}

	rescan := &model.Media{LibraryID: library.ID, Title: "已有电影", Path: mediaPath, TMDbID: 27205}
	if err := scanner.upsertLocalScanMedia(t.Context(), rescan); err != nil {
		t.Fatal(err)
	}
	if got := tracker.Snapshot(); len(got.Recent) != 1 {
		t.Fatalf("rescan created duplicate match log: %#v", got.Recent)
	}
}
