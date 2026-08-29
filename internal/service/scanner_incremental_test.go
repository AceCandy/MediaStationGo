package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newScannerTestEnv(t *testing.T) (*ScannerService, *repository.Container) {
	t.Helper()
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{})
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	sc := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	return sc, repos
}

func countMedia(t *testing.T, repos *repository.Container) int64 {
	t.Helper()
	var n int64
	if err := repos.DB.Model(&model.Media{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIngestPathAddsSingleFile(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Some Movie (2021).mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := sc.IngestPath(t.Context(), lib.ID, file)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !added {
		t.Fatal("expected file to be added")
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1", got)
	}
	var before model.Media
	if err := repos.DB.First(&before, "path = ?", file).Error; err != nil {
		t.Fatal(err)
	}
	if updated, err := sc.IngestPath(t.Context(), lib.ID, file); err != nil || updated {
		t.Fatalf("unchanged ingest = updated=%v err=%v, want skipped", updated, err)
	}
	var unchanged model.Media
	if err := repos.DB.First(&unchanged, "path = ?", file).Error; err != nil {
		t.Fatal(err)
	}
	if !unchanged.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unchanged ingest touched updated_at: before=%s after=%s", before.UpdatedAt, unchanged.UpdatedAt)
	}
	// Non-video file is ignored.
	other := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if added, _ := sc.IngestPath(t.Context(), lib.ID, other); added {
		t.Fatal("non-video file should not be ingested")
	}
}

func TestScanLibraryImportsISOImage(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(root, "Movie.Name.2026.iso")
	if err := os.WriteFile(isoPath, []byte("iso image"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 1 || res.ErrorCount != 0 {
		t.Fatalf("scan result = %#v, want one ISO image added without errors", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.Path != isoPath {
		t.Fatalf("ISO media = %#v, want path %q", media, isoPath)
	}
}

func TestScanLibraryUsesISOParentFolderForScrapeIdentity(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	movieDir := filepath.Join(root, "Dune.Part.Two.2024")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(movieDir, "BDMV.iso")
	if err := os.WriteFile(isoPath, []byte("iso image"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.Title != "dune part two" || media.Year != 2024 || media.ScrapeStatus != "pending" {
		t.Fatalf("ISO scrape identity = title=%q year=%d status=%q", media.Title, media.Year, media.ScrapeStatus)
	}
	candidates := scrapeQueryCandidates(&media, &lib)
	if len(candidates) == 0 || candidates[0] != "dune part two" {
		t.Fatalf("ISO scrape candidates = %#v", candidates)
	}
}

func TestScanLibraryRepairsPreviouslyUnmatchedGenericISO(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	movieDir := filepath.Join(root, "Dune.Part.Two.2024")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(movieDir, "BDMV.iso")
	if err := os.WriteFile(isoPath, []byte("iso image"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := model.Media{
		LibraryID:    lib.ID,
		Title:        "bdmv",
		Path:         isoPath,
		Container:    "iso",
		SizeBytes:    int64(len("iso image")),
		ScrapeStatus: "no_match",
	}
	if err := repos.DB.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 1 || res.Skipped != 0 {
		t.Fatalf("rescan result = %#v, want legacy ISO refreshed", res)
	}
	if len(res.Changes) != 1 || !strings.Contains(res.Changes[0].Reason, "首次补录文件指纹") {
		t.Fatalf("legacy ISO changes = %#v, want fingerprint backfill reason", res.Changes)
	}
	var media model.Media
	if err := repos.DB.First(&media, "id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if media.Title != "dune part two" || media.Year != 2024 || media.ScrapeStatus != "pending" {
		t.Fatalf("repaired ISO = title=%q year=%d status=%q", media.Title, media.Year, media.ScrapeStatus)
	}
	if media.ScanFileSizeBytes == 0 || media.ScanFileMTimeNS == 0 {
		t.Fatalf("legacy ISO fingerprint not backfilled: %#v", media)
	}
}

func TestScanLibraryReturnsNotFoundForMissingLibrary(t *testing.T) {
	sc, _ := newScannerTestEnv(t)

	res, err := sc.ScanLibrary(t.Context(), "missing-library")
	if err == nil {
		t.Fatalf("ScanLibrary() error = nil, result = %#v", res)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) && err.Error() != "library not found" {
		t.Fatalf("ScanLibrary() error = %v, want not found", err)
	}
}

func TestScanLibraryInvalidatesProbeDocumentWhenFileChanges(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	library := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanLibrary(t.Context(), library.ID); err != nil {
		t.Fatal(err)
	}
	media, err := repos.Media.FindByPath(t.Context(), path)
	if err != nil || media == nil {
		t.Fatalf("media = %#v, err = %v", media, err)
	}
	probeJSON, err := MarshalProbeDocument(probeResultFixture().Document)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.MediaProbe.Upsert(t.Context(), &model.MediaProbeMetadata{
		MediaID: media.ID, ProbeJSON: probeJSON, SchemaVersion: ProbeDocumentSchemaVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed-size"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanLibrary(t.Context(), library.ID); err != nil {
		t.Fatal(err)
	}
	if probe, err := repos.MediaProbe.FindByMediaID(t.Context(), media.ID); err != nil || probe != nil {
		t.Fatalf("probe after changed scan = %#v, err = %v, want nil", probe, err)
	}
}

func TestIngestPathReturnsNotFoundForMissingLibrary(t *testing.T) {
	sc, _ := newScannerTestEnv(t)

	added, err := sc.IngestPath(t.Context(), "missing-library", filepath.Join(t.TempDir(), "movie.mkv"))
	if err == nil {
		t.Fatalf("IngestPath() error = nil, added = %v", added)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) && err.Error() != "library not found" {
		t.Fatalf("IngestPath() error = %v, want not found", err)
	}
}

func TestScanLibraryReadsLocalSTRMTarget(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "STRM", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(root, "Remote Movie.strm")
	if err := os.WriteFile(strmPath, []byte("https://cdn.example.com/movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("scan result = %#v, want added=1", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.STRMURL != "https://cdn.example.com/movie.mkv" {
		t.Fatalf("strm media not parsed: %#v", media)
	}
}

func TestScanLibraryReadsLocalSTRMFileTarget(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "STRM", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "Local Movie.mkv")
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(root, "Local Movie.strm")
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatalf("scan: %v", err)
	}
	var media model.Media
	if err := repos.DB.Where("path = ?", strmPath).First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.STRMURL != target {
		t.Fatalf("strm target = %q, want %q", media.STRMURL, target)
	}
}

func TestReadLocalSTRMTargetRejectsNonMediaLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-media.strm")
	if err := os.WriteFile(path, []byte(filepath.Join(t.TempDir(), "secret.txt")), 0o644); err != nil {
		t.Fatal(err)
	}
	if target, err := readLocalSTRMTarget(path); err != nil || target != "" {
		t.Fatalf("target/error = %q/%v, want empty target", target, err)
	}
}

func TestScanLibraryReadsTMDbHintFromMovieParent(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	movieDir := filepath.Join(root, "白雪公主和七个小矮人 (1938) [tmdbid=408]")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(movieDir, "白雪公主和七个小矮人 (1938).strm")
	if err := os.WriteFile(strmPath, []byte("https://cdn.example.com/movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 1 || res.ErrorCount != 0 {
		t.Fatalf("scan result = %#v, want one media added without errors", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.TMDbID != 408 {
		t.Fatalf("media TMDbID = %d, want 408", media.TMDbID)
	}
	if media.MetadataID != "" || media.ScrapeStatus != "pending" {
		t.Fatalf("media metadata/status = %q/%q, want unresolved pending media", media.MetadataID, media.ScrapeStatus)
	}
	metadata, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", model.MetadataKindMovie, "408")
	if err != nil {
		t.Fatal(err)
	}
	if metadata != nil {
		t.Fatalf("metadata = %#v, want no local row before provider scrape", metadata)
	}
	view, err := repos.MediaView.FindByID(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view != nil {
		t.Fatalf("unresolved media view = %#v, want hidden until metadata is bound", view)
	}
}

func TestScanLibraryReusesTMDbMetadataFromMovieParent(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Provider title", Overview: "Provider overview", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), &metadata, []model.MetadataIdentifier{{
		Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "408",
	}}); err != nil {
		t.Fatal(err)
	}
	movieDir := filepath.Join(root, "白雪公主和七个小矮人 (1938) [tmdbid=408]")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.strm"), []byte("https://cdn.example.com/movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.MetadataID != metadata.ID || media.ScrapeStatus != "matched" {
		t.Fatalf("media metadata/status = %q/%q, want %q/matched", media.MetadataID, media.ScrapeStatus, metadata.ID)
	}
	saved, err := repos.Metadata.FindByID(t.Context(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.Title != metadata.Title || saved.Overview != metadata.Overview || saved.Source != metadata.Source {
		t.Fatalf("canonical metadata was overwritten: %#v", saved)
	}
	scraper := NewScraperService(&config.Config{}, zap.NewNop(), repos, nil, nil, nil, nil, NewHub(zap.NewNop()))
	candidates, err := scraper.scrapeCandidateRows(t.Context(), lib.ID, ScrapeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("matched cache hit produced %d scrape candidates", len(candidates))
	}
}

func TestScanLibrarySkipsUnchangedExistingLocalMedia(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Already In Library (2024).mkv")
	if err := os.WriteFile(file, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 || first.Skipped != 0 {
		t.Fatalf("first scan = %#v, want added=1 skipped=0", first)
	}
	second, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Added != 0 || second.Updated != 0 || second.Skipped != 1 {
		t.Fatalf("second scan = %#v, want unchanged file skipped", second)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1", got)
	}
}

func TestScanLibrarySkipsUnchangedSTRMByFileFingerprint(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "STRM", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "Remote Movie.strm")
	if err := os.WriteFile(path, []byte("https://cdn.example.com/movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 {
		t.Fatalf("first scan = %#v, want added=1", first)
	}
	if len(first.Changes) != 1 || first.Changes[0].Action != ScanChangeAdded || first.Changes[0].Path != path {
		t.Fatalf("first scan changes = %#v, want added path %q", first.Changes, path)
	}
	var before model.Media
	if err := repos.DB.First(&before, "path = ?", path).Error; err != nil {
		t.Fatal(err)
	}
	if before.ScanFileSizeBytes == 0 || before.ScanFileMTimeNS == 0 {
		t.Fatalf("scan fingerprint not persisted: %#v", before)
	}

	second, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Updated != 0 || second.Skipped != 1 {
		t.Fatalf("second scan = %#v, want unchanged STRM skipped", second)
	}
	if len(second.Changes) != 0 {
		t.Fatalf("second scan changes = %#v, want none", second.Changes)
	}
	var unchanged model.Media
	if err := repos.DB.First(&unchanged, "path = ?", path).Error; err != nil {
		t.Fatal(err)
	}
	if !unchanged.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unchanged STRM touched updated_at: before=%s after=%s", before.UpdatedAt, unchanged.UpdatedAt)
	}

	changedAt := time.Unix(0, before.ScanFileMTimeNS).Add(time.Second)
	if err := os.Chtimes(path, changedAt, changedAt); err != nil {
		t.Fatal(err)
	}
	third, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if third.Updated != 1 || third.Skipped != 0 {
		t.Fatalf("third scan = %#v, want changed mtime updated", third)
	}
	if len(third.Changes) != 1 || third.Changes[0].Action != ScanChangeUpdated || third.Changes[0].Path != path || !strings.Contains(third.Changes[0].Reason, "mtime_ns 变化") {
		t.Fatalf("third scan changes = %#v, want mtime update for %q", third.Changes, path)
	}

	if err := os.WriteFile(path, []byte("https://cdn.example.com/movie-updated.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sizeChangedAt := changedAt.Add(time.Second)
	if err := os.Chtimes(path, sizeChangedAt, sizeChangedAt); err != nil {
		t.Fatal(err)
	}
	fourth, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("fourth scan: %v", err)
	}
	if len(fourth.Changes) != 1 || !strings.Contains(fourth.Changes[0].Reason, "文件大小变化") || !strings.Contains(fourth.Changes[0].Reason, "mtime_ns 变化") {
		t.Fatalf("fourth scan changes = %#v, want size and mtime reasons", fourth.Changes)
	}
}

func TestScanLibraryUpdatesExistingPathFromOverlappingLibrary(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	file := filepath.Join(root, "Shared Movie (2026).mkv")
	if err := os.WriteFile(file, []byte("same-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstLib := model.Library{Name: "Movies A", Path: root, Type: "movie", Enabled: true}
	secondLib := model.Library{Name: "Movies B", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &firstLib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &secondLib); err != nil {
		t.Fatal(err)
	}

	first, err := sc.ScanLibrary(t.Context(), firstLib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 {
		t.Fatalf("first scan = %#v, want added=1", first)
	}
	second, err := sc.ScanLibrary(t.Context(), secondLib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Added != 0 || second.Updated != 1 || second.ErrorCount != 0 {
		t.Fatalf("second scan = %#v, want existing global path updated without duplicate insert errors", second)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want one row for overlapping libraries", got)
	}
}

func TestScanLibrarySkipsUnchangedLocalMetadata(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Local Metadata (2024).mkv")
	if err := os.WriteFile(file, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(root, "Local Metadata (2024).nfo")
	writeTestMovieNFO(t, nfo, "Local Metadata", "2024", "12345")

	first, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 || first.LocalMetadata != 1 {
		t.Fatalf("first scan = %#v, want added=1 local_metadata=1", first)
	}
	second, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Added != 0 || second.Updated != 0 || second.Skipped != 1 {
		t.Fatalf("second scan = %#v, want unchanged local metadata skipped", second)
	}

	writeTestMovieNFO(t, nfo, "Local Metadata Updated", "2024", "12345")
	third, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if third.Added != 0 || third.Updated != 0 || third.Skipped != 1 {
		t.Fatalf("third scan = %#v, want unchanged media fingerprint skipped", third)
	}
	if len(third.Changes) != 0 {
		t.Fatalf("third scan changes = %#v, want none", third.Changes)
	}
	var media model.Media
	if err := repos.DB.First(&media, "path = ?", file).Error; err != nil {
		t.Fatal(err)
	}
	local := serviceTestLocalMetadataHint(t, media)
	if local.Title != "Local Metadata" || local.TMDbID != 12345 || media.ScrapeStatus != "pending" || media.MetadataID != "" {
		t.Fatalf("local metadata hint changed despite unchanged media fingerprint: media=%+v hint=%+v", media, local)
	}
}

func writeTestMovieNFO(t *testing.T, path, title, year, tmdbID string) {
	t.Helper()
	body := `<movie><title>` + title + `</title><year>` + year + `</year><uniqueid type="tmdb">` + tmdbID + `</uniqueid></movie>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanLibrarySkipsUnchangedExistingLocalMediaWithMissingTrackMetadata(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Already In Library Missing Tracks (2024).mkv")
	if err := os.WriteFile(file, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 {
		t.Fatalf("first scan = %#v, want added=1", first)
	}

	second, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Added != 0 || second.Updated != 0 || second.Skipped != 1 || second.Probed != 0 {
		t.Fatalf("second scan = %#v, want unchanged file skipped without synchronous track probe", second)
	}
}

func TestScanLibraryImportsNewLocalMediaWithoutSynchronousProbe(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "New Movie (2026).mkv")
	if err := os.WriteFile(file, []byte("new-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 1 || res.Probed != 0 {
		t.Fatalf("scan result = %#v, want fast import without synchronous ffprobe", res)
	}
}

func TestScanLibraryReportsPerFileUpsertErrors(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Broken DB", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Cannot Insert (2024).mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Exec("DROP TABLE media").Error; err != nil {
		t.Fatal(err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan should continue and report file errors, got top-level error: %v", err)
	}
	if res.ErrorCount != 1 || len(res.Errors) != 1 {
		t.Fatalf("scan errors = count %d details %#v, want one detailed error", res.ErrorCount, res.Errors)
	}
	if res.Visited != 1 {
		t.Fatalf("visited = %d, want 1", res.Visited)
	}
}

func TestScanLibraryMapsPersistedHostLibraryPath(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(containerLibrary, "狂飙.S01E01.2023.mkv")
	if err := os.WriteFile(file, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `Q:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	lib := model.Library{Name: "国产剧", Path: `Q:\media\电视剧\国产剧`, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan mapped host path: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("scan result = %#v, want added=1", res)
	}
	var stored model.Library
	if err := repos.DB.First(&stored, "id = ?", lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Path != filepath.Clean(containerLibrary) {
		t.Fatalf("stored path = %q, want %q", stored.Path, filepath.Clean(containerLibrary))
	}
}

func TestRemovePathDeletesVanishedMedia(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	cache := NewRuntimeCacheService(&config.Config{}, zap.NewNop())
	sc.SetRuntimeCache(cache)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Gone (2020).mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.IngestPath(t.Context(), lib.ID, file); err != nil {
		t.Fatal(err)
	}
	if countMedia(t, repos) != 1 {
		t.Fatal("expected 1 media before removal")
	}
	var unresolved int64
	if err := repos.DB.Model(&model.Media{}).Where("metadata_id IS NULL").Count(&unresolved).Error; err != nil {
		t.Fatal(err)
	}
	if unresolved != 1 {
		t.Fatalf("unresolved media = %d, want 1", unresolved)
	}
	if err := repos.DB.Model(&model.Media{}).Where("path = ?", file).Update("scrape_status", "error").Error; err != nil {
		t.Fatal(err)
	}
	mediaService := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	issues, err := mediaService.ListScrapeIssues(t.Context(), lib.ID, nil, 1, 10)
	if err != nil || issues.Total != 1 {
		t.Fatalf("scrape issues before removal = %#v, err = %v", issues, err)
	}
	cache.SetJSON(t.Context(), "media:list:test", map[string]string{"state": "stale"}, time.Minute)
	var cached map[string]string
	if !cache.GetJSON(t.Context(), "media:list:test", &cached) {
		t.Fatal("expected media cache to be primed")
	}
	// A still-present file is not removed.
	if removed, _ := sc.RemovePath(t.Context(), file); removed != 0 {
		t.Fatalf("present file should not be removed, got %d", removed)
	}
	if !cache.GetJSON(t.Context(), "media:list:test", &cached) {
		t.Fatal("present file should not invalidate media cache")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	removed, err := sc.RemovePath(t.Context(), file)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if countMedia(t, repos) != 0 {
		t.Fatal("expected 0 media after removal")
	}
	issues, err = mediaService.ListScrapeIssues(t.Context(), lib.ID, nil, 1, 10)
	if err != nil || issues.Total != 0 {
		t.Fatalf("scrape issues after removal = %#v, err = %v", issues, err)
	}
	if cache.GetJSON(t.Context(), "media:list:test", &cached) {
		t.Fatal("vanished media removal should invalidate media cache")
	}
}

// TestScanSkipsHardlinkDuplicate verifies that a hardlink (same inode) kept
// for seeding is not imported as a second media item, preventing duplicate
// recognition and double-counted storage.
func TestScanSkipsHardlinkDuplicate(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(root, "Movie (2019).mkv")
	if err := os.WriteFile(orig, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "Movie (2019) [organized].mkv")
	if err := os.Link(orig, linked); err != nil {
		t.Skipf("hardlinks unsupported on this fs: %v", err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1 (hardlink should be deduped)", got)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", res.Skipped)
	}
}
