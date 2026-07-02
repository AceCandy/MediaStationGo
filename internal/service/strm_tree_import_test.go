package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestGenerateSTRMFromTreePaths(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "strm")
	svc := NewSTRMService(zap.NewNop(), nil, nil)

	res, err := svc.GenerateFromTree(t.Context(), GenerateSTRMTreeOptions{
		Provider:   "115",
		Paths:      []string{"/电视剧/国产剧/南部档案/Season 01/Archives.S01E01.mkv", "/电视剧/国产剧/南部档案/poster.jpg", "/电视剧/国产剧/南部档案/Existing.strm"},
		TreeText:   "电视剧\n└── 国产剧\n    └── 南部档案\n        └── Existing.Tree.strm",
		SourceRoot: "/电视剧",
		OutputDir:  outDir,
		BaseURL:    "https://media.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 1 || res.Skipped != 0 || len(res.Errors) != 0 {
		t.Fatalf("result = %#v, want one generated video and ignored sidecar", res)
	}
	path := filepath.Join(outDir, "国产剧", "南部档案", "Season 01", "Archives.S01E01.strm")
	got := readSTRM(t, path)
	if !strings.HasPrefix(got, "https://media.example.com/api/cloud/play/cloud115?") {
		t.Fatalf("strm url = %q, want cloud115 play url", got)
	}
	if !strings.Contains(got, "ref=%2F%E7%94%B5%E8%A7%86%E5%89%A7%2F%E5%9B%BD%E4%BA%A7%E5%89%A7%2F%E5%8D%97%E9%83%A8%E6%A1%A3%E6%A1%88%2FSeason+01%2FArchives.S01E01.mkv") {
		t.Fatalf("strm url = %q, missing encoded source ref", got)
	}
	if _, err := os.Stat(filepath.Join(outDir, "国产剧", "南部档案", "Existing.strm")); !os.IsNotExist(err) {
		t.Fatalf("existing .strm source should be ignored by tree generator, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "电视剧", "国产剧", "南部档案", "Existing.Tree.strm")); !os.IsNotExist(err) {
		t.Fatalf("tree .strm source should be ignored by tree generator, stat err=%v", err)
	}
}

func TestGenerateSTRMFromTreeSupportsCommonVideoExtensions(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "strm")
	svc := NewSTRMService(zap.NewNop(), nil, nil)

	res, err := svc.GenerateFromTree(t.Context(), GenerateSTRMTreeOptions{
		Provider: "openlist",
		Paths: []string{
			"/Movies/BluRay.Stream.2026.m2ts",
			"/Movies/Camera.Source.2026.MTS",
			"/Movies/DVD.Feature.2026.vob",
			"/Movies/Legacy.Video.2026.wmv",
			"/Movies/Web.Legacy.2026.flv",
			"/Movies/Disc.Image.2026.iso",
		},
		OutputDir: outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 5 || len(res.Errors) != 0 {
		t.Fatalf("result = %#v, want five common video sources generated and iso ignored", res)
	}
	for _, name := range []string{
		"BluRay.Stream.2026",
		"Camera.Source.2026",
		"DVD.Feature.2026",
		"Legacy.Video.2026",
		"Web.Legacy.2026",
	} {
		got := readSTRM(t, filepath.Join(outDir, "Movies", name+".strm"))
		if !strings.Contains(got, "/api/cloud/play/openlist?") {
			t.Fatalf("%s strm url = %q, want cloud play url", name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "Movies", "Disc.Image.2026.strm")); !os.IsNotExist(err) {
		t.Fatalf("iso source should stay ignored by tree generator, stat err=%v", err)
	}
}

func TestGenerateSTRMFromTreeDryRunDoesNotWriteOrCleanup(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "strm")
	stale := filepath.Join(outDir, "Movies", "Old.Movie.strm")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewSTRMService(zap.NewNop(), nil, nil)

	res, err := svc.GenerateFromTree(t.Context(), GenerateSTRMTreeOptions{
		Provider:  "openlist",
		Paths:     []string{"/Movies/New.Movie.2026.mkv"},
		OutputDir: outDir,
		Cleanup:   true,
		DryRun:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Previewed != 1 || res.Generated != 0 || res.Updated != 0 || res.Cleaned != 0 || len(res.Errors) != 0 {
		t.Fatalf("result = %#v, want one preview and no writes", res)
	}
	if len(res.Items) != 1 || res.Items[0].Action != "preview" || res.Items[0].Reason != "generated" {
		t.Fatalf("preview item = %#v, want generated preview", res.Items)
	}
	if _, err := os.Stat(filepath.Join(outDir, "Movies", "New.Movie.2026.strm")); !os.IsNotExist(err) {
		t.Fatalf("dry run should not write new strm, stat err=%v", err)
	}
	if got := readSTRM(t, stale); got != "old" {
		t.Fatalf("dry run cleanup touched stale file: %q", got)
	}
}

func TestGenerateSTRMFromTreeDryRunDoesNotCreateOutputDir(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "missing-strm")
	svc := NewSTRMService(zap.NewNop(), nil, nil)

	res, err := svc.GenerateFromTree(t.Context(), GenerateSTRMTreeOptions{
		Provider:  "openlist",
		Paths:     []string{"/Movies/New.Movie.2026.mkv"},
		OutputDir: outDir,
		DryRun:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Previewed != 1 {
		t.Fatalf("previewed = %d, want 1", res.Previewed)
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("dry run should not create output dir, stat err=%v", err)
	}
}
