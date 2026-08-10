package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestRepairSTRMFilesRewritesOwnedPlaybackURLs(t *testing.T) {
	outDir := t.TempDir()
	absoluteStreamPath := filepath.Join(outDir, "Movies", "A.strm")
	streamPath := filepath.Join(outDir, "Shows", "S01E01.strm")
	externalPath := filepath.Join(outDir, "External.strm")
	for _, path := range []string{absoluteStreamPath, streamPath, externalPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(absoluteStreamPath, []byte("http://old.local/api/stream/media-2?token=old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(streamPath, []byte("/api/stream/media-1?token=old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalPath, []byte("https://cdn.example.com/video.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewSTRMService(zap.NewNop(), nil, nil)
	res, err := svc.RepairFiles(t.Context(), RepairSTRMOptions{
		OutputDir: outDir,
		BaseURL:   "https://media.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Repaired != 2 || res.Skipped != 1 || len(res.Errors) != 0 {
		t.Fatalf("result = %#v, want two repaired and one skipped", res)
	}
	if got := readSTRM(t, absoluteStreamPath); got != "https://media.example.com/api/stream/media-2?token=old" {
		t.Fatalf("absolute stream strm = %q", got)
	}
	if got := readSTRM(t, streamPath); got != "https://media.example.com/api/stream/media-1?token=old" {
		t.Fatalf("stream strm = %q", got)
	}
	if got := readSTRM(t, externalPath); got != "https://cdn.example.com/video.mkv" {
		t.Fatalf("external strm should not change: %q", got)
	}
}

func TestRepairSTRMFilesDryRunDoesNotWrite(t *testing.T) {
	outDir := t.TempDir()
	filePath := filepath.Join(outDir, "Movie.strm")
	original := "http://old.local/api/stream/media-1?token=old\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewSTRMService(zap.NewNop(), nil, nil)

	res, err := svc.RepairFiles(t.Context(), RepairSTRMOptions{
		OutputDir: outDir,
		BaseURL:   "https://media.example.com",
		DryRun:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Previewed != 1 || res.Repaired != 0 || len(res.Items) != 1 {
		t.Fatalf("result = %#v, want one repair preview", res)
	}
	if got := readSTRM(t, filePath); got != strings.TrimSpace(original) {
		t.Fatalf("dry run changed file: %q", got)
	}
}
