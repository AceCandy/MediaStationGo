//go:build windows

package service

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func fileIdentity(path string) (string, bool) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	handle, err := windows.CreateFile(
		ptr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", false
	}
	if info.NumberOfLinks < 2 {
		return "", false
	}
	fileIndex := (uint64(info.FileIndexHigh) << 32) | uint64(info.FileIndexLow)
	return fmt.Sprintf("%d:%d", info.VolumeSerialNumber, fileIndex), true
}

func fileIdentityFromInfo(path string, _ os.FileInfo) (string, bool) { return fileIdentity(path) }
