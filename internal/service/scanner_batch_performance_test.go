package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm/logger"
)

type scanRecognitionQueryCounter struct {
	logger.Interface
	reads      atomic.Int64
	mediaPaths atomic.Int64
	snapshots  atomic.Int64
	nfoInputs  atomic.Int64
	nfoLocks   atomic.Int64
}

func (l *scanRecognitionQueryCounter) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	if strings.Contains(sql, `FROM "settings"`) && strings.Contains(sql, "recognition_words.") {
		l.reads.Add(1)
	}
	if strings.Contains(sql, `FROM "media" WHERE path =`) {
		l.mediaPaths.Add(1)
	}
	if strings.Contains(sql, `FROM "media"`) && strings.Contains(sql, `SELECT "path","season_num","scan_file_size_bytes","scan_file_mtime_ns","file_id"`) {
		l.snapshots.Add(1)
	}
	if strings.Contains(sql, "b.scan_inputs") && strings.Contains(sql, "AS compatible") {
		l.nfoInputs.Add(1)
	}
	if strings.Contains(sql, "pg_advisory_xact_lock") && strings.Contains(sql, "nfo:") {
		l.nfoLocks.Add(1)
	}
}

func TestNFOScanBatchesRecognitionAndAvoidsRedundantReads(t *testing.T) {
	for _, kind := range []string{model.LibraryTypeNFOMovie, model.LibraryTypeNFOTV} {
		t.Run(kind, func(t *testing.T) {
			scanner, repos := newScannerTestEnv(t)
			if err := repos.DB.AutoMigrate(&model.LibraryRoot{}, &model.NFOItem{}, &model.NFOMediaBinding{}); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			lib := model.Library{Name: kind, Path: root, Type: kind, Enabled: true}
			if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
				t.Fatal(err)
			}
			if err := repos.Setting.Set(t.Context(), RecognitionWordsLocalTextKey, "Wrong => Right"); err != nil {
				t.Fatal(err)
			}
			const files = 101
			paths := make([]string, files)
			for i := range files {
				paths[i] = filepath.Join(root, fmt.Sprintf("Wrong.S01E%03d.mkv", i+1))
				writeOrgFile(t, paths[i], "video")
				body := `<movie><title>Local movie</title></movie>`
				if kind == model.LibraryTypeNFOTV {
					body = fmt.Sprintf(`<episodedetails><title>Local episode</title><season>1</season><episode>%d</episode></episodedetails>`, i+1)
				}
				writeOrgFile(t, nfoPath(paths[i]), body)
			}
			counter := &scanRecognitionQueryCounter{Interface: logger.Default}
			repos.DB.Logger = counter
			info, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			_, reliable := nfoFileVersion(info)
			check := func(res *ScanResult, err error, added, updated, skipped int, pathReads, locks int64) {
				t.Helper()
				if err != nil || res == nil || res.ErrorCount != 0 || res.Added != added || res.Updated != updated || res.Skipped != skipped {
					t.Fatalf("scan=%+v err=%v, want added/updated/skipped=%d/%d/%d", res, err, added, updated, skipped)
				}
				if counter.reads.Load() != 10 || counter.mediaPaths.Load() != pathReads || counter.snapshots.Load() != 0 {
					t.Fatalf("setting/path/snapshot reads=%d/%d/%d, want 10/%d/0", counter.reads.Load(), counter.mediaPaths.Load(), counter.snapshots.Load(), pathReads)
				}
				if counter.nfoInputs.Load() != files || counter.nfoLocks.Load() != locks {
					t.Fatalf("NFO input/lock reads=%d/%d, want %d/%d", counter.nfoInputs.Load(), counter.nfoLocks.Load(), files, locks)
				}
				counter.reads.Store(0)
				counter.mediaPaths.Store(0)
				counter.snapshots.Store(0)
				counter.nfoInputs.Store(0)
				counter.nfoLocks.Store(0)
			}
			res, err := scanner.ScanLibrary(t.Context(), lib.ID)
			check(res, err, files, 0, 0, 2*files, files)
			res, err = scanner.ScanLibrary(t.Context(), lib.ID)
			if reliable {
				check(res, err, 0, 0, files, 0, 0)
			} else {
				check(res, err, 0, 0, files, files, files)
			}
			// 只修改侧车也必须更新，不能套用普通库的视频指纹跳过规则。
			body := `<movie><title>Changed movie</title></movie>`
			if kind == model.LibraryTypeNFOTV {
				body = `<episodedetails><title>Changed episode</title><season>1</season><episode>1</episode></episodedetails>`
			}
			writeOrgFile(t, nfoPath(paths[0]), body)
			var libraryRoot model.LibraryRoot
			if err := repos.DB.Where("library_id = ?", lib.ID).First(&libraryRoot).Error; err != nil {
				t.Fatal(err)
			}
			res, err = scanner.ScanLibraryRoot(t.Context(), lib.ID, libraryRoot.ID)
			if reliable {
				check(res, err, 0, 1, files-1, 2, 1)
			} else {
				check(res, err, 0, 1, files-1, files+1, files)
			}
			var binding model.NFOMediaBinding
			if err := repos.DB.Joins("JOIN media ON media.id = nfo_media_bindings.media_id").Where("media.path = ?", paths[0]).First(&binding).Error; err != nil || !strings.HasPrefix(binding.Title, "Changed") {
				t.Fatalf("sidecar binding=%+v err=%v", binding, err)
			}
			// 单文件事件使用刚修改的规则，而不是已经结束的扫描批次。
			if err := repos.Setting.Set(t.Context(), RecognitionWordsLocalTextKey, "Wrong => Latest"); err != nil {
				t.Fatal(err)
			}
			writeOrgFile(t, paths[1], "changed video")
			res, err = scanner.IngestPathResult(t.Context(), lib.ID, paths[1])
			if err != nil || res == nil || res.Updated != 1 || res.ErrorCount != 0 || counter.reads.Load() != 5 || counter.snapshots.Load() != 0 {
				t.Fatalf("single file=%+v err=%v setting reads=%d", res, err, counter.reads.Load())
			}
			media, err := repos.Media.FindByPath(t.Context(), paths[1])
			if err != nil || media == nil || media.Title != "latest" {
				t.Fatalf("single file title=%+v err=%v", media, err)
			}
		})
	}
}

func TestScanBatchRecognitionReloadsBetweenBatches(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	if err := repos.Setting.Set(t.Context(), RecognitionWordsLocalTextKey, "Wrong => First"); err != nil {
		t.Fatal(err)
	}
	counter := &scanRecognitionQueryCounter{Interface: logger.Default}
	repos.DB.Logger = counter
	batch := newLocalMediaWriteBatch(scanner, t.Context(), &ScanResult{}, 2)
	check := func(want string, reads int64) {
		t.Helper()
		title, year := batch.cleanQuery("Wrong.2021.mkv")
		if title != want || year != 2021 || counter.reads.Load() != reads {
			t.Fatalf("title=%q year=%d setting reads=%d, want %q/2021/%d", title, year, counter.reads.Load(), want, reads)
		}
	}
	check("first", 5)
	if err := repos.Setting.Set(t.Context(), RecognitionWordsLocalTextKey, "Wrong => Second"); err != nil {
		t.Fatal(err)
	}
	check("first", 5)
	check("second", 10)
	if err := repos.Setting.Set(t.Context(), RecognitionWordsEnabledKey, "false"); err != nil {
		t.Fatal(err)
	}
	check("second", 10)
	check("wrong", 15)
	check("wrong", 15)
}

func TestScanBatchesRecognitionAndHongGuoSearch(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	if err := repos.DB.AutoMigrate(&model.LibraryRoot{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	lib := model.Library{Name: "Batch scan", Path: root, Type: model.LibraryTypeHongGuo, Enabled: true}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	work := model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "12345678901", Kind: "series", Title: "Batch work"}
	if err := repos.DB.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Series [hongguo-12345678901]")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 101; i++ {
		if err := repos.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: i}).Error; err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("S01E%03d.strm", i)), []byte("https://media.example/video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var writes atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/_doc/") {
			writes.Add(1)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)
	repos.HongGuo.SetSearchBackend(repository.NewOpenSearchHongGuoBackend(config.SearchConfig{Backend: "opensearch", OpenSearchURL: upstream.URL, Index: "test"}))
	if _, err := repos.HongGuo.BackfillSearchIndex(t.Context(), 100, 0); err != nil {
		t.Fatal(err)
	}
	counter := &scanRecognitionQueryCounter{Interface: logger.Default}
	repos.DB.Logger = counter
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.ErrorCount != 0 || res.Added != 101 || writes.Load() != 2 || counter.reads.Load() != 10 {
		t.Fatalf("new scan added=%d errors=%d err=%v index writes=%d setting reads=%d", res.Added, res.ErrorCount, err, writes.Load(), counter.reads.Load())
	}
	var bindings int64
	if err := repos.DB.Model(&model.HongGuoMediaBinding{}).Count(&bindings).Error; err != nil || bindings != 101 {
		t.Fatalf("bindings=%d err=%v", bindings, err)
	}
	counter.reads.Store(0)
	writes.Store(0)
	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil || res.Skipped != 101 || res.ErrorCount != 0 || writes.Load() != 0 || counter.reads.Load() != 0 {
		t.Fatalf("unchanged scan skipped=%d errors=%d err=%v index writes=%d setting reads=%d", res.Skipped, res.ErrorCount, err, writes.Load(), counter.reads.Load())
	}
	for i := 1; i <= 2; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("S01E%03d.strm", i)), []byte("https://media.example/changed-video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var libraryRoot model.LibraryRoot
	if err := repos.DB.Where("library_id = ?", lib.ID).First(&libraryRoot).Error; err != nil {
		t.Fatal(err)
	}
	res, err = scanner.ScanLibraryRoot(t.Context(), lib.ID, libraryRoot.ID)
	if err != nil || res.Updated != 2 || res.ErrorCount != 0 || writes.Load() != 1 || counter.reads.Load() != 5 {
		t.Fatalf("root scan updated=%d errors=%d err=%v index writes=%d setting reads=%d", res.Updated, res.ErrorCount, err, writes.Load(), counter.reads.Load())
	}
	// 单文件事件必须仍然即时同步，不能复用已结束批次的状态。
	path := filepath.Join(dir, "S01E003.strm")
	if err := os.WriteFile(path, []byte("https://media.example/watcher-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := scanner.IngestPath(t.Context(), lib.ID, path); err != nil || !changed || writes.Load() != 2 || counter.reads.Load() != 10 {
		t.Fatalf("single event changed=%v err=%v index writes=%d setting reads=%d", changed, err, writes.Load(), counter.reads.Load())
	}
}
