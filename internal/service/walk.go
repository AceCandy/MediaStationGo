// Package service — internal filesystem walker shared by scanner / watcher.
package service

import (
	"io/fs"
	"path/filepath"
)

// walkInfo is a tiny abstraction over os.FileInfo so that callers do not
// need to depend on os/io packages directly.
type walkInfo struct {
	isDir bool
	size  int64
}

// walk traverses root depth-first calling fn for every entry. Hidden
// directories (starting with ".") are skipped.
func walk(root string, fn func(string, walkInfo) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best effort — keep walking
		}
		name := d.Name()
		if d.IsDir() && name != "." && len(name) > 1 && name[0] == '.' {
			return filepath.SkipDir
		}
		info := walkInfo{isDir: d.IsDir()}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				info.size = fi.Size()
			}
		}
		return fn(path, info)
	})
}
