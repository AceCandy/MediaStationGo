package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
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
	path, err := s.libraryCoverPath(libraryID, stored.SHA256)
	if err != nil {
		return nil, err
	}
	if err := writeStoredImage(path, data, ".library-cover-*.tmp"); err != nil {
		return nil, err
	}
	oldVersion, hadLocalCover := libraryCoverVersion(lib.CoverURL, libraryID)
	coverURL := libraryCoverURL(libraryID) + "?v=" + stored.SHA256
	if err := s.UpdateLibraryCover(ctx, libraryID, coverURL); err != nil {
		if !hadLocalCover || oldVersion != stored.SHA256 {
			_ = os.Remove(path)
		}
		return nil, err
	}
	if hadLocalCover && oldVersion != stored.SHA256 {
		if oldPath, pathErr := s.libraryCoverPath(libraryID, oldVersion); pathErr == nil {
			_ = os.Remove(oldPath)
		}
	}
	return s.repo.Library.FindByID(ctx, libraryID)
}

// ServeLibraryCover 返回媒体库当前使用的本地上传封面。
func (s *MediaService) ServeLibraryCover(ctx context.Context, w http.ResponseWriter, r *http.Request, libraryID string) error {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return err
	}
	if lib == nil {
		return ErrLibraryCoverNotFound
	}
	version, ok := libraryCoverVersion(lib.CoverURL, libraryID)
	if !ok {
		return ErrLibraryCoverNotFound
	}
	path, err := s.libraryCoverPath(libraryID, version)
	if err != nil {
		return err
	}
	if !serveImageFile(w, r, libraryID, path, imageBrowserCacheControl) {
		return ErrLibraryCoverNotFound
	}
	return nil
}

// ClearLibraryCover 清除媒体库封面及对应的本地上传文件。
func (s *MediaService) ClearLibraryCover(ctx context.Context, libraryID string) (*model.Library, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, ErrLibraryNotFound
	}
	version, hadLocalCover := libraryCoverVersion(lib.CoverURL, libraryID)
	if err := s.UpdateLibraryCover(ctx, libraryID, ""); err != nil {
		return nil, err
	}
	if hadLocalCover {
		path, pathErr := s.libraryCoverPath(libraryID, version)
		if pathErr != nil {
			return nil, pathErr
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, errors.Join(removeErr, s.UpdateLibraryCover(ctx, libraryID, lib.CoverURL))
		}
	}
	return s.repo.Library.FindByID(ctx, libraryID)
}

func (s *MediaService) libraryCoverPath(libraryID, version string) (string, error) {
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.App.DataDir) == "" {
		return "", errors.New("application data directory is unavailable")
	}
	if len(version) != sha256.Size*2 {
		return "", errors.New("invalid library cover version")
	}
	if _, err := hex.DecodeString(version); err != nil {
		return "", errors.New("invalid library cover version")
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
	if len(version) != sha256.Size*2 {
		return "", false
	}
	_, err = hex.DecodeString(version)
	return version, err == nil
}
