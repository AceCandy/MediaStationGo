package service

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTransferCrossDevice(t *testing.T) {
	other, err := os.MkdirTemp("/dev/shm", "msgo-transfer-test-")
	if err != nil {
		t.Skip("separate temporary filesystem unavailable")
	}
	t.Cleanup(func() { _ = os.RemoveAll(other) })
	src := writeTemp(t, t.TempDir(), "source", "payload")
	dst := filepath.Join(other, "target")
	if err := renameNoReplace(src, dst); !errors.Is(err, syscall.EXDEV) {
		if err == nil {
			t.Skip("temporary directories share a filesystem")
		}
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "payload" {
		t.Fatalf("copy failed: %q %v", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source remains: %v", err)
	}
	srcDir := t.TempDir()
	writeTemp(t, srcDir, "file", "tree")
	dstDir := filepath.Join(other, "tree")
	if err := transferDirectory(srcDir, dstDir, TransferMove); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dstDir, "file")); err != nil || string(data) != "tree" {
		t.Fatalf("tree failed: %q %v", data, err)
	}
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Fatalf("directory source remains: %v", err)
	}
}
