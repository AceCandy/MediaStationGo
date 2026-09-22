//go:build !linux

package service

import "syscall"

// renameNoReplace 非 Linux 平台使用独占复制，避免普通 Rename 覆盖目标。
func renameNoReplace(src, dst string) error {
	return syscall.EXDEV
}
