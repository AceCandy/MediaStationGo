package service

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneImageFile 使用写时复制生成独立文件，后续写入和删除不影响源文件。
func cloneImageFile(dst, src *os.File) error {
	return unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
}
