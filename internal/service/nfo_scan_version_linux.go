//go:build linux

package service

import (
	"os"
	"syscall"
)

// nfoFileVersion 同时检查身份和变更时间，避免同大小、恢复 mtime 的替换被跳过。
func nfoFileVersion(info os.FileInfo) (nfoInputVersion, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nfoInputVersion{}, false
	}
	return nfoInputVersion{
		Device: uint64(stat.Dev), Inode: stat.Ino,
		Size: info.Size(), Mode: uint32(info.Mode()), MTime: info.ModTime().UnixNano(),
		CTime: int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec),
	}, true
}
