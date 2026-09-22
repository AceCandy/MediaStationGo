package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

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

func TestMediaProbePersistsSummaryAndCompleteDocument(t *testing.T) {
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
	got, _ := repos.MediaProbe.FindByMediaID(t.Context(), media.ID)
	if got.VideoCodec != "hevc" || got.AudioCodec != "eac3" || got.Width != 3840 || got.DurationMS != 120_000 || got.BitRate != 8_000_000 {
		t.Fatalf("probe summary = %#v", got)
	}
	legacy, _ := repos.Media.FindByID(t.Context(), media.ID)
	if legacy.DurationSec != 0 || legacy.VideoCodec != "" {
		t.Fatalf("legacy media summary was updated: %#v", legacy)
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
	got, _ := repos.MediaProbe.FindByMediaID(t.Context(), media.ID)
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
	startedAt := time.Now()
	if _, err := NewMediaProbeService(repos, &stubMediaProbeRunner{result: result}).ProbeMedia(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed < 2*time.Second {
		t.Fatalf("remote probe delay = %v, want at least 2s", elapsed)
	}
	got, _ := repos.MediaProbe.FindByMediaID(t.Context(), media.ID)
	if got.SizeBytes != result.Document.Format.Size {
		t.Fatalf("size_bytes = %d, want remote target size %d", got.SizeBytes, result.Document.Format.Size)
	}
}

func TestMediaProbeBackfillsSummaryFromValidDocumentOnly(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	media := []model.Media{
		{MetadataID: metadata.ID, LibraryID: "library", Title: "Valid", Path: "/valid.mkv", DurationSec: 999},
		{MetadataID: metadata.ID, LibraryID: "library", Title: "Invalid", Path: "/invalid.mkv", DurationSec: 777},
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	probeJSON, err := MarshalProbeDocument(probeResultFixture().Document)
	if err != nil {
		t.Fatal(err)
	}
	rows := []model.MediaProbeMetadata{
		{MediaID: media[0].ID, ProbeJSON: probeJSON, SchemaVersion: ProbeDocumentSchemaVersion},
		{MediaID: media[1].ID, ProbeJSON: "{}", SchemaVersion: ProbeDocumentSchemaVersion},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaProbeService(repos, nil)
	if err := svc.BackfillSummaries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.BackfillSummaries(t.Context()); err != nil {
		t.Fatal(err)
	}
	valid, _ := repos.MediaProbe.FindByMediaID(t.Context(), media[0].ID)
	invalid, _ := repos.MediaProbe.FindByMediaID(t.Context(), media[1].ID)
	if valid.SummaryVersion != ProbeSummaryVersion || valid.DurationMS != 120_000 {
		t.Fatalf("valid summary = %#v", valid)
	}
	if invalid.SummaryVersion != 0 || invalid.DurationMS != 0 {
		t.Fatalf("invalid summary inherited legacy media values: %#v", invalid)
	}
}

func TestRemoteMediaProbeDelayRange(t *testing.T) {
	for range 100 {
		delay := remoteMediaProbeDelay()
		if delay < 2*time.Second || delay > 5*time.Second || delay%time.Second != 0 {
			t.Fatalf("remote probe delay = %v, want an integer 2-5s", delay)
		}
	}
}

func TestMapRemoteProbePath(t *testing.T) {
	root := t.TempDir()
	general := filepath.Join(root, "general")
	specific := filepath.Join(root, "specific")
	mappings := strings.Join([]string{
		"https://media.example.test/archive/ => " + general,
		"https://media.example.test/archive/4k/ => " + specific,
	}, "\n")

	tests := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{
			name:     "longest prefix decodes path and ignores query",
			rawURL:   "https://media.example.test/archive/4k/%E7%94%B5%E5%BD%B1.mkv?token=ignored",
			expected: filepath.Join(specific, "电影.mkv"),
		},
		{name: "path boundary", rawURL: "https://media.example.test/archive-extra/movie.mkv"},
		{name: "scheme mismatch", rawURL: "http://media.example.test/archive/movie.mkv"},
		{name: "host mismatch", rawURL: "https://other.example.test/archive/movie.mkv"},
		{name: "encoded traversal", rawURL: "https://media.example.test/archive/%2e%2e/secret.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapRemoteProbePath(mappings, tt.rawURL); got != tt.expected {
				t.Fatalf("mapped path = %q, want %q", got, tt.expected)
			}
		})
	}
	if got := mapRemoteProbePath("http:///archive/ => "+general, "http:///archive/movie.mkv"); got != "" {
		t.Fatalf("hostless URL mapped to %q", got)
	}
	if got := mapRemoteProbePath("https://user@media.example.test/archive/ => "+general, "https://media.example.test/archive/movie.mkv"); got != "" {
		t.Fatalf("URL prefix with userinfo mapped to %q", got)
	}
}

func TestMediaProbeResolvesMappedRemoteSTRMSource(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{}, &model.Setting{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), FFprobePathMappingsSettingKey, "https://media.example.test/archive/ => "+root); err != nil {
		t.Fatal(err)
	}
	media := &model.Media{
		MetadataID: metadata.ID, LibraryID: "library", Title: "Movie",
		Path: "/virtual/movie.strm", STRMURL: "https://media.example.test/archive/movie.mkv?token=ignored", Container: "strm",
	}
	if err := db.Create(media).Error; err != nil {
		t.Fatal(err)
	}
	runner := &stubMediaProbeRunner{probeFunc: func(path string) (*ProbeResult, error) {
		if path != target {
			t.Fatalf("probe path = %q, want %q", path, target)
		}
		return probeResultFixture(), nil
	}}
	svc := NewMediaProbeService(repos, runner)
	source, err := svc.resolveSource(t.Context(), media)
	if err != nil {
		t.Fatal(err)
	}
	if !source.local || source.path != target || source.url != "" {
		t.Fatalf("mapped source = %#v, want local path %q", source, target)
	}
	if _, err := svc.ProbeMedia(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}

	otherRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherRoot, "movie.mkv"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), FFprobePathMappingsSettingKey, "https://media.example.test/archive/ => "+otherRoot); err != nil {
		t.Fatal(err)
	}
	identity, err := currentSourceIdentity(media, svc.probePathMappings(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if identity == source.identity {
		t.Fatal("mapping change did not change probe source identity")
	}
}

func TestMediaProbePathMappingFallsBackToRemote(t *testing.T) {
	db := newServiceTestDB(t, &model.Setting{})
	repos := repository.New(db)
	root := t.TempDir()
	if err := repos.Setting.Set(t.Context(), FFprobePathMappingsSettingKey, "https://media.example.test/archive/ => "+root); err != nil {
		t.Fatal(err)
	}
	svc := NewMediaProbeService(repos, &stubMediaProbeRunner{})
	tests := []struct {
		name  string
		media model.Media
	}{
		{
			name:  "mapped file missing",
			media: model.Media{Path: "/virtual/movie.strm", STRMURL: "https://media.example.test/archive/missing.mkv", Container: "strm"},
		},
		{
			name:  "mapping does not match",
			media: model.Media{Path: "/virtual/movie.strm", STRMURL: "https://other.example.test/movie.mkv", Container: "strm"},
		},
		{
			name:  "non strm media",
			media: model.Media{Path: "/virtual/movie.mkv", STRMURL: "https://media.example.test/archive/movie.mkv", Container: "mkv"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := svc.resolveSource(t.Context(), &tt.media)
			if err != nil {
				t.Fatal(err)
			}
			if source.local || source.url != tt.media.STRMURL {
				t.Fatalf("source = %#v, want remote URL %q", source, tt.media.STRMURL)
			}
		})
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
			return nil, fmt.Errorf("probe failed for %s", path)
		}
		if filepath.Base(path) == "fallback.mkv" {
			return &ProbeResult{DurationSec: 120, VideoCodec: "hevc"}, nil
		}
		return probeResultFixture(), nil
	}}
	var latest ProbeBackfillResult
	var details []string
	result, err := NewMediaProbeService(repos, runner).BackfillLibrary(t.Context(), "target", 0, func(current ProbeBackfillResult) {
		latest = current
		details = append(details, current.Details...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || result.Completed != 1 || result.Skipped != 0 || result.Failed != 2 {
		t.Fatalf("result = %#v", result)
	}
	if latest.Total != result.Total || latest.Completed != result.Completed || latest.Skipped != result.Skipped || latest.Failed != result.Failed {
		t.Fatalf("latest progress = %#v, want %#v", latest, result)
	}
	if len(probed) != 3 {
		t.Fatalf("probed paths = %#v", probed)
	}
	if len(details) != 3 {
		t.Fatalf("details = %#v, want one per probe", details)
	}
	joinedDetails := strings.Join(details, "\n")
	if !strings.Contains(joinedDetails, fmt.Sprintf("✅️ %s %s", media[1].ID, media[1].Path)) {
		t.Fatalf("success detail missing from %q", joinedDetails)
	}
	if !strings.Contains(joinedDetails, fmt.Sprintf("❌️ %s %s probe failed for [redacted-path]", media[2].ID, media[2].Path)) {
		t.Fatalf("failure detail missing from %q", joinedDetails)
	}
	if strings.Contains(joinedDetails, media[4].Path) {
		t.Fatalf("failure detail contains unrelated path: %q", joinedDetails)
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
	result, err := NewMediaProbeService(repos, &stubMediaProbeRunner{result: probeResultFixture()}).BackfillLibrary(t.Context(), "target", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 101 || result.Completed != 101 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestMediaProbeBackfillAllCoversLibrariesAndSkipsValidDocuments(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	media := []model.Media{
		{MetadataID: metadata.ID, LibraryID: "first", Title: "Valid", Path: filepath.Join(dir, "valid.mkv")},
		{MetadataID: metadata.ID, LibraryID: "second", Title: "Missing", Path: filepath.Join(dir, "missing.mkv")},
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
	if err := db.Create(&model.MediaProbeMetadata{MediaID: media[0].ID, ProbeJSON: validJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}
	deleted := model.Media{MetadataID: metadata.ID, LibraryID: "first", Title: "Deleted", Path: filepath.Join(dir, "deleted.mkv")}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}

	result, err := NewMediaProbeService(repos, &stubMediaProbeRunner{result: probeResultFixture()}).BackfillAll(t.Context(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Completed != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestMediaProbeBackfillAllReturnsImmediatelyWhenNothingPending(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	media := model.Media{MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: filepath.Join(t.TempDir(), "movie.mkv")}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	validJSON, err := MarshalProbeDocument(probeResultFixture().Document)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: validJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}
	probed := 0
	result, err := NewMediaProbeService(repos, &stubMediaProbeRunner{onProbe: func() { probed++ }}).BackfillAll(t.Context(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || result.Completed != 0 || result.Skipped != 0 || result.Failed != 0 || len(result.Details) != 0 || probed != 0 {
		t.Fatalf("result = %#v, probe calls = %d", result, probed)
	}
}

func TestMediaProbeBackfillAllHonorsLimit(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	dir := t.TempDir()
	media := make([]model.Media, 3)
	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("limited-%d.mkv", i))
		if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
		media[i] = model.Media{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)}, MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: path}
		if err := db.Create(&media[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	validJSON, err := MarshalProbeDocument(probeResultFixture().Document)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MediaProbeMetadata{MediaID: media[0].ID, ProbeJSON: validJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := NewMediaProbeService(repos, &stubMediaProbeRunner{result: probeResultFixture()}).BackfillAll(t.Context(), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Completed != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestMediaProbeBackfillSkipsUnavailableSTRMWithoutConsumingLimit(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	dir := t.TempDir()
	strmPath := filepath.Join(dir, "unsupported.strm")
	mediaPath := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(strmPath, []byte("unsupported target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000001"}, LibraryID: "library", Title: "Unsupported", Path: strmPath},
		{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000002"}, LibraryID: "library", Title: "Movie", Path: mediaPath},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	probed := 0
	runner := &stubMediaProbeRunner{result: probeResultFixture(), onProbe: func() { probed++ }}
	result, err := NewMediaProbeService(repos, runner).BackfillAll(t.Context(), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Completed != 1 || result.Skipped != 1 || result.Failed != 0 || probed != 1 {
		t.Fatalf("result = %#v, probe calls = %d", result, probed)
	}
}

func TestMediaProbeAutomaticBackfillCreatesVisibleEventTaskAndCoalescesWake(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{}, &model.Library{})
	repos := repository.New(db)
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: "library", Title: "Movie", Path: path}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &stubMediaProbeRunner{result: probeResultFixture(), onProbe: func() {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	}}
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	probe := NewMediaProbeService(repos, runner).SetTaskTracker(zap.NewNop(), tracker, t.Context())

	probe.WakeBackfill()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("automatic probe backfill did not start")
	}
	probe.WakeBackfill()
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := tracker.Snapshot()
		if len(snapshot.Recent) == 1 && len(snapshot.Active) == 0 {
			task := snapshot.Recent[0]
			if task.Kind != TaskKindProbe || task.Trigger != TaskTriggerEvent || task.Status != TaskStatusCompleted {
				t.Fatalf("automatic probe task = %#v", task)
			}
			if task.Metrics["completed"] != 1 || task.Metrics["failed"] != 0 {
				t.Fatalf("automatic probe metrics = %#v", task.Metrics)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("automatic probe tasks did not settle: %#v", tracker.Snapshot())
}

func TestMediaProbeAutomaticBackfillSkipsEpisodesButManualIncludesThem(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{}, &model.Library{})
	dir := t.TempDir()
	for _, libraryType := range []string{"movie", "tv", "anime", "variety", "show", "shows", model.LibraryTypeNFOTV} {
		library := model.Library{Name: libraryType, Type: libraryType, Path: dir}
		if err := db.Create(&library).Error; err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, libraryType+".mkv")
		if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Media{LibraryID: library.ID, Path: path}).Error; err != nil {
			t.Fatal(err)
		}
	}
	probe := NewMediaProbeService(repository.New(db), &stubMediaProbeRunner{result: probeResultFixture()})
	result, err := probe.backfill(t.Context(), "", 0, nil, true)
	if err != nil || result.Total != 1 || result.Completed != 1 {
		t.Fatalf("automatic result = %#v, error = %v", result, err)
	}
	pending, err := probe.hasPendingProbe(t.Context())
	if err != nil || pending {
		t.Fatalf("episodes must not wake automatic backfill: pending=%v, error=%v", pending, err)
	}
	result, err = probe.BackfillAll(t.Context(), 0, nil)
	if err != nil || result.Total != 6 || result.Completed != 6 {
		t.Fatalf("manual result = %#v, error = %v", result, err)
	}
}

func TestScanResultBoundsChangeDetails(t *testing.T) {
	result := &ScanResult{}
	for i := 0; i < maxScanChangeDetails+3; i++ {
		result.addChange(ScanChangeAdded, fmt.Sprintf("/media/%d.mkv", i), "")
	}
	if len(result.Changes) != maxScanChangeDetails || result.OmittedChanges != 3 {
		t.Fatalf("changes = %d omitted = %d", len(result.Changes), result.OmittedChanges)
	}
	details := result.ChangeDetails()
	if got := details[len(details)-1]; got != "ℹ️ 另有 3 条媒体变化未展开" {
		t.Fatalf("last detail = %q", got)
	}
}

func probeResultFixture() *ProbeResult {
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Format: ProbeFormat{
		Name: "matroska", Duration: 120, Size: 1_000_000, BitRate: 8_000_000,
	}, Streams: []ProbeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160},
		{Index: 3, CodecType: "audio", CodecName: "eac3", Disposition: ProbeDisposition{Default: true}},
	}}
	return &ProbeResult{DurationSec: 120, Width: 3840, Height: 2160, VideoCodec: "hevc", AudioCodec: "eac3", Container: "matroska", Document: doc}
}

func TestProjectProbeSummaryClearsMissingValues(t *testing.T) {
	row := model.MediaProbeMetadata{
		DurationMS: 1, SizeBytes: 1, Container: "old", BitRate: 1,
		Width: 1, Height: 1, VideoCodec: "old", AudioCodec: "old",
	}
	projectProbeSummary(&row, &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion}, 0)
	if row.DurationMS != 0 || row.SizeBytes != 0 || row.Container != "" || row.BitRate != 0 ||
		row.Width != 0 || row.Height != 0 || row.VideoCodec != "" || row.AudioCodec != "" {
		t.Fatalf("stale summary values were preserved: %#v", row)
	}
}
