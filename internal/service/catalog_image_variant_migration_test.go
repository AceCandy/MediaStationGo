package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogImageVariantMigrationReusesExistingCaches(t *testing.T) {
	for _, tc := range []struct {
		name, oldRoot string
		targets       []string
	}{
		{"artwork", "artwork", []string{"tmdb/artwork", "douban/artwork"}},
		{"artwork_relative", "artwork", []string{"tmdb/artwork"}},
		{"people", "people", []string{"tmdb/people", "douban/people"}},
		{"hongguo", "catalogs/hongguo/artwork", []string{"hongguo/artwork", "hongguo/people"}},
		{"huangguoai", "catalogs/huangguoai/artwork", []string{"huangguoai/artwork"}},
		{"cover", "library-covers/library-hash", []string{"local/artwork"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir, cacheDir := t.TempDir(), t.TempDir()
			if tc.name == "cover" || tc.name == "artwork_relative" {
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				dataDir, err = filepath.Rel(cwd, dataDir)
				if err != nil {
					t.Fatal(err)
				}
			}
			data := variantPNG(t, 60, 40)
			stored, err := prepareStoredImage(data)
			if err != nil {
				t.Fatal(err)
			}
			oldKey := filepath.Join("sha256", stored.SHA256[:2], stored.SHA256[2:4], filepath.Base(stored.StorageKey))
			oldPath, err := storedImagePath(dataDir, filepath.Join(tc.oldRoot, oldKey))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "cover" {
				oldPath = filepath.Join(dataDir, tc.oldRoot, stored.SHA256)
			}
			if err := writeStoredImage(oldPath, data, ".test-*"); err != nil {
				t.Fatal(err)
			}
			v := newImageVariants(cacheDir, dataDir)
			query := "/?maxWidth=20&format=png"
			w := httptest.NewRecorder()
			if !v.serveFile(w, httptest.NewRequest("GET", query, nil), "old", oldPath, imageBrowserCacheControl) {
				t.Fatal("old source not served")
			}
			stat, _ := os.Stat(oldPath)
			// 独立按原有格式计算版本，防止迁移和读取同时改错缓存身份。
			version := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("v1:%s:%d:%d", oldPath, stat.Size(), stat.ModTime().UnixNano()))))
			spec := "w0-h0-mw20-mh0-fw0-fh0-q80.png"
			oldCache := filepath.Join(v.dir, v.sourceGroup("", oldPath), version, spec)
			cached := append(append([]byte{}, w.Body.Bytes()...), []byte("cached-variant-marker")...)
			if err := os.WriteFile(oldCache, cached, 0600); err != nil {
				t.Fatal(err)
			}
			var targets []string
			for _, target := range tc.targets {
				path, err := storedImagePath(dataDir, filepath.Join("catalogs", target, stored.StorageKey))
				if err != nil {
					t.Fatal(err)
				}
				if err := writeStoredImage(path, data, ".test-*"); err != nil {
					t.Fatal(err)
				}
				targets = append(targets, path)
			}
			count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir)
			if err != nil || count != len(targets) {
				t.Fatalf("migration: %d %v", count, err)
			}
			for _, target := range targets {
				adjacent := filepath.Join(filepath.Dir(target), stored.SHA256+".thumbnails", "v1", spec)
				if got, err := os.ReadFile(adjacent); err != nil || !bytes.Equal(got, cached) {
					t.Fatal("migrated cache missing beside original")
				}
				w = httptest.NewRecorder()
				if !v.serveFile(w, httptest.NewRequest("GET", query, nil), "new", target, imageBrowserCacheControl) || !bytes.Equal(w.Body.Bytes(), cached) {
					t.Fatal("new source regenerated instead of reusing migrated cache")
				}
				head := httptest.NewRecorder()
				v.serveFile(head, httptest.NewRequest("HEAD", query, nil), "new", target, imageBrowserCacheControl)
				if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != w.Header().Get("Content-Length") {
					t.Fatal("migrated HEAD changed")
				}
				req := httptest.NewRequest("GET", query, nil)
				req.Header.Set("If-None-Match", w.Header().Get("ETag"))
				conditional := httptest.NewRecorder()
				v.serveFile(conditional, req, "new", target, imageBrowserCacheControl)
				if conditional.Code != 304 {
					t.Fatal("migrated ETag not honored")
				}
			}
			if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 0 {
				t.Fatalf("repeat: %d %v", count, err)
			}
			if got, err := os.ReadFile(oldCache); err != nil || !bytes.Equal(got, cached) {
				t.Fatal("old cache removed")
			}
			if err := os.WriteFile(oldCache, append(append([]byte{}, cached...), 'x'), 0600); err != nil {
				t.Fatal(err)
			}
			if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 0 {
				t.Fatalf("existing new cache overwritten: %d %v", count, err)
			}
			w = httptest.NewRecorder()
			v.serveFile(w, httptest.NewRequest("GET", query, nil), "new", targets[0], imageBrowserCacheControl)
			if !bytes.Equal(w.Body.Bytes(), cached) {
				t.Fatal("existing target bytes changed")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := MigrateCatalogImageVariants(ctx, dataDir, cacheDir); err != context.Canceled {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}

func TestCatalogImageVariantMigrationSkipsStaleAndChangedSources(t *testing.T) {
	dataDir, cacheDir := t.TempDir(), t.TempDir()
	data := variantPNG(t, 60, 40)
	stored, _ := prepareStoredImage(data)
	oldPath := filepath.Join(dataDir, "artwork", "sha256", stored.SHA256[:2], stored.SHA256[2:4], filepath.Base(stored.StorageKey))
	newPath := filepath.Join(dataDir, "catalogs", "tmdb", "artwork", stored.StorageKey)
	for _, path := range []string{oldPath, newPath} {
		if err := writeStoredImage(path, data, ".test-*"); err != nil {
			t.Fatal(err)
		}
	}
	v := newImageVariants(cacheDir, dataDir)
	req := httptest.NewRequest("GET", "/?maxWidth=20&format=png", nil)
	v.serveFile(httptest.NewRecorder(), req, "old", oldPath, imageBrowserCacheControl)
	stat, _ := os.Stat(oldPath)
	later := stat.ModTime().Add(time.Second)
	if err := os.Chtimes(oldPath, later, later); err != nil {
		t.Fatal(err)
	}
	if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 0 {
		t.Fatalf("stale version copied: %d %v", count, err)
	}
	v.serveFile(httptest.NewRecorder(), req, "old", oldPath, imageBrowserCacheControl)
	changed := append([]byte{}, data...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(newPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 0 {
		t.Fatalf("different original copied: %d %v", count, err)
	}
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 0 {
		t.Fatalf("missing target: %d %v", count, err)
	}
	if err := writeStoredImage(newPath, data, ".test-*"); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(filepath.Dir(newPath), stored.SHA256+".thumbnails")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err == nil {
		t.Fatal("write failure ignored")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 1 {
		t.Fatalf("retry: %d %v", count, err)
	}
}

func TestCatalogImageVariantMigrationFromCatalogCache(t *testing.T) {
	for _, state := range []string{"current", "stale", "changed", "missing"} {
		t.Run(state, func(t *testing.T) {
			dataDir, cacheDir := t.TempDir(), t.TempDir()
			data := variantPNG(t, 60, 40)
			stored, _ := prepareStoredImage(data)
			path := filepath.Join(dataDir, "catalogs", "douban", "people", stored.StorageKey)
			if err := writeStoredImage(path, data, ".test-*"); err != nil {
				t.Fatal(err)
			}
			stat, _ := os.Stat(path)
			// 手工构造上一版 catalog 缓存；无需存在最早的 people/sha256 原图。
			version := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("v1:%s:%d:%d", path, stat.Size(), stat.ModTime().UnixNano()))))
			spec := "w0-h0-mw20-mh0-fw0-fh0-q80.png"
			group := filepath.Join("catalogs", "douban", "people", stored.SHA256[:2], stored.SHA256[2:4], stored.SHA256[4:6], stored.SHA256)
			oldCache := filepath.Join(cacheDir, "image-variants", group, version, spec)
			cached := append(variantPNG(t, 20, 13), []byte("catalog-cache-marker")...)
			if err := writeStoredImage(oldCache, cached, ".test-*"); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "stale":
				later := stat.ModTime().Add(time.Second)
				if err := os.Chtimes(path, later, later); err != nil {
					t.Fatal(err)
				}
			case "changed":
				changed := append([]byte{}, data...)
				changed[len(changed)-1] ^= 1
				if err := os.WriteFile(path, changed, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir)
			want := 0
			if state == "current" {
				want = 1
			}
			if err != nil || count != want {
				t.Fatalf("catalog migration: %d %v", count, err)
			}
			adjacent := filepath.Join(filepath.Dir(path), stored.SHA256+".thumbnails", "v1", spec)
			if state != "current" {
				if _, err := os.Stat(adjacent); !os.IsNotExist(err) {
					t.Fatal("unverified cache copied")
				}
				return
			}
			if got, err := os.ReadFile(adjacent); err != nil || !bytes.Equal(got, cached) {
				t.Fatal("catalog thumbnail not preserved")
			}
			v := newImageVariants(cacheDir, dataDir)
			w := httptest.NewRecorder()
			v.serveFile(w, httptest.NewRequest("GET", "/?maxWidth=20&format=png", nil), "source", path, imageBrowserCacheControl)
			if !bytes.Equal(w.Body.Bytes(), cached) {
				t.Fatal("migrated catalog cache regenerated")
			}
			if count, err := MigrateCatalogImageVariants(t.Context(), dataDir, cacheDir); err != nil || count != 0 {
				t.Fatalf("repeat: %d %v", count, err)
			}
			if got, err := os.ReadFile(oldCache); err != nil || !bytes.Equal(got, cached) {
				t.Fatal("old catalog cache changed")
			}
		})
	}
}
