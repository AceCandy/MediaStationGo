//go:build !linux

package service

import "os"

// 无可靠的文件变更时间时保留完整读取，不能仅凭 size/mtime 跳过侧车。
func nfoFileVersion(os.FileInfo) (nfoInputVersion, bool) {
	return nfoInputVersion{}, false
}
