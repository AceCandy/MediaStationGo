//go:build unix

package service

import (
	"fmt"
	"os"
	"syscall"
)

// fileIdentity returns a stable "device:inode" identifier for the file at
// path. Hardlinks to the same data share an identity, which lets the scanner
// avoid importing the same physical file twice when a source and its organized
// hardlink both exist. ok is false when the identity cannot be determined.
func fileIdentity(path string) (string, bool) {
	return fileIdentityFromInfo(path, nil)
}

// fileIdentityFromInfo 复用遍历属性；符号链接仍按目标 inode 去重。
func fileIdentityFromInfo(path string, fi os.FileInfo) (string, bool) {
	if fi == nil || fi.Mode()&os.ModeSymlink != 0 {
		var err error
		fi, err = os.Stat(path)
		if err != nil {
			return "", false
		}
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return "", false
	}
	// 单链接文件没有去重意义，避免给独立文件也打上可碰撞的标识。
	if st.Nlink < 2 {
		return "", false
	}
	return fmt.Sprintf("%d:%d", uint64(st.Dev), uint64(st.Ino)), true
}
