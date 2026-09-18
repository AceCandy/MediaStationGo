package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

var errImageProxyRequestSetup = errors.New("image proxy request setup failed")
var errImageProxyNonImageContent = errors.New("upstream returned non-image content")

func (p *ImageProxy) PrefetchRemote(ctx context.Context, raw string) error {
	_, _, err := p.Fetch(ctx, raw)
	return err
}

func (p *ImageProxy) RemoveCached(raw string) error {
	if !isHTTPish(raw) {
		return nil
	}
	_, cachePath, failPath, err := p.remoteImageCachePaths(raw)
	if err != nil {
		return nil
	}
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(failPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(directImageFailPath(failPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (p *ImageProxy) RemoveFailed(raw string) error {
	if !isHTTPish(raw) {
		return nil
	}
	_, _, failPath, err := p.remoteImageCachePaths(raw)
	if err != nil {
		return nil
	}
	if err := os.Remove(failPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(directImageFailPath(failPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func directImageFailPath(failPath string) string {
	return failPath + ".direct"
}

// Serve writes the requested image to w. Caller is expected to validate
// the JWT before invoking it.
func (p *ImageProxy) Serve(ctx context.Context, w http.ResponseWriter, r *http.Request, raw string) error {
	if isLocalImagePath(raw) {
		return p.serveLocalImage(w, r, raw)
	}
	return p.serveRemoteImage(ctx, w, r, raw)
}

func (p *ImageProxy) serveLocalImage(w http.ResponseWriter, r *http.Request, raw string) error {
	path := filepath.Clean(raw)
	abs, err := filepath.Abs(path)
	if err != nil || !p.isAllowedLocalPath(abs) {
		servePlaceholder(w)
		return nil
	}
	if !p.serveImageFile(w, r, filepath.Base(abs), abs, imageBrowserCacheControl) {
		servePlaceholder(w)
	}
	return nil
}

func (p *ImageProxy) serveRemoteImage(ctx context.Context, w http.ResponseWriter, r *http.Request, raw string) error {
	u, err := p.validateURL(raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Host)
	key, cachePath, failPath := p.remoteImageCachePathsForValidated(raw)
	forceRefresh := r.URL.Query().Get("refresh") != ""
	if !forceRefresh && hasUsableImageCache(cachePath) && p.serveImageFile(w, r, key, cachePath, imageBrowserCacheControl) {
		return nil
	}
	directOnly := p.useDoubanImageDirect(ctx, host)
	if directOnly {
		failPath = directImageFailPath(failPath)
	}
	p.removeUnusableImageCache(cachePath, failPath)
	if !forceRefresh && p.serveFreshRemoteFailure(w, failPath) {
		return nil
	}
	data, ctype, contentLength, cached, err := p.fetchAndCacheRemoteImage(ctx, raw, host, cachePath, failPath, directOnly)
	if err != nil {
		if forceRefresh && p.serveImageFile(w, r, key, cachePath, imageBrowserCacheControl) {
			return nil
		}
		if errors.Is(err, errImageProxyRequestSetup) {
			servePlaceholder(w)
		} else {
			serveCachedPlaceholder(w)
		}
		return nil
	}
	if cached && p.serveImageFile(w, r, key, cachePath, imageBrowserCacheControl) {
		return nil
	}
	cacheControl := imageBrowserCacheControl
	if o, requested, parseErr := parseImageVariantOptions(r); parseErr != nil {
		http.Error(w, parseErr.Error(), http.StatusBadRequest)
		return nil
	} else if requested && p.variants != nil {
		if p.variants.serve(w, r, fmt.Sprintf("%x", sha256.Sum256(data)), o, func() ([]byte, error) { return data, nil }) {
			return nil
		}
		cacheControl = "no-store"
	}
	w.Header().Set("Content-Type", ctype)
	if contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	modTime := time.Now()
	if stat, err := os.Stat(cachePath); cached && err == nil && stat.Size() > 0 {
		modTime = stat.ModTime()
		w.Header().Set("ETag", imageFileETag(key, stat))
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, key, modTime, bytes.NewReader(data))
	return nil
}

func hasUsableImageCache(cachePath string) bool {
	file, err := os.Open(cachePath) // #nosec G304 -- cachePath is SHA-derived under cacheDir.
	if err != nil {
		return false
	}
	defer file.Close()
	// 热缓存只需要检查文件头，不能为小缩略图反复读取整张原图。
	data, err := io.ReadAll(io.LimitReader(file, 512))
	if err != nil {
		return false
	}
	ctype := detectContentType(data)
	return len(data) > 0 && isImageContentType(ctype) && !isTransparentPlaceholderData(data)
}

func (p *ImageProxy) removeUnusableImageCache(cachePath, failPath string) {
	data, err := os.ReadFile(cachePath) // #nosec G304 -- cachePath is SHA-derived under cacheDir.
	if err != nil {
		return
	}
	ctype := detectContentType(data)
	if len(data) > 0 && isImageContentType(ctype) && !isTransparentPlaceholderData(data) {
		return
	}
	_ = os.Remove(cachePath)
	_ = os.Remove(failPath)
}

func (p *ImageProxy) serveFreshRemoteFailure(w http.ResponseWriter, failPath string) bool {
	if stat, err := os.Stat(failPath); err == nil && time.Since(stat.ModTime()) < imageNegativeCacheTTL {
		serveCachedPlaceholder(w)
		return true
	} else if err == nil {
		_ = os.Remove(failPath)
	}
	return false
}

func (p *ImageProxy) fetchAndCacheRemoteImage(ctx context.Context, raw, host, cachePath, failPath string, directOnly bool) ([]byte, string, string, bool, error) {
	if err := os.MkdirAll(p.cacheDir, 0o750); err != nil {
		p.log.Warn("imageproxy: mkdir failed", zap.String("dir", p.cacheDir), zap.Error(err))
		return nil, "", "", false, errImageProxyRequestSetup
	}
	fetchURL, fetchHost := raw, host
	if isDoubanImageHost(host) && p.apiConfig != nil {
		if resolved, resolveErr := p.apiConfig.Resolve(ctx, "douban"); resolveErr == nil && resolved.Enabled {
			if projected := projectDoubanArtworkURL(raw, resolved.BaseURL); projected != "" && projected != raw {
				if target, validateErr := p.validateURL(projected); validateErr == nil {
					fetchURL, fetchHost = projected, strings.ToLower(target.Host)
				}
			}
		}
	}
	data, ctype, contentLength, err := p.fetchRemoteImageUncached(ctx, fetchURL, fetchHost, directOnly)
	if err != nil && fetchURL != raw {
		data, ctype, contentLength, err = p.fetchRemoteImageUncached(ctx, raw, host, directOnly)
	}
	if err == nil {
		cached := p.writeImageCache(cachePath, failPath, "img-*.tmp", data)
		return data, ctype, contentLength, cached, nil
	}
	if isRemoteImageHTTPStatus(err, http.StatusNotFound) {
		p.markImageFetchFailed(failPath)
	}
	return nil, "", "", false, err
}

func (p *ImageProxy) fetchRemoteImageDirect(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := p.validateURL(raw)
	if err != nil {
		return nil, "", err
	}
	data, ctype, _, err := p.fetchRemoteImageUncached(ctx, raw, strings.ToLower(u.Host), false)
	return data, ctype, err
}

func (p *ImageProxy) fetchRemoteImageUncached(ctx context.Context, raw, host string, directOnly bool) ([]byte, string, string, error) {
	key := remoteImageFetchKey{raw, directOnly}
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", "", err
		}
		p.fetchMu.Lock()
		if active := p.fetches[key]; active != nil {
			p.fetchMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, "", "", ctx.Err()
			case <-active.done:
				// 首个请求取消不应永久中断仍然有效的等待者。
				if active.canceled {
					continue
				}
				return active.data, active.contentType, active.contentLength, active.err
			}
		}
		active := &remoteImageFetch{done: make(chan struct{})}
		if p.fetches == nil {
			p.fetches = make(map[remoteImageFetchKey]*remoteImageFetch)
		}
		p.fetches[key] = active
		p.fetchMu.Unlock()
		active.data, active.contentType, active.contentLength, active.err = p.fetchRemoteImage(ctx, raw, host, directOnly)
		active.canceled = ctx.Err() != nil
		p.fetchMu.Lock()
		delete(p.fetches, key)
		close(active.done)
		p.fetchMu.Unlock()
		return active.data, active.contentType, active.contentLength, active.err
	}
}

type remoteImageFetchKey struct {
	url        string
	directOnly bool
}

type remoteImageFetch struct {
	done                       chan struct{}
	data                       []byte
	contentType, contentLength string
	err                        error
	canceled                   bool
}

func (p *ImageProxy) fetchRemoteImage(ctx context.Context, raw, host string, directOnly bool) ([]byte, string, string, error) {
	var lastErr error
	for _, candidate := range p.remoteImageFetchClients(directOnly) {
		data, ctype, contentLength, err := p.fetchRemoteImageOnce(ctx, raw, host, candidate)
		if err == nil {
			return data, ctype, contentLength, nil
		}
		if ctx.Err() != nil {
			return nil, "", "", ctx.Err()
		}
		if errors.Is(err, errImageProxyRequestSetup) {
			return nil, "", "", err
		}
		if isRemoteImageHTTPStatus(err, http.StatusNotFound) {
			return nil, "", "", err
		}
		lastErr = err
	}
	if p.canUseExternalImageFallback(directOnly, host) {
		data, ctype, contentLength, err := fetchRemoteImageWithCurl(ctx, raw, host)
		if err == nil {
			return data, ctype, contentLength, nil
		}
		p.log.Warn("imageproxy: curl fallback failed", zap.String("host", host), zap.Error(err))
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("upstream image fetch failed")
	}
	return nil, "", "", lastErr
}

// Fetch pulls a remote image and returns bytes plus Content-Type using cache.
func (p *ImageProxy) Fetch(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := p.validateURL(raw)
	if err != nil {
		return nil, "", err
	}
	host := strings.ToLower(u.Host)
	_, cachePath, failPath := p.remoteImageCachePathsForValidated(raw)
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 { // #nosec G304 -- cachePath is SHA-derived under cacheDir.
		ctype := detectContentType(data)
		if isImageContentType(ctype) && !isTransparentPlaceholderData(data) {
			return data, ctype, nil
		}
	}
	directOnly := p.useDoubanImageDirect(ctx, host)
	if directOnly {
		failPath = directImageFailPath(failPath)
	}
	p.removeUnusableImageCache(cachePath, failPath)
	if stat, err := os.Stat(failPath); err == nil && time.Since(stat.ModTime()) < imageNegativeCacheTTL {
		return nil, "", errors.New("recent image fetch failure")
	} else if err == nil {
		_ = os.Remove(failPath)
	}
	data, ctype, _, _, err := p.fetchAndCacheRemoteImage(ctx, raw, host, cachePath, failPath, directOnly)
	return data, ctype, err
}

func (p *ImageProxy) writeImageCache(cachePath, failPath, pattern string, data []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	tmp, tmpErr := os.CreateTemp(p.cacheDir, pattern)
	if tmpErr != nil {
		return false
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return false
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return false
	}
	if err := os.Rename(tmp.Name(), cachePath); err != nil {
		_ = os.Remove(tmp.Name())
		return false
	}
	_ = os.Remove(failPath)
	return true
}
