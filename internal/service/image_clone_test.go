package service

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyStoredImagePreservesValidatedBytes(t *testing.T) {
	for _, sourceState := range []string{"same", "changed", "missing"} {
		t.Run(sourceState, func(t *testing.T) {
			dir := t.TempDir()
			source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
			validated := []byte("validated image bytes")
			current := append([]byte{}, validated...)
			if sourceState == "changed" {
				current[0] ^= 1
			}
			if sourceState != "missing" {
				if err := os.WriteFile(source, current, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := copyStoredImage(source, target, validated, ".test-*"); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, validated) {
				t.Fatalf("wrong target bytes: %v", err)
			}
			if sourceState != "missing" {
				srcInfo, _ := os.Stat(source)
				dstInfo, _ := os.Stat(target)
				if os.SameFile(srcInfo, dstInfo) {
					t.Fatal("source and target share inode")
				}
				if err := os.WriteFile(target, []byte("changed target"), 0600); err != nil {
					t.Fatal(err)
				}
				if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, current) {
					t.Fatal("writing target changed source")
				}
			}
			if leftovers, _ := filepath.Glob(filepath.Join(dir, ".test-*")); len(leftovers) != 0 {
				t.Fatal("temporary image left behind")
			}
		})
	}
}

func TestImageCloneFilesystemSupport(t *testing.T) {
	// 在工作区同一文件系统验证真实克隆；临时目录始终删除。
	dir, err := os.MkdirTemp(".", ".image-clone-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	source, err := os.Create(filepath.Join(dir, "source"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.Create(filepath.Join(dir, "target"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	data := bytes.Repeat([]byte("image content"), 1024)
	if _, err := source.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := cloneImageFile(target, source); err != nil {
		t.Skipf("filesystem cloning unavailable: %v", err)
	}
	if got, err := os.ReadFile(target.Name()); err != nil || !bytes.Equal(got, data) {
		t.Fatal("clone differs")
	}
	if _, err := target.WriteAt([]byte("different"), 0); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(source.Name()); err != nil || !bytes.Equal(got, data) {
		t.Fatal("clone write changed source")
	}
	if err := copyStoredImage(source.Name(), filepath.Join(dir, "atomic-copy"), data, ".test-*"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "atomic-copy")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("atomic clone differs")
	}
	// 同长度的源内容变化也必须回退到已验证字节。
	if _, err := source.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	if err := copyStoredImage(source.Name(), filepath.Join(dir, "fallback-copy"), data, ".test-*"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "fallback-copy")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("clone mismatch did not restore validated bytes")
	}
}

func TestCopyStoredImageCleansFailedTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyStoredImage(filepath.Join(dir, "missing"), target, []byte("bytes"), ".test-*"); err == nil {
		t.Fatal("rename failure ignored")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".test-*")); len(leftovers) != 0 {
		t.Fatal("failed copy left temporary file")
	}
}
