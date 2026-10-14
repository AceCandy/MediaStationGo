package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const localScanReadBatch = 500

// ScanTimings 记录扫描阶段累计耗时；遍历处理包含指纹查询和入库耗时。
type ScanTimings struct {
	SnapshotMS int64 `json:"snapshot_ms"`
	WalkMS     int64 `json:"walk_ms"`
	PruneMS    int64 `json:"prune_ms"`
	PartsMS    int64 `json:"parts_ms"`
}

func (t ScanTimings) Metrics() map[string]int64 {
	return map[string]int64{"snapshot_ms": t.SnapshotMS, "walk_ms": t.WalkMS, "prune_ms": t.PruneMS, "parts_ms": t.PartsMS}
}

func (res *ScanResult) trackPartDirectory(path string) {
	if res.partDirectories == nil {
		res.partDirectories = map[string]struct{}{}
	}
	res.partDirectories[filepath.Dir(path)] = struct{}{}
}

func (s *ScannerService) reconcileScanParts(ctx context.Context, lib *model.Library, res *ScanResult) {
	start := time.Now()
	defer func() { res.PartsMS += time.Since(start).Milliseconds() }()
	directories := make([]string, 0, len(res.partDirectories))
	for dir := range res.partDirectories {
		directories = append(directories, dir)
	}
	sort.Strings(directories)
	for _, dir := range directories {
		changed, err := s.reconcileMediaParts(ctx, lib.ID, dir, false, res)
		recordMediaPartScanChanges(res, changed)
		if err != nil {
			addScanError(res, dir, err)
		}
	}
	res.partDirectories = nil
}

// scanLocalLibraryFiles 只保留当前批次的文件指纹；硬链接跨批次通过已提交行延续去重。
func (s *ScannerService) scanLocalLibraryFiles(ctx context.Context, lib *model.Library, root *model.LibraryRoot, writeBatch *localMediaWriteBatch, res *ScanResult, progress ScanProgressFunc, rootIndex, rootTotal, rootsCompleted int) error {
	started := time.Now()
	defer func() { res.WalkMS += time.Since(started).Milliseconds() }()
	type entry struct {
		path   string
		info   walkInfo
		fileID string
	}
	files := make([]entry, 0, localScanReadBatch)
	flush := func() error {
		if len(files) == 0 {
			return nil
		}
		defer func() { files = files[:0] }()
		paths, fileIDs := make([]string, 0, len(files)), make([]string, 0)
		for i, f := range files {
			paths = append(paths, f.path)
			if !libraryUsesNFOOnly(lib) {
				if id, ok := fileIdentityFromInfo(f.path, f.info.fileInfo); ok {
					files[i].fileID = id
					fileIDs = append(fileIDs, id)
				}
			}
		}
		var existing map[string]existingLocalMedia
		identities := map[string]string{}
		if !libraryUsesNFOOnly(lib) {
			snapshotStart := time.Now()
			var err error
			existing, err = s.existingLocalMediaSnapshot(ctx, lib.ID, paths)
			res.snapshotElapsed += time.Since(snapshotStart)
			res.SnapshotMS = res.snapshotElapsed.Milliseconds()
			if err != nil {
				s.log.Warn("load scan batch fingerprints failed", zap.Error(err))
				existing = nil
			}
			if len(fileIDs) > 0 {
				var rows []struct {
					FileID string
					Path   string
				}
				// 已入库路径优先，覆盖同库其他根目录；每个 inode 只读取一个代表。
				if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
					Select("DISTINCT ON (file_id) file_id, path").Where("library_id = ? AND file_id IN ?", lib.ID, fileIDs).
					Order("file_id, path").Find(&rows).Error; err != nil {
					return err
				}
				for _, row := range rows {
					identities[row.FileID] = row.Path
				}
			}
		}
		for _, f := range files {
			if err := ctx.Err(); err != nil {
				return err
			}
			if old, ok := existing[filepath.Clean(f.path)]; ok && (old.PartGroupKey != "" || old.PartIndex != 0) {
				res.trackPartDirectory(f.path)
			}
			if _, candidate := parseMediaPartCandidate(f.path); candidate {
				res.trackPartDirectory(f.path)
			}
			s.ingestFile(ctx, lib, root, f.path, f.info.size, f.info.modTimeNS, identities, existing, writeBatch, res, f.fileID)
			if res.Visited%scanProgressEvery == 0 {
				s.emitScanProgress(progress, ScanProgressRunning, root, rootIndex, rootTotal, rootsCompleted, res)
			}
		}
		writeBatch.Flush()
		return nil
	}
	err := walkFiltered(root.Path, func(path string) bool {
		_, ok := videoExtensions[strings.ToLower(filepath.Ext(path))]
		return ok
	}, func(path string, info walkInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.isDir {
			return nil
		}
		files = append(files, entry{path: path, info: info})
		if len(files) == localScanReadBatch {
			return flush()
		}
		return nil
	})
	// 与原扫描一致：遍历失败前已经发现的文件仍入库，失败的根目录不清理旧记录。
	if flushErr := flush(); err == nil {
		err = flushErr
	}
	return err
}

// scanDirectoryNames 避免缺失核对再次 Stat 每个旧文件；大目录超限时回退逐路径核验。
// 缓存只保留一个目录、最多 4096 个名字，不随整个媒体库的文件数增长。
type scanDirectoryNames struct {
	directory string
	names     map[string]struct{}
}

func (c *scanDirectoryNames) contains(path string) bool {
	dir := filepath.Dir(path)
	if dir != c.directory {
		c.directory, c.names = dir, nil
		f, err := os.Open(dir)
		if err == nil {
			names, readErr := f.Readdirnames(4097)
			_ = f.Close()
			if (readErr == nil || len(names) > 0) && len(names) <= 4096 {
				c.names = make(map[string]struct{}, len(names))
				for _, name := range names {
					c.names[name] = struct{}{}
				}
			}
		}
	}
	_, ok := c.names[filepath.Base(path)]
	return ok
}
