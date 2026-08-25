package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestDuplicateDetectMarksExternalIdentityDuplicates(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	if err := repos.DB.AutoMigrate(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	firstPath := filepath.Join(root, "show-a.mkv")
	secondPath := filepath.Join(root, "show-b.mkv")
	writeOrgFile(t, firstPath, "first-release")
	writeOrgFile(t, secondPath, "second-release")

	lib := model.Library{Name: "剧集", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	first := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         firstPath,
		SizeBytes:    13,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "matched",
	}
	second := model.Media{
		LibraryID:    lib.ID,
		Title:        "Spy Family",
		Path:         secondPath,
		SizeBytes:    14,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "matched",
	}
	if err := repos.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	report, err := NewDuplicateService(zap.NewNop(), repos, nil).Detect(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.ItemsMarked != 1 || report.GroupsFound != 1 {
		t.Fatalf("report = %#v, want one external identity duplicate", report)
	}
	if report.Groups[0].Primary.SizeBytes != 0 || report.Groups[0].Duplicates[0].SizeBytes != 0 {
		t.Fatalf("duplicate report used legacy media sizes: %#v", report.Groups[0])
	}
	var rows []model.Media
	if err := repos.DB.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	marked := 0
	for _, row := range rows {
		if row.IsDuplicate && row.DuplicateOf != "" {
			marked++
		}
	}
	if marked != 1 {
		t.Fatalf("marked duplicate rows = %d, want 1; rows=%#v", marked, rows)
	}
}

func TestDuplicateCurrentUsesProbeSizes(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	if err := repos.DB.AutoMigrate(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	primary := model.Media{PermanentBase: model.PermanentBase{ID: "primary"}, LibraryID: lib.ID, Title: "主版本", Path: filepath.Join(lib.Path, "primary.mkv"), SizeBytes: 11, FileHash: "same"}
	duplicate := model.Media{PermanentBase: model.PermanentBase{ID: "duplicate"}, LibraryID: lib.ID, Title: "重复版本", Path: filepath.Join(lib.Path, "duplicate.mkv"), SizeBytes: 22, FileHash: "same", IsDuplicate: true, DuplicateOf: primary.ID}
	if err := repos.DB.Create([]model.Media{primary, duplicate}).Error; err != nil {
		t.Fatal(err)
	}
	probedAt := time.Now()
	probes := []model.MediaProbeMetadata{
		{MediaID: primary.ID, ProbeJSON: "{}", SchemaVersion: 1, SummaryVersion: 1, SizeBytes: 1000, ProbedAt: probedAt},
		{MediaID: duplicate.ID, ProbeJSON: "{}", SchemaVersion: 1, SummaryVersion: 1, SizeBytes: 2000, ProbedAt: probedAt},
	}
	if err := repos.DB.Create(&probes).Error; err != nil {
		t.Fatal(err)
	}

	report, err := NewDuplicateService(zap.NewNop(), repos, nil).Current(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Groups) != 1 || report.Groups[0].Primary.SizeBytes != 1000 || len(report.Groups[0].Duplicates) != 1 || report.Groups[0].Duplicates[0].SizeBytes != 2000 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestDuplicateMediaJSONExposesOnlyPageFields(t *testing.T) {
	payload, err := json.Marshal(newDuplicateMedia(model.Media{
		PermanentBase: model.PermanentBase{ID: "media"}, Title: "标题", Path: "/media/movie.mkv", SizeBytes: 10,
		DurationSec: 20, Container: "mkv", STRMURL: "https://secret.example/movie", FileHash: "hash",
	}))
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(payload)
	for _, forbidden := range []string{"duration_sec", "container", "strm_url", "file_hash", "video_codec", "audio_codec"} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("duplicate media JSON exposed %q: %s", forbidden, jsonText)
		}
	}
}
