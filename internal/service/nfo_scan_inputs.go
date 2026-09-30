package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const maxNFOScanCacheEntries = 512

type nfoInputVersion struct {
	Device uint64 `json:"dev"`
	Inode  uint64 `json:"ino"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	MTime  int64  `json:"mtime"`
	CTime  int64  `json:"ctime"`
}

type nfoScanInputs struct {
	Version int                        `json:"version"`
	Context string                     `json:"context"`
	Files   map[string]nfoInputVersion `json:"files"`
	Sources map[string]bool            `json:"sources,omitempty"`
}

type nfoCachedDocument struct {
	version nfoInputVersion
	doc     *nfoDocument
}

type nfoCachedPoster struct {
	version nfoInputVersion
	poster  bool
}

type nfoCachedArtwork struct {
	version nfoInputVersion
	stored  nfoInputVersion
	asset   model.ArtworkAsset
}

// nfoScanCache 仅在扫描批次内复用成功结果，不缓存失败或可变的入库对象。
type nfoScanCache struct {
	uses    int
	docs    map[string]nfoCachedDocument
	posters map[string]nfoCachedPoster
	artwork map[string]nfoCachedArtwork
}

// nfoScanReader 记录选择资料时实际检查的文件与目录；nil 保持普通库读取行为。
type nfoScanReader struct {
	cache    *nfoScanCache
	inputs   nfoScanInputs
	unstable bool
}

func newNFOScanReader(batch *localMediaWriteBatch) *nfoScanReader {
	var cache *nfoScanCache
	if batch != nil {
		cache = batch.nfoCache
		if cache != nil && cache.uses >= batch.limit {
			cache = nil
		}
	}
	if cache == nil {
		cache = &nfoScanCache{docs: map[string]nfoCachedDocument{}, posters: map[string]nfoCachedPoster{}, artwork: map[string]nfoCachedArtwork{}}
	}
	cache.uses++
	if batch != nil {
		batch.nfoCache = cache
	}
	return &nfoScanReader{cache: cache, inputs: nfoScanInputs{Version: 1, Files: map[string]nfoInputVersion{}, Sources: map[string]bool{}}}
}

func (r *nfoScanReader) record(path string, info os.FileInfo) nfoInputVersion {
	version, reliable := nfoFileVersion(info)
	if !reliable {
		r.unstable = true
	}
	path = filepath.Clean(path)
	old, exists := r.inputs.Files[path]
	if exists && old != version {
		r.unstable = true
	}
	// ponytail: 超大候选目录回退完整读取，避免依赖快照和缓存随目录无限增长。
	if !exists && len(r.inputs.Files) >= maxNFOScanCacheEntries {
		r.unstable = true
		return version
	}
	r.inputs.Files[path] = version
	return version
}

// directory 在探测候选前记录父目录；新建或删除候选必须让快照失效。
func (r *nfoScanReader) directory(path string) {
	if r == nil {
		return
	}
	path = filepath.Clean(path)
	for {
		if _, ok := r.inputs.Files[path]; ok {
			return
		}
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				r.unstable = true
			}
			r.record(path, info)
			return
		}
		// 断链的目标可在其他目录恢复，当前父目录不能可靠代表它。
		if link, linkErr := os.Lstat(path); linkErr == nil && link.Mode()&os.ModeSymlink != 0 {
			r.unstable = true
		}
		parent := filepath.Dir(path)
		if !errors.Is(err, os.ErrNotExist) || parent == path {
			r.unstable = true
			return
		}
		path = parent
	}
}

func (r *nfoScanReader) stat(path string) (os.FileInfo, error) {
	if r != nil {
		r.directory(filepath.Dir(path))
	}
	info, err := os.Stat(path)
	if r != nil {
		if err == nil {
			r.record(path, info)
		} else if !errors.Is(err, os.ErrNotExist) {
			r.unstable = true
		} else if link, linkErr := os.Lstat(path); linkErr == nil && link.Mode()&os.ModeSymlink != 0 {
			r.unstable = true
		}
	}
	return info, err
}

func (r *nfoScanReader) fileExists(path string) bool {
	info, err := r.stat(path)
	return err == nil && !info.IsDir()
}

// globDirectories 保留原 Glob 的模式语义，并覆盖模式目录的新增匹配。
func (r *nfoScanReader) globDirectories(pattern string) {
	if r == nil {
		return
	}
	if !strings.ContainsAny(pattern, "*?[\\") {
		r.directory(pattern)
		return
	}
	parent := filepath.Dir(pattern)
	if parent == pattern {
		r.unstable = true
		return
	}
	r.globDirectories(parent)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		r.unstable = true
	}
	for _, path := range matches {
		r.directory(path)
	}
}

func (r *nfoScanReader) glob(pattern string) ([]string, error) {
	r.globDirectories(filepath.Dir(pattern))
	return filepath.Glob(pattern)
}

func nfoScanContext(lib *model.Library, media *model.Media, root string) string {
	data, _ := json.Marshal([]string{lib.Type, filepath.Clean(root), media.Title, media.STRMURL})
	// 只保存上下文摘要，避免在依赖快照中重复持久化 STRM URL 参数。
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (inputs *nfoScanInputs) unchanged(context string, proxy *ImageProxy) bool {
	if inputs.Version != 1 || inputs.Context != context || len(inputs.Files) == 0 {
		return false
	}
	for path, old := range inputs.Files {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		version, reliable := nfoFileVersion(info)
		if !reliable || version != old {
			return false
		}
	}
	for path := range inputs.Sources {
		if proxy == nil || !proxy.isAllowedLocalPath(path) {
			return false
		}
	}
	return true
}

// snapshot 仅保存从读取开始到结束都稳定的成功输入，失败仍走完整读取。
func (r *nfoScanReader) snapshot(context string, proxy *ImageProxy) string {
	r.inputs.Context = context
	if r.unstable || !r.inputs.unchanged(context, proxy) {
		return ""
	}
	data, err := json.Marshal(r.inputs)
	if err != nil {
		return ""
	}
	return string(data)
}

func (r *nfoScanReader) prepareLocalAsset(store *ArtworkStore, owner, kind, path string) (*model.ArtworkAsset, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil || store.imageProxy == nil || !store.imageProxy.isAllowedLocalPath(abs) {
		return nil, errors.New("artwork path is outside allowed roots")
	}
	r.inputs.Sources[abs] = true
	info, err := r.stat(abs)
	if err != nil {
		return nil, err
	}
	version, reliable := nfoFileVersion(info)
	if cached, ok := r.cache.artwork[abs]; ok && reliable && cached.version == version {
		storedPath, pathErr := store.pathForStorageKey(cached.asset.StorageKey)
		if stored, statErr := os.Stat(storedPath); pathErr == nil && statErr == nil && r.record(storedPath, stored) == cached.stored {
			asset := cached.asset
			return &asset, nil
		}
	}
	asset, err := store.prepareLocalAsset(owner, kind, abs)
	if err != nil {
		return nil, err
	}
	_, _ = r.stat(abs)
	storedPath, err := store.pathForStorageKey(asset.StorageKey)
	if err != nil {
		return nil, err
	}
	stored, err := os.Stat(storedPath)
	if err != nil {
		return nil, err
	}
	storedVersion := r.record(storedPath, stored)
	if reliable && !r.unstable && len(r.cache.artwork) < maxNFOScanCacheEntries {
		r.cache.artwork[abs] = nfoCachedArtwork{version: version, stored: storedVersion, asset: *asset}
	}
	return asset, nil
}
