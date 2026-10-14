// Package service — internal filesystem walker shared by scanner / watcher.
package service

import (
	"io/fs"
	"os"
	"path/filepath"
)

// walkInfo is a tiny abstraction over os.FileInfo so that callers do not
// need to depend on os/io packages directly.
type walkInfo struct {
	isDir     bool
	size      int64
	modTimeNS int64
	fileInfo  os.FileInfo
}

// walk traverses root depth-first calling fn for every entry. Hidden
// directories (starting with ".") are skipped.
func walk(root string, fn func(string, walkInfo) error) error {
	return walkFiltered(root, nil, fn)
}

// walkFiltered 在读取文件属性前过滤无关文件；目录错误仍完整传播。
func walkFiltered(root string, include func(string) bool, fn func(string, walkInfo) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && name != "." && len(name) > 1 && name[0] == '.' {
			return filepath.SkipDir
		}
		info := walkInfo{isDir: d.IsDir()}
		if !d.IsDir() {
			if include != nil && !include(path) {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			info.fileInfo = fi
			info.size = fi.Size()
			info.modTimeNS = fi.ModTime().UnixNano()
		}
		return fn(path, info)
	})
}
