package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestWatcherNFORefreshPreservesConfirmedIdentityAndProbe(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "movie.mkv")
	writeTestFile(t, path, "video")
	nfo := filepath.Join(root, "movie.nfo")
	writeTestMovieNFO(t, nfo, "Original", "2024", "42")
	if _, err := scanner.IngestPathResult(t.Context(), lib.ID, path); err != nil {
		t.Fatal(err)
	}
	var before model.Media
	if err := repos.DB.Where("path = ?", path).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "Confirmed", Source: "tmdb"})
	if err := repos.DB.Model(&before).Updates(map[string]any{"metadata_id": metadata.ID, "scrape_status": "matched"}).Error; err != nil {
		t.Fatal(err)
	}
	probe := model.MediaProbeMetadata{MediaID: before.ID, ProbeJSON: `{"retained":true}`, SchemaVersion: ProbeDocumentSchemaVersion}
	if err := repos.DB.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	writeTestMovieNFO(t, nfo, "Updated", "2025", "999")
	w := NewWatcherService(zap.NewNop(), repos, scanner, NewTaskTrackerService(zap.NewNop(), nil))
	w.processBatch(t.Context(), []duePath{{path: path, libraryID: lib.ID}, {path: nfo, libraryID: lib.ID}})
	var after model.Media
	if err := repos.DB.First(&after, "id = ?", before.ID).Error; err != nil {
		t.Fatal(err)
	}
	hint := serviceTestLocalMetadataHint(t, after)
	if hint.Title != "Updated" || after.MetadataID != metadata.ID || after.TMDbID != before.TMDbID || after.ScrapeStatus != "matched" || after.ScanFileMTimeNS != before.ScanFileMTimeNS || after.ScanFileSizeBytes != before.ScanFileSizeBytes {
		t.Fatalf("sidecar changed confirmed identity or failed to refresh: %+v", after)
	}
	var saved model.MediaProbeMetadata
	if err := repos.DB.First(&saved, "media_id = ?", before.ID).Error; err != nil || saved.ProbeJSON != probe.ProbeJSON {
		t.Fatalf("sidecar invalidated probe: %+v %v", saved, err)
	}
	if changed, err := scanner.refreshLocalMetadataHints(t.Context(), lib.ID, path); err != nil || changed {
		t.Fatalf("duplicate event rewrote hints: %v %v", changed, err)
	}
	if err := os.WriteFile(nfo, []byte("<movie>broken"), 0600); err != nil {
		t.Fatal(err)
	}
	w.processBatch(t.Context(), []duePath{{path: nfo, libraryID: lib.ID}})
	if retry, ok := w.pending[path]; !ok || !retry.metadata || retry.attempts != 1 {
		t.Fatalf("failed sidecar did not retain refresh intent: %+v", w.pending)
	}
	if err := repos.DB.First(&after, "id = ?", before.ID).Error; err != nil || serviceTestLocalMetadataHint(t, after).Title != "Updated" {
		t.Fatal("invalid sidecar overwrote accepted hints")
	}
}

func TestWatcherNFORefreshRetriesUnmatchedAndIsolatesSources(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	for _, typ := range []string{"movie", model.LibraryTypeHongGuo, model.LibraryTypeNFOMovie} {
		root := t.TempDir()
		lib := model.Library{Name: typ, Path: root, Type: typ, Enabled: true}
		if err := repos.Library.Create(t.Context(), &lib); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "movie.mkv")
		writeTestFile(t, path, "video")
		writeTestMovieNFO(t, filepath.Join(root, "movie.nfo"), "Corrected", "2024", "123")
		source := ""
		if typ == model.LibraryTypeHongGuo {
			source = "hongguo"
		} else if typ == model.LibraryTypeNFOMovie {
			source = "nfo"
		}
		media := model.Media{LibraryID: lib.ID, Path: path, CatalogSource: source, ScrapeStatus: "error"}
		if err := repos.DB.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		changed, err := scanner.refreshLocalMetadataHints(t.Context(), lib.ID, path)
		if err != nil || changed != (typ == "movie") {
			t.Fatalf("type=%s changed=%v err=%v", typ, changed, err)
		}
		var got model.Media
		if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
			t.Fatal(err)
		}
		if typ == "movie" {
			if got.TMDbID != 123 || got.ScrapeStatus != "pending" {
				t.Fatalf("corrected hints not retried: %+v", got)
			}
		} else if got.LocalMetadataHint != "" || got.ScrapeStatus != "error" {
			t.Fatalf("ordinary refresh crossed catalog boundary: %+v", got)
		}
	}
}

func TestWatcherNFORefreshWaitsForRunningScrape(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "S01E02.mkv")
	writeTestFile(t, path, "video")
	nfo := filepath.Join(root, "S01E02.nfo")
	writeTestFile(t, nfo, `<episodedetails><title>Corrected</title><season>2</season><episode>3</episode><uniqueid type="tmdb">123</uniqueid></episodedetails>`)
	media := model.Media{LibraryID: lib.ID, Path: path, ScrapeStatus: "running", TMDbID: 42, SeasonNum: 1, EpisodeNum: 2}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if changed, err := scanner.refreshLocalMetadataHints(t.Context(), lib.ID, path); err == nil || changed {
		t.Fatalf("running scrape accepted changed hints: %v %v", changed, err)
	}
	if err := repos.DB.Model(&media).Update("scrape_status", "no_match").Error; err != nil {
		t.Fatal(err)
	}
	if changed, err := scanner.refreshLocalMetadataHints(t.Context(), lib.ID, path); err != nil || !changed {
		t.Fatalf("settled scrape did not refresh: %v %v", changed, err)
	}
	if err := os.Remove(nfo); err != nil {
		t.Fatal(err)
	}
	if changed, err := scanner.refreshLocalMetadataHints(t.Context(), lib.ID, path); err != nil || !changed {
		t.Fatalf("deleted NFO did not refresh: %v %v", changed, err)
	}
	var stored model.Media
	if err := repos.DB.First(&stored, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TMDbID != 0 || stored.SeasonNum != 1 || stored.EpisodeNum != 2 || stored.ScrapeStatus != "pending" {
		t.Fatalf("deleted NFO retained stale identity hints: %+v", stored)
	}
}
