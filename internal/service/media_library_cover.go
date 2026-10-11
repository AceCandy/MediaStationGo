package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var (
	ErrInvalidLibraryCover  = errors.New("invalid library cover")
	ErrLibraryCoverNotFound = errors.New("library cover not found")
	ErrLibraryNotFound      = errors.New("library not found")
)

const MaxLibraryCoverRequestBytes int64 = maxArtworkBytes + 1<<20

// SaveLibraryCover 校验并保存用户上传的媒体库封面。
func (s *MediaService) SaveLibraryCover(ctx context.Context, libraryID string, source io.Reader) (*model.Library, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, ErrLibraryNotFound
	}
	data, err := io.ReadAll(io.LimitReader(source, maxArtworkBytes+1))
	if err != nil {
		return nil, err
	}
	stored, err := prepareStoredImage(data)
	if err != nil {
		return nil, errors.Join(ErrInvalidLibraryCover, err)
	}
	path, err := s.libraryCoverPath(libraryID, filepath.Base(stored.StorageKey))
	if err != nil {
		return nil, err
	}
	if err := writeStoredImage(path, data, ".library-cover-*.tmp"); err != nil {
		return nil, err
	}
	coverURL := libraryCoverURL(libraryID) + "?v=" + filepath.Base(stored.StorageKey)
	if err := s.UpdateLibraryCover(ctx, libraryID, coverURL); err != nil {
		return nil, err
	}
	return s.repo.Library.FindByID(ctx, libraryID)
}

// ServeLibraryCover 返回媒体库当前使用的本地上传封面。
func (s *MediaService) ServeLibraryCover(ctx context.Context, w http.ResponseWriter, r *http.Request, libraryID string) error {
	coverURL, err := s.repo.Library.FindCoverURL(ctx, libraryID)
	if err != nil {
		return err
	}
	version, ok := libraryCoverVersion(coverURL, libraryID)
	if !ok {
		return ErrLibraryCoverNotFound
	}
	path, err := s.libraryCoverPath(libraryID, version)
	if err != nil {
		return err
	}
	if !s.variants.serveFile(w, r, libraryID, path, imageBrowserCacheControl) {
		return ErrLibraryCoverNotFound
	}
	return nil
}

// ClearLibraryCover 清除封面引用；内容寻址的原图可能被其他库或本地资料复用。
func (s *MediaService) ClearLibraryCover(ctx context.Context, libraryID string) (*model.Library, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, ErrLibraryNotFound
	}
	if err := s.UpdateLibraryCover(ctx, libraryID, ""); err != nil {
		return nil, err
	}
	return s.repo.Library.FindByID(ctx, libraryID)
}

func (s *MediaService) libraryCoverPath(libraryID, version string) (string, error) {
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.App.DataDir) == "" {
		return "", errors.New("application data directory is unavailable")
	}
	hash := strings.TrimSuffix(version, filepath.Ext(version))
	if len(hash) != sha256.Size*2 {
		return "", errors.New("invalid library cover version")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", errors.New("invalid library cover version")
	}
	if ext := filepath.Ext(version); ext != "" {
		switch ext {
		case ".jpg", ".png", ".webp", ".gif", ".bmp":
		default:
			return "", errors.New("invalid library cover format")
		}
		key, err := catalogImageKey("local", "artwork", filepath.Join(hash[:2], hash[2:4], hash[4:6], version))
		if err != nil {
			return "", err
		}
		return storedImagePath(s.cfg.App.DataDir, key)
	}
	sum := sha256.Sum256([]byte(libraryID))
	return filepath.Join(s.cfg.App.DataDir, "library-covers", hex.EncodeToString(sum[:]), strings.ToLower(version)), nil
}

func libraryCoverURL(libraryID string) string {
	return "/api/libraries/" + url.PathEscape(libraryID) + "/cover"
}

func libraryCoverVersion(rawURL, libraryID string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Path != libraryCoverURL(libraryID) {
		return "", false
	}
	version := strings.ToLower(parsed.Query().Get("v"))
	hash := strings.TrimSuffix(version, filepath.Ext(version))
	if len(hash) != sha256.Size*2 {
		return "", false
	}
	_, err = hex.DecodeString(hash)
	return version, err == nil
}
