// Package service — server-side file browser and safe local file operations.
//
// FileManagerService exposes a strict, allow-listed view of the server's
// filesystem. The design follows a StorageChain boundary: callers
// work with file-item-like records, while this service owns path validation,
// local storage operations, and mutation safety.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// FileManagerService browses and mutates the server-side filesystem.
type FileManagerService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewFileManagerService is the constructor.
func NewFileManagerService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *FileManagerService {
	return &FileManagerService{cfg: cfg, log: log, repo: repo}
}

// Entry is one file or directory shown in the browser.
type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Ext      string `json:"ext,omitempty"`
}

// Listing describes the contents of a directory plus navigation hints.
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent,omitempty"`
	Roots   []Root  `json:"roots,omitempty"`
	Entries []Entry `json:"entries"`
}

// Root is the entry-point label shown when no path is given.
type Root struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type FileOperationResult struct {
	Path string `json:"path"`
}

// ErrPathOutOfBounds is returned when path falls outside every allowed root.
var ErrPathOutOfBounds = errors.New("path is outside the allowed roots")

// ErrRootMutation protects configured roots such as /media and /downloads.
var ErrRootMutation = errors.New("refusing to mutate an allowed root")

// ErrSTRMTargetNotDeletable 表示 STRM 未解析到可信根内现存的本地普通文件。
var ErrSTRMTargetNotDeletable = errors.New("STRM target is not a deletable local file")

// STRMDeleteTarget 是删除确认框展示的服务端实时解析结果。
type STRMDeleteTarget struct {
	TargetPath string `json:"target_path"`
	ParentPath string `json:"parent_path,omitempty"`
}

// List enumerates a directory under one of the allowed roots, returning up to
// maxEntries items sorted by (dir-first, path). Recursive listing is capped by
// maxEntries to avoid accidentally walking huge NAS trees from the UI.
func (s *FileManagerService) List(path string, maxEntries int, recursive ...bool) (*Listing, error) {
	if maxEntries <= 0 || maxEntries > 5000 {
		maxEntries = 1000
	}
	roots, rootList, err := s.allowedRootList()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return &Listing{Path: "", Roots: rootList}, nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !s.withinAllowed(abs, roots) {
		return nil, ErrPathOutOfBounds
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return &Listing{Path: abs, Roots: rootList, Entries: []Entry{s.entryFromInfo(abs, info)}}, nil
	}

	out := &Listing{Path: abs, Roots: rootList, Entries: []Entry{}}
	parent := filepath.Dir(abs)
	if parent != abs && s.withinAllowed(parent, roots) {
		out.Parent = parent
	}
	if len(recursive) > 0 && recursive[0] {
		if err := s.walkEntries(abs, maxEntries, out); err != nil {
			return nil, err
		}
		sortFileEntries(out.Entries)
		return out, nil
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if len(out.Entries) >= maxEntries {
			break
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(abs, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		out.Entries = append(out.Entries, s.entryFromInfo(full, info))
	}
	sortFileEntries(out.Entries)
	return out, nil
}

func (s *FileManagerService) CreateFolder(parent, name string) (*FileOperationResult, error) {
	parentPath, roots, err := s.requireAllowedPath(parent, false)
	if err != nil {
		return nil, err
	}
	cleanName := sanitizeFilename(name)
	if strings.TrimSpace(name) == "" || cleanName == "" || strings.ContainsAny(name, `/\`) {
		return nil, errors.New("invalid folder name")
	}
	dst := filepath.Join(parentPath, cleanName)
	if !s.withinAllowed(dst, roots) {
		return nil, ErrPathOutOfBounds
	}
	if err := os.MkdirAll(dst, 0o755); err != nil { // #nosec G301 -- user-created media directories must remain readable by NAS/player users.
		return nil, err
	}
	return &FileOperationResult{Path: dst}, nil
}

func (s *FileManagerService) Rename(path, name string) (*FileOperationResult, error) {
	src, roots, err := s.requireAllowedPath(path, true)
	if err != nil {
		return nil, err
	}
	cleanName := sanitizeFilename(name)
	if strings.TrimSpace(name) == "" || cleanName == "" || strings.ContainsAny(name, `/\`) {
		return nil, errors.New("invalid name")
	}
	dst := filepath.Join(filepath.Dir(src), cleanName)
	if !s.withinAllowed(dst, roots) {
		return nil, ErrPathOutOfBounds
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("target already exists: %s", dst)
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, err
	}
	return &FileOperationResult{Path: dst}, nil
}

func (s *FileManagerService) Delete(path string) error {
	target, _, err := s.requireAllowedPath(path, true)
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}

// ResolveSTRMDeleteTarget 从具体媒体的 sidecar 和当前路径映射解析可安全删除的本地目标。
func (s *FileManagerService) ResolveSTRMDeleteTarget(ctx context.Context, mediaID string) (*STRMDeleteTarget, error) {
	if s == nil || s.repo == nil || s.repo.Media == nil {
		return nil, ErrMediaNotFound
	}
	media, err := s.repo.Media.FindByID(ctx, strings.TrimSpace(mediaID))
	if err != nil {
		return nil, err
	}
	if media == nil {
		return nil, ErrMediaNotFound
	}
	if !strings.EqualFold(filepath.Ext(media.Path), ".strm") {
		return nil, ErrSTRMTargetNotDeletable
	}
	rawTarget, err := readLocalSTRMTarget(media.Path)
	if err != nil {
		return nil, err
	}

	var target string
	var trustedRoots []string
	if isLocalSTRMMediaTarget(rawTarget) {
		target = rawTarget
		roots, _, rootsErr := s.allowedRootList()
		if rootsErr != nil {
			return nil, rootsErr
		}
		for _, root := range roots {
			trustedRoots = append(trustedRoots, root)
		}
	} else if isHTTPPlaybackTarget(rawTarget) {
		rawMappings := ""
		if s.repo.Setting != nil {
			rawMappings, _ = s.repo.Setting.Get(ctx, FFprobePathMappingsSettingKey)
		}
		var mappingRoot string
		target, mappingRoot = mapRemoteProbePathWithRoot(rawMappings, rawTarget)
		if mappingRoot != "" {
			trustedRoots = []string{mappingRoot}
		}
	}
	if target == "" || len(trustedRoots) == 0 {
		return nil, ErrSTRMTargetNotDeletable
	}

	target, trustedRoot, err := secureSTRMDeleteTarget(target, trustedRoots)
	if err != nil {
		return nil, err
	}
	result := &STRMDeleteTarget{TargetPath: target}
	parent := filepath.Dir(target)
	sidecar, _ := filepath.Abs(media.Path)
	realSidecar, _ := filepath.EvalSymlinks(sidecar)
	sidecarInsideParent := pathWithin(sidecar, parent) || (realSidecar != "" && pathWithin(realSidecar, parent))
	if parent != target && !strings.EqualFold(parent, trustedRoot) && pathWithin(parent, trustedRoot) && !sidecarInsideParent {
		result.ParentPath = parent
	}
	return result, nil
}

// DeleteSTRMTarget 只删除 STRM 指向的本地文件或其父目录，不修改 sidecar 和数据库。
func (s *FileManagerService) DeleteSTRMTarget(ctx context.Context, mediaID string, deleteParent bool) (string, error) {
	resolved, err := s.ResolveSTRMDeleteTarget(ctx, mediaID)
	if err != nil {
		return "", err
	}
	if !deleteParent {
		info, err := os.Lstat(resolved.TargetPath)
		if err != nil || !info.Mode().IsRegular() {
			if err != nil {
				return "", err
			}
			return "", ErrSTRMTargetNotDeletable
		}
		return resolved.TargetPath, os.Remove(resolved.TargetPath)
	}
	if resolved.ParentPath == "" {
		return "", ErrRootMutation
	}
	return resolved.ParentPath, os.RemoveAll(resolved.ParentPath)
}

func secureSTRMDeleteTarget(target string, trustedRoots []string) (string, string, error) {
	absTarget, err := filepath.Abs(strings.TrimSpace(target))
	if err != nil {
		return "", "", err
	}
	info, err := os.Lstat(absTarget)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		if err != nil {
			return "", "", err
		}
		return "", "", ErrPathOutOfBounds
	}
	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		return "", "", err
	}
	info, err = os.Stat(realTarget)
	if err != nil || !info.Mode().IsRegular() {
		if err != nil {
			return "", "", err
		}
		return "", "", ErrSTRMTargetNotDeletable
	}

	bestRoot := ""
	for _, root := range trustedRoots {
		absRoot, err := filepath.Abs(strings.TrimSpace(root))
		if err != nil {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil || filepath.Dir(realRoot) == realRoot || !pathWithin(realTarget, realRoot) {
			continue
		}
		if len(realRoot) > len(bestRoot) {
			bestRoot = filepath.Clean(realRoot)
		}
	}
	if bestRoot == "" {
		return "", "", ErrPathOutOfBounds
	}
	return filepath.Clean(realTarget), bestRoot, nil
}

func (s *FileManagerService) Transfer(sourcePath, destDir string, mode TransferMode) (*FileOperationResult, error) {
	src, roots, err := s.requireAllowedPath(sourcePath, true)
	if err != nil {
		return nil, err
	}
	dstDir, _, err := s.requireAllowedPath(destDir, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if !s.withinAllowed(dst, roots) {
		return nil, ErrPathOutOfBounds
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("target already exists: %s", dst)
	}
	if mode == "" {
		mode = TransferCopy
	}
	if info.IsDir() {
		if err := transferDirectory(src, dst, mode); err != nil {
			return nil, err
		}
	} else if err := transferFile(src, dst, mode); err != nil {
		return nil, err
	}
	return &FileOperationResult{Path: dst}, nil
}

func (s *FileManagerService) walkEntries(root string, maxEntries int, out *Listing) error {
	count := 0
	return filepath.WalkDir(root, func(full string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if full == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= maxEntries {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out.Entries = append(out.Entries, s.entryFromInfo(full, info))
		count++
		return nil
	})
}

func (s *FileManagerService) entryFromInfo(path string, info os.FileInfo) Entry {
	return Entry{
		Name:     filepath.Base(path),
		Path:     path,
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		Modified: info.ModTime().Unix(),
		Ext:      strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
}

func sortFileEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Path) < strings.ToLower(entries[j].Path)
	})
}
