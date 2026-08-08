package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
		StorageKey: filepath.ToSlash(filepath.Join("sha256", hash[:2], hash[2:4], hash+ext)),
		MimeType:   mimeType,
		Width:      width,
		Height:     height,
		SizeBytes:  int64(len(data)),
	}, nil
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
	if _, err := tmp.Write(data); err != nil {
		return err
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
