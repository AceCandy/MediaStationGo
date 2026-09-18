package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

var ErrPeopleImageNotFound = errors.New("people image not found")

type peopleImageSource struct {
	Key    string
	Failed bool
}

// PeopleImageStore 将人物头像持久化到 DataDir/people，并只从该目录对外服务。
type PeopleImageStore struct {
	root       string
	repo       *repository.PersonRepository
	imageProxy *ImageProxy
	mu         sync.Mutex
	sources    *RuntimeCacheService
}

func NewPeopleImageStore(cfg *config.Config, repo *repository.PersonRepository, imageProxy *ImageProxy) *PeopleImageStore {
	return &PeopleImageStore{
		root:       filepath.Join(cfg.App.DataDir, "people"),
		repo:       repo,
		imageProxy: imageProxy,
		sources:    NewRuntimeCacheService(nil, nil),
	}
}

// ImportCached 复用近期成功导入的远程头像；文件缺失或损坏时重新下载。
func (s *PeopleImageStore) ImportCached(ctx context.Context, source string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	source = strings.TrimSpace(source)
	if s != nil && isHTTPish(source) {
		var cached peopleImageSource
		if s.sources.GetJSON(ctx, source, &cached) {
			if cached.Failed {
				return "", errors.New("recent people image import failure; retry after cooldown")
			}
			if s.hasUsableImage(cached.Key) {
				return cached.Key, nil
			}
		}
	}
	key, err := s.Import(ctx, source)
	if s != nil && isHTTPish(source) && err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) {
		s.sources.SetJSON(ctx, source, peopleImageSource{Failed: true}, time.Minute)
	}
	return key, err
}

// Import fetches and stores one profile image, returning its content-hash key.
func (s *PeopleImageStore) Import(ctx context.Context, source string) (string, error) {
	if s == nil {
		return "", ErrPeopleImageNotFound
	}
	source = strings.TrimSpace(source)
	data, err := s.readSource(ctx, source)
	if err != nil {
		return "", err
	}
	stored, err := prepareStoredImage(data)
	if err != nil {
		return "", err
	}
	path, err := storedImagePath(s.root, stored.StorageKey)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	matches := false
	if stat, statErr := os.Stat(path); statErr == nil && stat.Size() == stored.SizeBytes {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			matches = true
		}
	}
	if !matches {
		if err := writeStoredImage(path, data, ".people-*.tmp"); err != nil {
			return "", err
		}
	}
	if isHTTPish(source) {
		// 仅缓存成功来源，重启或过期后重新下载一次；图片本身仍独立持久保存。
		s.sources.SetJSON(ctx, source, peopleImageSource{Key: stored.StorageKey}, 24*time.Hour)
	}
	return stored.StorageKey, nil
}

func (s *PeopleImageStore) readSource(ctx context.Context, source string) ([]byte, error) {
	if source == "" {
		return nil, ErrPeopleImageNotFound
	}
	if isHTTPish(source) {
		if s.imageProxy == nil {
			return nil, errors.New("image proxy is unavailable")
		}
		data, _, err := s.imageProxy.fetchRemoteImageDirect(ctx, source)
		return data, err
	}
	if !isLocalImagePath(source) || s.imageProxy == nil {
		return nil, errors.New("unsupported people image source")
	}
	abs, err := filepath.Abs(filepath.Clean(source))
	if err != nil || !s.imageProxy.isAllowedLocalPath(abs) {
		return nil, errors.New("people image path is outside allowed roots")
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
		return nil, errors.New("people image exceeds 32 MiB limit")
	}
	return data, nil
}

// ServePerson reports whether id is a Person and serves only its local image.
// Missing local bytes return an error so the public Emby response can use its
// placeholder without performing request-time network I/O.
func (s *PeopleImageStore) ServePerson(ctx context.Context, w http.ResponseWriter, r *http.Request, id string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, nil
	}
	person, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if isMissingPeopleTable(err) {
			return false, nil
		}
		return true, err
	}
	if person == nil {
		return false, nil
	}
	key := strings.TrimSpace(person.ProfileImageKey)
	if key == "" || !s.hasUsableImage(key) {
		return true, ErrPeopleImageNotFound
	}
	path, err := storedImagePath(s.root, key)
	if err != nil || !s.imageProxy.serveImageFile(w, r, key, path, imageBrowserCacheControl) {
		return true, ErrPeopleImageNotFound
	}
	return true, nil
}

func (s *PeopleImageStore) hasUsableImage(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	path, err := storedImagePath(s.root, key)
	if err != nil {
		return false
	}
	file, err := os.Open(path) // #nosec G304 -- path is derived from a validated storage key under the people root.
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.IsDir() || stat.Size() <= 0 || stat.Size() > maxArtworkBytes {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxArtworkBytes+1))
	if err != nil || len(data) > maxArtworkBytes {
		return false
	}
	stored, err := prepareStoredImage(data)
	return err == nil && stored.StorageKey == key
}
