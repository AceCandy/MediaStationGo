package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type storedImage struct {
	SHA256     string
	StorageKey string
	MimeType   string
	Width      int
	Height     int
	SizeBytes  int64
}

func prepareStoredImage(data []byte) (storedImage, error) {
	if len(data) == 0 || len(data) > maxArtworkBytes {
		return storedImage{}, errors.New("invalid artwork size")
	}
	mimeType, ok := validImageContentType(data)
	if !ok {
		return storedImage{}, errImageProxyNonImageContent
	}
	width, height, err := artworkDimensions(data, mimeType)
	if err != nil {
		return storedImage{}, err
	}
	ext, err := artworkExtension(mimeType)
	if err != nil {
		return storedImage{}, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	return storedImage{
		SHA256:     hash,
		StorageKey: filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], hash[4:6], hash+ext)),
		MimeType:   mimeType,
		Width:      width,
		Height:     height,
		SizeBytes:  int64(len(data)),
	}, nil
}

// catalogImageKey 按来源隔离原图；本地导入与手工资料共用 local 体系。
func catalogImageKey(provider, category, key string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "", "manual", "local_nfo", "nfo":
		provider = "local"
	}
	for _, c := range provider {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return "", errors.New("invalid image provider")
		}
	}
	if category != "artwork" && category != "people" {
		return "", errors.New("invalid image category")
	}
	return filepath.ToSlash(filepath.Join("catalogs", provider, category, key)), nil
}

// managedImagePath 兼容尚未迁移的旧键，新键只能落在指定类别的资料目录内。
func managedImagePath(dataDir, category, key string) (string, error) {
	if strings.HasPrefix(key, "catalogs/") {
		parts := strings.Split(key, "/")
		if len(parts) != 7 || parts[2] != category {
			return "", errors.New("invalid catalog image key")
		}
		return storedImagePath(dataDir, key)
	}
	return storedImagePath(filepath.Join(dataDir, category), key)
}

func storedImagePath(root, storageKey string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(storageKey)))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid image storage key")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil || (pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator))) {
		return "", errors.New("invalid image storage path")
	}
	return pathAbs, nil
}

func writeStoredImage(path string, data []byte, pattern string) error {
	return copyStoredImage("", path, data, pattern)
}

// copyStoredImage 优先克隆已经校验的源文件；不支持或源内容变化时复制校验过的字节。
// 目标始终先写临时文件并同步落盘，随后原子替换，不与源文件共享 inode。
func copyStoredImage(source, path string, data []byte, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	cloned := false
	if source != "" {
		if src, err := os.Open(source); err == nil {
			err = cloneImageFile(tmp, src)
			_ = src.Close()
			if err == nil {
				if stat, err := tmp.Stat(); err == nil && stat.Size() == int64(len(data)) {
					actual := make([]byte, len(data))
					_, err := tmp.ReadAt(actual, 0)
					cloned = err == nil && bytes.Equal(actual, data)
				}
			}
		}
	}
	if !cloned {
		if source != "" {
			if err := tmp.Truncate(0); err != nil {
				return err
			}
			if _, err := tmp.Seek(0, io.SeekStart); err != nil {
				return err
			}
		}
		if _, err := tmp.Write(data); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}
