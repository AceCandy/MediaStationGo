//go:build linux

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

func alternateNFOPNG(t *testing.T, original []byte) []byte {
	t.Helper()
	// 相同编码长度的真实图片，避免把压缩长度巧合当成变更检测前提。
	for red := 1; red < 255; red++ {
		img := image.NewRGBA(image.Rect(0, 0, 2, 3))
		img.Set(0, 0, color.RGBA{R: uint8(red), A: 255})
		var out bytes.Buffer
		if err := png.Encode(&out, img); err != nil {
			t.Fatal(err)
		}
		if out.Len() == len(original) {
			return out.Bytes()
		}
	}
	t.Fatal("no same-size alternate PNG fixture")
	return nil
}

func overwriteNFOSameFacts(t *testing.T, path string, body []byte) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(body)) != info.Size() {
		t.Fatalf("replacement size=%d, want %d", len(body), info.Size())
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
}

func TestNFOScanCacheReusesStableInputsAndCopiesAssets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Film.mkv")
	nfo := nfoPath(path)
	poster := filepath.Join(root, "poster.png")
	writeOrgFile(t, path, "video")
	writeOrgFile(t, nfo, `<movie><title>First</title></movie>`)
	if err := os.WriteFile(poster, testArtworkPNG(t, 2, 3), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	proxy.SetLibraryRootsProvider(func() []string { return []string{root} })
	store := NewArtworkStore(cfg, repository.New(nil).Artwork, proxy)
	batch := &localMediaWriteBatch{limit: 100}
	lib := &model.Library{Type: model.LibraryTypeNFOMovie, Path: root}
	read := func(r *nfoScanReader) *repository.NFOIngest {
		t.Helper()
		_, _ = r.stat(path)
		input, err := r.readNFOIngest(lib, &model.Media{Path: path}, root)
		if err != nil || len(input.Artwork) != 1 {
			t.Fatalf("input=%+v err=%v", input, err)
		}
		for i := range input.Artwork {
			art := &input.Artwork[i]
			art.Asset, err = r.prepareLocalAsset(store, path, art.Type, art.SourcePath)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := fingerprintNFOIngest(input); err != nil {
			t.Fatal(err)
		}
		return input
	}
	firstReader := newNFOScanReader(batch)
	first := read(firstReader)
	firstDoc := batch.nfoCache.docs[nfo].doc
	first.Artwork[0].Asset.ID = "persisted"
	first.Artwork[0].Asset.UpdatedAt = time.Now()
	secondReader := newNFOScanReader(batch)
	second := read(secondReader)
	if firstDoc == nil || batch.nfoCache.docs[nfo].doc != firstDoc || len(batch.nfoCache.artwork) != 1 || len(batch.nfoCache.posters) != 1 {
		t.Fatal("successful shared inputs were not cached")
	}
	if first.Binding.Fingerprint != second.Binding.Fingerprint || second.Artwork[0].Asset.ID != "" || !second.Artwork[0].Asset.UpdatedAt.IsZero() {
		t.Fatal("persisted asset fields polluted cached inputs or fingerprint")
	}
	// 同大小并恢复 mtime 的 NFO 覆盖仍必须读取新内容。
	overwriteNFOSameFacts(t, nfo, []byte(`<movie><title>Other</title></movie>`))
	changed := read(newNFOScanReader(batch))
	if changed.Binding.Title != "Other" || changed.Binding.Fingerprint == first.Binding.Fingerprint {
		t.Fatal("same-facts replacement reused an old document")
	}
	writeOrgFile(t, nfo, `<movie>`)
	if _, _, err := newNFOScanReader(batch).readNFO(nfo); err == nil {
		t.Fatal("invalid XML reused an old successful document")
	}
	writeOrgFile(t, nfo, `<movie><title>Fixed</title></movie>`)
	if got := read(newNFOScanReader(batch)); got.Binding.Title != "Fixed" {
		t.Fatal("failed read prevented recovery")
	}
	oldCache := batch.nfoCache
	oldCache.uses = batch.limit
	if newNFOScanReader(batch).cache == oldCache {
		t.Fatal("cache did not reset at the batch boundary")
	}
	// 缓存命中也必须重新检查路径许可。
	proxy.cfg.App.DataDir = ""
	proxy.SetLibraryRootsProvider(nil)
	if _, err := secondReader.prepareLocalAsset(store, path, model.ArtworkTypePoster, poster); err == nil {
		t.Fatal("cached asset bypassed current path permissions")
	}
}

func TestNFOScanInputsDetectSelectionChangesAndUnstableReads(t *testing.T) {
	for _, change := range []string{"new_nfo", "new_image", "rejected_image", "xml_reference", "delete", "replacement", "unstable", "broken_symlink"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Film.mkv")
			nfo := filepath.Join(root, "movie.nfo")
			poster := filepath.Join(root, "poster.png")
			folder := filepath.Join(root, "folder.png")
			reference := filepath.Join(root, "extra", "image.png")
			writeOrgFile(t, path, "video")
			writeOrgFile(t, nfo, `<movie><title>Local</title><poster>`+reference+`</poster></movie>`)
			if err := os.WriteFile(poster, testArtworkPNG(t, 3, 2), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(folder, testArtworkPNG(t, 2, 3), 0600); err != nil {
				t.Fatal(err)
			}
			reader := newNFOScanReader(nil)
			_, _ = reader.stat(path)
			lib := &model.Library{Type: model.LibraryTypeNFOMovie}
			if _, err := reader.readNFOIngest(lib, &model.Media{Path: path}, root); err != nil {
				t.Fatal(err)
			}
			if data := reader.snapshot("context", nil); data == "" {
				t.Fatal("stable successful input had no snapshot")
			}
			switch change {
			case "new_nfo":
				writeOrgFile(t, nfoPath(path), `<movie><title>Preferred</title></movie>`)
			case "new_image":
				if err := os.WriteFile(filepath.Join(root, "Film-poster.png"), testArtworkPNG(t, 2, 3), 0600); err != nil {
					t.Fatal(err)
				}
			case "rejected_image":
				if err := os.WriteFile(poster, testArtworkPNG(t, 2, 3), 0600); err != nil {
					t.Fatal(err)
				}
			case "xml_reference":
				writeOrgFile(t, reference, "new candidate")
			case "delete":
				if err := os.Remove(nfo); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				info, _ := os.Stat(path)
				replacement := filepath.Join(root, "replacement")
				writeOrgFile(t, replacement, "other")
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "unstable":
				writeOrgFile(t, nfo, `<movie><title>During read</title></movie>`)
				if reader.snapshot("context", nil) != "" {
					t.Fatal("input changed during read was accepted")
				}
			case "broken_symlink":
				link := filepath.Join(root, "missing.png")
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing-target"), link); err != nil {
					t.Fatal(err)
				}
				other := newNFOScanReader(nil)
				_, _ = other.stat(link)
				if other.snapshot("context", nil) != "" {
					t.Fatal("broken symlink was treated as a stable missing candidate")
				}
			}
			if reader.inputs.unchanged("context", nil) {
				t.Fatal("selection dependency change was ignored")
			}
		})
	}
}

func TestNFOScanInputsTrackGlobDirectoriesAndBoundDependencies(t *testing.T) {
	root := t.TempDir()
	writeOrgFile(t, filepath.Join(root, "Show 1", "Season 1", "poster.png"), "image")
	pattern := filepath.Join(root, "Show [12]", "Season [12]", "*.png")
	reader := newNFOScanReader(nil)
	matches, err := reader.glob(pattern)
	if err != nil || len(matches) != 1 || reader.snapshot("glob", nil) == "" {
		t.Fatalf("glob matches=%v err=%v", matches, err)
	}
	writeOrgFile(t, filepath.Join(root, "Show 1", "Season 2", "poster.png"), "image")
	if reader.inputs.unchanged("glob", nil) {
		t.Fatal("a new matching nested directory did not invalidate the glob dependency")
	}
	// 有界依赖不能悄悄遗漏文件后仍保存可跳过的快照。
	bounded := newNFOScanReader(nil)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxNFOScanCacheEntries; i++ {
		bounded.record(filepath.Join(root, string(rune('a'+i))), info)
	}
	if len(bounded.inputs.Files) > maxNFOScanCacheEntries || !bounded.unstable || bounded.snapshot("bounded", nil) != "" {
		t.Fatal("dependency bound allowed an incomplete successful snapshot")
	}
}

func TestNFOScanInputsDetectAncestorSymlinkRedirect(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(t.TempDir(), "Show")
	second := filepath.Join(t.TempDir(), "Show")
	writeOrgFile(t, filepath.Join(first, "folder.png"), "old image")
	writeOrgFile(t, filepath.Join(second, "poster.png"), "new image")
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Dir(first), link); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(link, "Show")
	reader := newNFOScanReader(nil)
	// 缺失候选记录的是最近的目标目录；重复候选可以复用这次目录观察。
	_, _ = reader.stat(filepath.Join(parent, "poster.png"))
	_, _ = reader.stat(filepath.Join(parent, "fanart.png"))
	if reader.snapshot("symlink", nil) == "" {
		t.Fatal("stable symlink target directory had no snapshot")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(second), link); err != nil {
		t.Fatal(err)
	}
	if reader.inputs.unchanged("symlink", nil) {
		t.Fatal("ancestor symlink redirect reused a different directory identity")
	}
}

func TestNFOIncrementalScanPreservesEditsAndRepairsAssets(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	if err := repos.DB.AutoMigrate(&model.LibraryRoot{}, &model.NFOItem{}, &model.NFOMediaBinding{}, &model.ArtworkAsset{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "Film.mkv")
	nfo := filepath.Join(root, "movie.nfo")
	poster := filepath.Join(root, "poster.png")
	writeOrgFile(t, path, "video")
	writeOrgFile(t, nfo, `<movie><title>Local</title></movie>`)
	if err := os.WriteFile(poster, testArtworkPNG(t, 2, 3), 0600); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Local", Path: root, Type: model.LibraryTypeNFOMovie}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	scanner.cfg = &config.Config{App: config.AppConfig{DataDir: t.TempDir()}}
	proxy := NewImageProxy(scanner.cfg, zap.NewNop())
	proxy.SetLibraryRootsProvider(func() []string { return []string{root} })
	scanner.SetImageProxy(proxy)
	counter := &scanRecognitionQueryCounter{Interface: logger.Default}
	repos.DB.Logger = counter
	scan := func(added, updated, skipped, failures int, locks int64) {
		t.Helper()
		counter.nfoLocks.Store(0)
		res, err := scanner.ScanLibrary(t.Context(), lib.ID)
		if err != nil || res.Added != added || res.Updated != updated || res.Skipped != skipped || res.ErrorCount != failures || counter.nfoLocks.Load() != locks {
			t.Fatalf("scan=%+v err=%v locks=%d, want %d/%d/%d errors=%d locks=%d", res, err, counter.nfoLocks.Load(), added, updated, skipped, failures, locks)
		}
	}
	load := func() model.NFOMediaBinding {
		t.Helper()
		var binding model.NFOMediaBinding
		if err := repos.DB.First(&binding).Error; err != nil {
			t.Fatal(err)
		}
		return binding
	}
	scan(1, 0, 0, 0, 1)
	first := load()
	if first.ScanInputs == "" || first.PosterAssetID == "" {
		t.Fatal("first successful ingest missed inputs/artwork")
	}
	scan(0, 0, 1, 0, 0)
	if err := repos.NFO.UpdateMetadata(t.Context(), first.MediaID, first.ItemID, model.NFOFields{Title: "Manual"}, true); err != nil {
		t.Fatal(err)
	}
	scan(0, 0, 1, 0, 0)
	if load().Title != "Manual" {
		t.Fatal("unchanged scan erased manual edits")
	}
	// 旧记录无快照时完整处理一次，只更新依赖列，不覆盖手工资料。
	if err := repos.DB.Model(&model.NFOMediaBinding{}).Where("media_id = ?", first.MediaID).UpdateColumn("scan_inputs", nil).Error; err != nil {
		t.Fatal(err)
	}
	before := load()
	scan(0, 0, 1, 0, 1)
	legacy := load()
	if legacy.ScanInputs == "" || legacy.Title != "Manual" || !legacy.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("legacy snapshot upgrade rewrote accepted fields or update time")
	}
	// 未知版本及可部分解析的截断 JSON 都不能用于提前跳过。
	for _, manifest := range []string{`{"version":99,"files":{}}`, legacy.ScanInputs[:len(legacy.ScanInputs)-1]} {
		if err := repos.DB.Model(&model.NFOMediaBinding{}).Where("media_id = ?", first.MediaID).UpdateColumn("scan_inputs", manifest).Error; err != nil {
			t.Fatal(err)
		}
		scan(0, 0, 1, 0, 1)
		if load().Title != "Manual" {
			t.Fatal("invalid manifest recovery erased accepted edits")
		}
	}
	var asset model.ArtworkAsset
	if err := repos.DB.First(&asset, "id = ?", first.PosterAssetID).Error; err != nil {
		t.Fatal(err)
	}
	store := NewArtworkStore(scanner.cfg, repos.Artwork, proxy)
	stored, err := store.pathForStorageKey(asset.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	scan(0, 0, 1, 0, 1)
	if _, err := os.Stat(stored); err != nil || load().Title != "Manual" {
		t.Fatal("missing managed image was not restored safely")
	}
	scan(0, 0, 1, 0, 0)
	data := testArtworkPNG(t, 2, 3)
	// 图片内容变化用同大小的文件身份替换验证，不依赖编码后的长度巧合。
	info, _ := os.Stat(poster)
	replacement := filepath.Join(root, "replacement.png")
	if err := os.WriteFile(replacement, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, poster); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(poster, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	scan(0, 0, 1, 0, 1)
	// 内容相同的触碰不能算资料更新；同大小恢复 mtime 的新图片仍写入新资产。
	overwriteNFOSameFacts(t, poster, alternateNFOPNG(t, data))
	scan(0, 1, 0, 0, 1)
	if load().PosterAssetID == first.PosterAssetID {
		t.Fatal("image-only update reused old artwork")
	}
	accepted := load()
	writeOrgFile(t, nfo, `<movie>`)
	scan(0, 1, 0, 1, 1)
	if got := load(); got.Title != accepted.Title || got.ScanInputs != accepted.ScanInputs {
		t.Fatal("invalid NFO overwrote accepted metadata or successful inputs")
	}
	writeOrgFile(t, nfo, `<movie><title>Fixed</title></movie>`)
	scan(0, 1, 0, 0, 1)
	if load().Title != "Fixed" {
		t.Fatal("NFO repair did not recover")
	}
	if err := os.Remove(nfo); err != nil {
		t.Fatal(err)
	}
	scan(0, 1, 0, 1, 1)
	if load().Title != "Fixed" {
		t.Fatal("missing NFO erased accepted metadata")
	}
	writeOrgFile(t, nfo, `<movie><title>Again</title></movie>`)
	scan(0, 1, 0, 0, 1)
	// 仅同大小、恢复 mtime 的视频更新也应失效原探测。
	if err := repos.DB.Create(&model.MediaProbeMetadata{MediaID: first.MediaID}).Error; err != nil {
		t.Fatal(err)
	}
	overwriteNFOSameFacts(t, path, []byte("other"))
	scan(0, 1, 0, 0, 1)
	var probes int64
	if err := repos.DB.Model(&model.MediaProbeMetadata{}).Where("media_id = ?", first.MediaID).Count(&probes).Error; err != nil || probes != 0 {
		t.Fatalf("changed video retained probe: %d %v", probes, err)
	}
	beforeCancel := load()
	writeOrgFile(t, nfo, `<movie><title>Cancelled</title></movie>`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := scanner.ScanLibrary(ctx, lib.ID); err == nil {
		t.Fatal("cancelled scan unexpectedly succeeded")
	}
	if got := load(); got.Title != beforeCancel.Title || got.ScanInputs != beforeCancel.ScanInputs {
		t.Fatal("cancelled scan saved new inputs")
	}
	// 持久化快照必须有版本且包含视频与受控图片，不能只保存视频时间戳。
	var inputs nfoScanInputs
	if err := json.Unmarshal([]byte(beforeCancel.ScanInputs), &inputs); err != nil || inputs.Version != 1 || len(inputs.Files) < 4 {
		t.Fatalf("incomplete inputs: %+v %v", inputs, err)
	}
	// 无 NFO 标题时使用识别结果；只改规则也不能被旧文件快照跳过。
	writeOrgFile(t, nfo, `<movie/>`)
	scan(0, 1, 0, 0, 1)
	fallback := load().Title
	if err := repos.Setting.Set(t.Context(), RecognitionWordsLocalTextKey, "Film => Next"); err != nil {
		t.Fatal(err)
	}
	scan(0, 1, 0, 0, 1)
	if got := load().Title; got == fallback || got != "next" {
		t.Fatalf("changed rules retained fallback title %q -> %q", fallback, got)
	}
}

func TestNFOIncrementalTVTracksSharedDocumentsAndArtwork(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	if err := repos.DB.AutoMigrate(&model.LibraryRoot{}, &model.NFOItem{}, &model.NFOMediaBinding{}, &model.ArtworkAsset{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	showDir := filepath.Join(root, "Show")
	seasonDir := filepath.Join(showDir, "Season 01")
	showNFO := filepath.Join(showDir, "tvshow.nfo")
	seasonNFO := filepath.Join(seasonDir, "season.nfo")
	writeOrgFile(t, showNFO, `<tvshow><title>Show</title></tvshow>`)
	writeOrgFile(t, seasonNFO, `<season><title>Season</title><poster>../poster.png</poster></season>`)
	if err := os.WriteFile(filepath.Join(showDir, "poster.png"), testArtworkPNG(t, 2, 3), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(seasonDir, "Show.S01E01.mkv"), filepath.Join(seasonDir, "Show.S01E01 - 4K.mkv"), filepath.Join(seasonDir, "Show.S01E02.mkv")}
	for _, path := range paths {
		writeOrgFile(t, path, "video")
		writeOrgFile(t, nfoPath(path), `<episodedetails><title>Episode</title><poster>../poster.png</poster></episodedetails>`)
	}
	lib := model.Library{Name: "TV", Type: model.LibraryTypeNFOTV, Path: root}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, []model.LibraryRoot{{Path: root, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	var libraryRoot model.LibraryRoot
	if err := repos.DB.Where("library_id = ?", lib.ID).First(&libraryRoot).Error; err != nil {
		t.Fatal(err)
	}
	scanner.cfg = &config.Config{App: config.AppConfig{DataDir: t.TempDir()}}
	proxy := NewImageProxy(scanner.cfg, zap.NewNop())
	proxy.SetLibraryRootsProvider(func() []string { return []string{root} })
	scanner.SetImageProxy(proxy)
	res := &ScanResult{LibraryID: lib.ID}
	batch := newLocalMediaWriteBatch(scanner, t.Context(), res, 100)
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner.ingestFile(t.Context(), &lib, &libraryRoot, path, info.Size(), info.ModTime().UnixNano(), map[string]string{}, nil, batch, res)
	}
	if res.Added != 3 || res.ErrorCount != 0 || batch.nfoCache == nil || len(batch.nfoCache.docs) != 5 || len(batch.nfoCache.artwork) != 1 {
		t.Fatalf("shared scan=%+v cache=%+v", res, batch.nfoCache)
	}
	for _, asset := range batch.nfoCache.artwork {
		if asset.asset.ID != "" || !asset.asset.UpdatedAt.IsZero() {
			t.Fatal("repository mutation escaped into the shared image cache")
		}
	}
	batch.Flush()
	if batch.nfoCache != nil {
		t.Fatal("root flush retained shared input cache")
	}
	var assets, episodes int64
	if err := repos.DB.Model(&model.ArtworkAsset{}).Count(&assets).Error; err != nil || assets != 1 {
		t.Fatalf("assets=%d err=%v", assets, err)
	}
	if err := repos.DB.Model(&model.NFOItem{}).Where("kind = 'episode'").Count(&episodes).Error; err != nil || episodes != 2 {
		t.Fatalf("episodes=%d err=%v", episodes, err)
	}
	counter := &scanRecognitionQueryCounter{Interface: logger.Default}
	repos.DB.Logger = counter
	scan := func(updated, skipped int) {
		t.Helper()
		counter.nfoLocks.Store(0)
		result, err := scanner.ScanLibrary(t.Context(), lib.ID)
		if err != nil || result.ErrorCount != 0 || result.Updated != updated || result.Skipped != skipped || counter.nfoLocks.Load() != int64(updated) {
			t.Fatalf("scan=%+v err=%v locks=%d", result, err, counter.nfoLocks.Load())
		}
	}
	scan(0, 3)
	writeOrgFile(t, showNFO, `<tvshow><title>Changed show</title></tvshow>`)
	scan(3, 0)
	writeOrgFile(t, seasonNFO, `<season><title>Changed season</title><poster>../poster.png</poster></season>`)
	scan(3, 0)
	writeOrgFile(t, nfoPath(paths[2]), `<episodedetails><title>Changed episode</title><poster>../poster.png</poster></episodedetails>`)
	scan(1, 2)
	for kind, title := range map[string]string{"series": "Changed show", "season": "Changed season"} {
		var item model.NFOItem
		if err := repos.DB.Where("kind = ?", kind).First(&item).Error; err != nil || item.Title != title || item.PosterAssetID == "" {
			t.Fatalf("shared %s item=%+v err=%v", kind, item, err)
		}
	}
}
