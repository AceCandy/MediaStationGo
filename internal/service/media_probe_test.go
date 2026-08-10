package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type stubMediaProbeRunner struct {
	result    *ProbeResult
	err       error
	onProbe   func()
	probeFunc func(string) (*ProbeResult, error)
}

func (s *stubMediaProbeRunner) Probe(_ context.Context, path string) (*ProbeResult, error) {
	if s.onProbe != nil {
		s.onProbe()
	}
	if s.probeFunc != nil {
		return s.probeFunc(path)
	}
	return s.result, s.err
}

func (s *stubMediaProbeRunner) ProbeHTTP(context.Context, string) (*ProbeResult, error) {
	return s.Probe(context.Background(), "")
}

func TestMediaProbePersistsScalarAndCompleteDocument(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: path}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	runner := &stubMediaProbeRunner{result: probeResultFixture()}
	svc := NewMediaProbeService(repos, runner)
	if _, err := svc.ProbeMedia(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := repos.Media.FindByID(t.Context(), media.ID)
	if got.VideoCodec != "hevc" || got.AudioCodec != "eac3" || got.Width != 3840 || got.DurationSec != 120 {
		t.Fatalf("scalar projection = %#v", got)
	}
	doc, ok := svc.Load(t.Context(), media.ID)
	if !ok || len(doc.Streams) != 2 || doc.Streams[1].Index != 3 {
		t.Fatalf("stored probe document = %#v, ok=%v", doc, ok)
	}
}

func TestMediaProbePersistsLocalSTRMTargetSize(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	target := filepath.Join(dir, "target.mkv")
	content := []byte("target media bytes")
	if err := os.WriteFile(target, content, 0o600); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(dir, "movie.strm")
	if err := os.WriteFile(strmPath, []byte(target+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		MetadataID: metadata.ID, LibraryID: "library", Title: "Movie",
		Path: strmPath, STRMURL: target, Container: "strm", SizeBytes: 1,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	runner := &stubMediaProbeRunner{probeFunc: func(path string) (*ProbeResult, error) {
		if path != target {
			t.Fatalf("probe path = %q, want %q", path, target)
		}
		return probeResultFixture(), nil
	}}
	if _, err := NewMediaProbeService(repos, runner).ProbeMedia(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := repos.Media.FindByID(t.Context(), media.ID)
	if got.SizeBytes != int64(len(content)) {
		t.Fatalf("size_bytes = %d, want target size %d", got.SizeBytes, len(content))
	}
}

func TestMediaProbePersistsRemoteSTRMTargetSize(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	media := model.Media{
		MetadataID: metadata.ID, LibraryID: "library", Title: "Movie",
		Path: "/virtual/movie.strm", STRMURL: "https://media.example.test/movie.mkv", Container: "strm", SizeBytes: 201,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	result := probeResultFixture()
	result.Document.Format.Size = 26_972_800_320
	if _, err := NewMediaProbeService(repos, &stubMediaProbeRunner{result: result}).ProbeMedia(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := repos.Media.FindByID(t.Context(), media.ID)
	if got.SizeBytes != result.Document.Format.Size {
		t.Fatalf("size_bytes = %d, want remote target size %d", got.SizeBytes, result.Document.Format.Size)
	}
}

func TestMediaProbeFallbackDoesNotOverwriteCompleteDocument(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: path}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	oldJSON, _ := MarshalProbeDocument(probeResultFixture().Document)
	row := model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: oldJSON, SchemaVersion: ProbeDocumentSchemaVersion}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaProbeService(repos, &stubMediaProbeRunner{result: &ProbeResult{DurationSec: 90, VideoCodec: "h264"}})
	if _, err := svc.ProbeMedia(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := repos.MediaProbe.FindByMediaID(t.Context(), media.ID)
	if after.ProbeJSON != oldJSON {
		t.Fatal("partial probe result overwrote complete document")
	}
}

func TestMediaProbeRejectsChangedLocalSource(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: path}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	runner := &stubMediaProbeRunner{result: probeResultFixture(), onProbe: func() {
		if err := os.WriteFile(path, []byte("changed source"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := NewMediaProbeService(repos, runner).ProbeMedia(t.Context(), media.ID)
	if !errors.Is(err, ErrMediaProbeSourceChanged) {
		t.Fatalf("error = %v, want source changed", err)
	}
	if row, _ := repos.MediaProbe.FindByMediaID(t.Context(), media.ID); row != nil {
		t.Fatal("stale probe result was persisted")
	}
}

func TestMediaProbeBackfillLibraryAccountsForResults(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	media := []model.Media{
		{MetadataID: metadata.ID, LibraryID: "target", Title: "Valid", Path: filepath.Join(dir, "valid.mkv")},
		{MetadataID: metadata.ID, LibraryID: "target", Title: "Missing", Path: filepath.Join(dir, "missing.mkv")},
		{MetadataID: metadata.ID, LibraryID: "target", Title: "Outdated", Path: filepath.Join(dir, "failure.mkv")},
		{MetadataID: metadata.ID, LibraryID: "target", Title: "Fallback", Path: filepath.Join(dir, "fallback.mkv")},
		{MetadataID: metadata.ID, LibraryID: "other", Title: "Other", Path: filepath.Join(dir, "other.mkv")},
	}
	for i := range media {
		if err := os.WriteFile(media[i].Path, []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&media[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	validJSON, err := MarshalProbeDocument(probeResultFixture().Document)
	if err != nil {
		t.Fatal(err)
	}
	rows := []model.MediaProbeMetadata{
		{MediaID: media[0].ID, ProbeJSON: validJSON, SchemaVersion: ProbeDocumentSchemaVersion},
		{MediaID: media[2].ID, ProbeJSON: "{}", SchemaVersion: 0},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var probed []string
	runner := &stubMediaProbeRunner{probeFunc: func(path string) (*ProbeResult, error) {
		probed = append(probed, filepath.Base(path))
		if filepath.Base(path) == "failure.mkv" {
			return nil, errors.New("probe failed")
		}
		if filepath.Base(path) == "fallback.mkv" {
			return &ProbeResult{DurationSec: 120, VideoCodec: "hevc"}, nil
		}
		return probeResultFixture(), nil
	}}
	var latest ProbeBackfillResult
	result, err := NewMediaProbeService(repos, runner).BackfillLibrary(t.Context(), "target", func(current ProbeBackfillResult) {
		latest = current
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 4 || result.Completed != 1 || result.Skipped != 1 || result.Failed != 2 {
		t.Fatalf("result = %#v", result)
	}
	if latest.Total != result.Total || latest.Completed != result.Completed || latest.Skipped != result.Skipped || latest.Failed != result.Failed {
		t.Fatalf("latest progress = %#v, want %#v", latest, result)
	}
	if len(probed) != 3 {
		t.Fatalf("probed paths = %#v", probed)
	}
	for _, path := range probed {
		if path == "other.mkv" {
			t.Fatalf("other library was probed: %#v", probed)
		}
	}
}

func TestMediaProbeBackfillLibraryUsesStablePagination(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	for i := 0; i < 101; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%03d.mkv", i))
		if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Media{MetadataID: metadata.ID, LibraryID: "target", Title: "Movie", Path: path}).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := NewMediaProbeService(repos, &stubMediaProbeRunner{result: probeResultFixture()}).BackfillLibrary(t.Context(), "target", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 101 || result.Completed != 101 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func probeResultFixture() *ProbeResult {
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160},
		{Index: 3, CodecType: "audio", CodecName: "eac3", Disposition: ProbeDisposition{Default: true}},
	}}
	return &ProbeResult{DurationSec: 120, Width: 3840, Height: 2160, VideoCodec: "hevc", AudioCodec: "eac3", Container: "matroska", Document: doc}
}
