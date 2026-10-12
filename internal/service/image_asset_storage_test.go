package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStoredImageCleansFailedTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeStoredImage(target, []byte("bytes"), ".test-*"); err == nil {
		t.Fatal("rename failure ignored")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".test-*")); len(leftovers) != 0 {
		t.Fatal("failed copy left temporary file")
	}
}

func TestManagedImagePathOnlyAcceptsCatalogKeys(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"sha256/aa/bb/image.jpg", "catalogs/tmdb/people/aa/bb/cc/image.jpg", "catalogs/../artwork/aa/bb/cc/image.jpg", "catalogs/tmdb/artwork/../bb/cc/image.jpg"} {
		if _, err := managedImagePath(dir, "artwork", key); err == nil {
			t.Fatalf("invalid key accepted: %s", key)
		}
	}
	key := "catalogs/tmdb/artwork/aa/bb/cc/image.jpg"
	got, err := managedImagePath(dir, "artwork", key)
	if err != nil || got != filepath.Join(dir, filepath.FromSlash(key)) {
		t.Fatalf("catalog path: %s, %v", got, err)
	}
}
