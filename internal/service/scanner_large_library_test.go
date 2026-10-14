package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm/logger"
)

type scanBoundedQueryCounter struct {
	logger.Interface
	snapshots   atomic.Int64
	maxSnapshot atomic.Int64
	pages       atomic.Int64
	maxPage     atomic.Int64
	parts       atomic.Int64
}

func (l *scanBoundedQueryCounter) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, rows := query()
	if strings.Contains(sql, `SELECT "path","season_num","scan_file_size_bytes"`) {
		l.snapshots.Add(1)
		if rows > l.maxSnapshot.Load() {
			l.maxSnapshot.Store(rows)
		}
	}
	if strings.Contains(sql, "SELECT id, path, library_root_id, part_group_key, part_index") {
		l.pages.Add(1)
		if rows > l.maxPage.Load() {
			l.maxPage.Store(rows)
		}
	}
	if strings.Contains(sql, `"local_metadata_hint","scan_title","scan_year","scrape_status"`) {
		l.parts.Add(1)
	}
}

func TestScanLargeLibraryBoundedReadsAndDeletion(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Large", Path: root, Type: model.LibraryTypeHongGuo, Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	roots, err := repos.Library.ListRoots(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	const total = 1201
	paths := make([]string, total)
	rows := make([]model.Media, total)
	for i := range total {
		path := filepath.Join(root, fmt.Sprintf("series-%03d", i/100), fmt.Sprintf("S01E%03d.mkv", i%100+1))
		writeTestFile(t, path, "video")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		paths[i] = path
		rows[i] = model.Media{LibraryID: lib.ID, LibraryRootID: roots[0].ID, Path: path, CatalogSource: model.TaskSystemHongGuo, SeasonNum: 1, EpisodeNum: i%100 + 1, ScanFileSizeBytes: info.Size(), ScanFileMTimeNS: info.ModTime().UnixNano()}
	}
	if err := repos.DB.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Exec("CREATE INDEX test_scan_path ON media(library_id, path)").Error; err != nil {
		t.Fatal(err)
	}
	counter := &scanBoundedQueryCounter{Interface: logger.Default}
	repos.DB.Logger = counter
	started := time.Now()
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.Skipped != total || res.Added != 0 || res.Updated != 0 || res.Removed != 0 || res.ErrorCount != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if counter.snapshots.Load() != 3 || counter.maxSnapshot.Load() > localScanReadBatch || counter.pages.Load() != 3 || counter.maxPage.Load() > localScanReadBatch || counter.parts.Load() != 0 {
		t.Fatalf("snapshots=%d max=%d prune pages=%d max=%d parts=%d", counter.snapshots.Load(), counter.maxSnapshot.Load(), counter.pages.Load(), counter.maxPage.Load(), counter.parts.Load())
	}
	t.Logf("unchanged files=%d elapsed=%s timings=%+v snapshot queries=%d prune queries=%d", total, time.Since(started), res.ScanTimings, counter.snapshots.Load(), counter.pages.Load())
	// 删除超过一个分页，包含整个目录消失；keyset 删除不能漏掉后续页。
	for _, path := range paths[:601] {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Dir(paths[0])); err != nil {
		t.Fatal(err)
	}
	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.Removed != 601 || res.Skipped != 600 || countMedia(t, repos) != 600 {
		t.Fatalf("deletion res=%+v err=%v", res, err)
	}
	if len(res.Changes) != maxScanChangeDetails || res.OmittedChanges != 601-maxScanChangeDetails {
		t.Fatal("deletion details lost bounded accounting")
	}
}

func TestScanHardlinkAcrossReadBatches(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Links", Path: root, Type: model.LibraryTypeHongGuo, Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	rows := make([]model.Media, localScanReadBatch)
	for i := range rows {
		path := filepath.Join(root, fmt.Sprintf("%04d.S01E001.mkv", i))
		writeTestFile(t, path, "video")
		info, _ := os.Stat(path)
		rows[i] = model.Media{LibraryID: lib.ID, Path: path, CatalogSource: model.TaskSystemHongGuo, SeasonNum: 1, EpisodeNum: 1, ScanFileSizeBytes: info.Size(), ScanFileMTimeNS: info.ModTime().UnixNano()}
	}
	if err := repos.DB.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := filepath.Join(root, "zz.S01E001.mkv")
	if err := os.Link(rows[0].Path, duplicate); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.ErrorCount != 0 || res.Added != 0 || res.Skipped != localScanReadBatch+1 || countMedia(t, repos) != localScanReadBatch {
		t.Fatalf("cross-batch links: %+v err=%v", res, err)
	}
}

func TestScanClearsOldPartStateWithoutFullLibraryReconcile(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Parts", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "Plain.mkv")
	writeTestFile(t, path, "video")
	info, _ := os.Stat(path)
	media := model.Media{LibraryID: lib.ID, Path: path, PartGroupKey: "old", PartIndex: 1, ScanFileSizeBytes: info.Size(), ScanFileMTimeNS: info.ModTime().UnixNano()}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.ErrorCount != 0 {
		t.Fatalf("scan: %+v %v", res, err)
	}
	stored, err := repos.Media.FindByID(t.Context(), media.ID)
	if err != nil || stored.PartGroupKey != "" || stored.PartIndex != 0 {
		t.Fatalf("stale parts=%+v err=%v", stored, err)
	}
}

func TestScanFilteredWalkAndIdentityReuse(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "first.mkv")
	linked := filepath.Join(root, "second.mkv")
	writeTestFile(t, primary, "video")
	if err := os.Link(primary, linked); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "ignore.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, ".hidden", "hidden.mkv"), "hidden")
	var paths []string
	err := walkFiltered(root, func(path string) bool { return filepath.Ext(path) == ".mkv" }, func(path string, info walkInfo) error {
		if info.isDir {
			return nil
		}
		paths = append(paths, path)
		before, _ := fileIdentity(path)
		// 删除名字后仍可由已经读取的属性取得 inode，证明没有再 Stat 这个路径。
		if err := os.Remove(path); err != nil {
			return err
		}
		after, ok := fileIdentityFromInfo(path, info.fileInfo)
		if before != "" && (!ok || after != before) {
			t.Fatalf("identity was re-read: %q %q", before, after)
		}
		return nil
	})
	if err != nil || len(paths) != 2 {
		t.Fatalf("filtered files=%v err=%v", paths, err)
	}
}

func TestScanDirectoryNameCacheIsBounded(t *testing.T) {
	root := t.TempDir()
	for i := range 4100 {
		writeTestFile(t, filepath.Join(root, fmt.Sprintf("%d.mkv", i)), "")
	}
	cache := scanDirectoryNames{}
	if cache.contains(filepath.Join(root, "0.mkv")) || len(cache.names) > 4096 {
		t.Fatal("oversized directory retained")
	}
}

func BenchmarkScanFileIdentity(b *testing.B) {
	root := b.TempDir()
	for i := range 1000 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.mkv", i)), nil, 0600); err != nil {
			b.Fatal(err)
		}
	}
	for _, reuse := range []bool{false, true} {
		b.Run(fmt.Sprintf("reuse=%v", reuse), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := walk(root, func(path string, info walkInfo) error {
					if !info.isDir {
						if reuse {
							fileIdentityFromInfo(path, info.fileInfo)
						} else {
							fileIdentity(path)
						}
					}
					return nil
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestScanHongGuoBatchFailureKeepsOtherFiles(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Rollback", Path: root, Type: model.LibraryTypeHongGuo, Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	good, bad := filepath.Join(root, "a.S01E001.mkv"), filepath.Join(root, "b.S01E002.mkv")
	writeTestFile(t, good, "video")
	writeTestFile(t, bad, "video")
	// 跨来源旧路径必须拒绝，但同批合法文件仍能在回退后成功提交。
	foreign := model.Media{LibraryID: "foreign", Path: bad}
	if err := repos.DB.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.ErrorCount != 1 || res.Added != 1 {
		t.Fatalf("fallback res=%+v err=%v", res, err)
	}
	stored, err := repos.Media.FindByPath(t.Context(), good)
	if err != nil || stored == nil || stored.CatalogSource != model.TaskSystemHongGuo {
		t.Fatalf("good media not committed: %v", err)
	}
	old, err := repos.Media.FindByPath(t.Context(), bad)
	if err != nil || old.ID != foreign.ID || old.CatalogSource != "" {
		t.Fatal("failed batch changed foreign identity")
	}
}

func TestScanSymlinkFingerprintAndDirectoryBoundary(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root, outside := t.TempDir(), t.TempDir()
	lib := model.Library{Name: "Symlinks", Path: root, Type: model.LibraryTypeHongGuo, Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "target.mkv")
	writeTestFile(t, target, "video")
	link := filepath.Join(root, "S01E001.mkv")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		res, err := scanner.ScanLibrary(t.Context(), lib.ID)
		if err != nil || res.ErrorCount != 0 || res.Visited != 1 {
			t.Fatalf("symlink scan=%+v err=%v", res, err)
		}
		if i == 0 && res.Added != 1 || i == 1 && res.Skipped != 1 {
			t.Fatal("symlink fingerprint changed")
		}
	}
}
