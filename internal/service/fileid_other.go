//go:build !unix && !windows

package service

import "os"

// fileIdentity is a no-op on platforms without inode semantics; dedup by
// hardlink identity is simply disabled there.
func fileIdentity(path string) (string, bool) {
	return "", false
}

func fileIdentityFromInfo(path string, _ os.FileInfo) (string, bool) { return fileIdentity(path) }
