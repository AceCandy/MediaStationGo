package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const maxArtworkBytes = 32 << 20

var ErrArtworkNotFound = errors.New("artwork not found")

// ArtworkStore 将选中的原图保存到 DataDir，并维护共享元数据的图片关联。
type ArtworkStore struct {
	root       string
	repo       *repository.ArtworkRepository
	imageProxy *ImageProxy
	mu         sync.Mutex
}

func NewArtworkStore(cfg *config.Config, repo *repository.ArtworkRepository, imageProxy *ImageProxy) *ArtworkStore {
	return &ArtworkStore{
		root:       filepath.Join(cfg.App.DataDir, "artwork"),
		repo:       repo,
		imageProxy: imageProxy,
	}
}

func (s *ArtworkStore) ImportRemote(ctx context.Context, metadataID, artworkType, provider, sourceURL string) (*model.ArtworkAsset, error) {
	if s.imageProxy == nil {
		return nil, errors.New("image proxy is unavailable")
	}
	data, mimeType, err := s.imageProxy.Fetch(ctx, sourceURL)
	if err != nil {
		return nil, err
	}
	return s.save(ctx, metadataID, artworkType, provider, sourceURL, data, mimeType)
}

func (s *ArtworkStore) ImportLocal(ctx context.Context, metadataID, artworkType, sourcePath string) (*model.ArtworkAsset, error) {
	if s.imageProxy == nil {
		return nil, errors.New("image proxy is unavailable")
	}
	abs, err := filepath.Abs(filepath.Clean(sourcePath))
	if err != nil || !s.imageProxy.isAllowedLocalPath(abs) {
		return nil, errors.New("artwork path is outside allowed roots")
	}
	file, err := os.Open(abs) // #nosec G304 -- path is restricted to configured application or library roots.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxArtworkBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtworkBytes {
		return nil, errors.New("artwork exceeds 32 MiB limit")
	}
	return s.save(ctx, metadataID, artworkType, "local_nfo", abs, data, "")
}

func (s *ArtworkStore) ImportCloudCached(ctx context.Context, metadataID, artworkType, sourceURL string) (*model.ArtworkAsset, error) {
	if s.imageProxy == nil {
		return nil, errors.New("image proxy is unavailable")
	}
	typ, ref, ok := ParseCloudArtworkURL(sourceURL)
	if !ok {
		return nil, errors.New("invalid cloud artwork URL")
	}
	_, cachePath, _ := s.imageProxy.cloudImageCachePaths(typ + ":" + ref)
	data, err := os.ReadFile(cachePath) // #nosec G304 -- cachePath is SHA-derived under the configured cache directory.
	if err != nil {
		return nil, err
	}
	return s.save(ctx, metadataID, artworkType, "local_nfo", sourceURL, data, "")
}

func (s *ArtworkStore) save(ctx context.Context, metadataID, artworkType, provider, sourceURL string, data []byte, _ string) (*model.ArtworkAsset, error) {
	if strings.TrimSpace(metadataID) == "" {
		return nil, errors.New("metadata id is required")
	}
	if !validArtworkType(artworkType) {
		return nil, errors.New("invalid artwork type")
	}
	if len(data) == 0 || len(data) > maxArtworkBytes {
		return nil, errors.New("invalid artwork size")
	}
	mimeType, ok := validImageContentType(data)
	if !ok {
		return nil, errImageProxyNonImageContent
	}
	width, height, err := artworkDimensions(data, mimeType)
	if err != nil {
		return nil, err
	}
	ext, err := artworkExtension(mimeType)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	storageKey := filepath.ToSlash(filepath.Join("sha256", hash[:2], hash[2:4], hash+ext))
	path, err := s.pathForStorageKey(storageKey)
	if err != nil {
		return nil, err
	}
	if err := s.write(path, data); err != nil {
		return nil, err
	}
	asset := &model.ArtworkAsset{
		SHA256: hash, StorageKey: storageKey, MimeType: mimeType,
		Width: width, Height: height, SizeBytes: int64(len(data)),
	}
	if s.repo == nil {
		return nil, errors.New("artwork repository is unavailable")
	}
	return s.repo.SaveSelection(ctx, metadataID, artworkType, provider, sourceURL, asset)
}

func (s *ArtworkStore) write(path string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, err := os.Stat(path); err == nil && stat.Size() == int64(len(data)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".artwork-*.tmp")
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

func (s *ArtworkStore) Serve(ctx context.Context, w http.ResponseWriter, r *http.Request, assetID string) error {
	if s.repo == nil {
		return ErrArtworkNotFound
	}
	asset, err := s.repo.FindAssetByID(ctx, assetID)
	if err != nil {
		return err
	}
	if asset == nil {
		return ErrArtworkNotFound
	}
	path, err := s.pathForStorageKey(asset.StorageKey)
	if err != nil {
		return ErrArtworkNotFound
	}
	if !serveImageFile(w, r, asset.ID, path, imageBrowserCacheControl) {
		return ErrArtworkNotFound
	}
	return nil
}

func ArtworkURL(assetID string) string {
	if strings.TrimSpace(assetID) == "" {
		return ""
	}
	return "/api/artwork/" + url.PathEscape(assetID)
}

func (s *ArtworkStore) pathForStorageKey(storageKey string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(storageKey))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid artwork storage key")
	}
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil || !strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator)) {
		return "", errors.New("invalid artwork storage path")
	}
	return pathAbs, nil
}

func validArtworkType(value string) bool {
	switch value {
	case model.ArtworkTypePoster, model.ArtworkTypeBackdrop, model.ArtworkTypeStill:
		return true
	default:
		return false
	}
}

func artworkExtension(mimeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	case "image/bmp", "image/x-ms-bmp":
		return ".bmp", nil
	default:
		return "", errors.New("unsupported artwork format")
	}
}

func artworkDimensions(data []byte, mimeType string) (int, int, error) {
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
		return cfg.Width, cfg.Height, nil
	}
	switch mimeType {
	case "image/webp":
		return webPDimensions(data)
	case "image/bmp", "image/x-ms-bmp":
		if len(data) >= 26 {
			width := int(int32(binary.LittleEndian.Uint32(data[18:22])))
			height := int(int32(binary.LittleEndian.Uint32(data[22:26])))
			if height < 0 {
				height = -height
			}
			if width > 0 && height > 0 {
				return width, height, nil
			}
		}
	}
	return 0, 0, errors.New("cannot decode artwork dimensions")
}

func webPDimensions(data []byte) (int, int, error) {
	if len(data) < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, errors.New("invalid WebP image")
	}
	switch string(data[12:16]) {
	case "VP8X":
		width := 1 + int(data[24]) + int(data[25])<<8 + int(data[26])<<16
		height := 1 + int(data[27]) + int(data[28])<<8 + int(data[29])<<16
		return width, height, nil
	case "VP8L":
		if data[20] != 0x2f {
			break
		}
		width := 1 + int(data[21]) + int(data[22]&0x3f)<<8
		height := 1 + int(data[22]>>6) + int(data[23])<<2 + int(data[24]&0x0f)<<10
		return width, height, nil
	case "VP8 ":
		if data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			break
		}
		width := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
		if width > 0 && height > 0 {
			return width, height, nil
		}
	}
	return 0, 0, errors.New("cannot decode WebP dimensions")
}
