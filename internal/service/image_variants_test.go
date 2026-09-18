package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"go.uber.org/zap"
)

func variantPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.SetNRGBA(width/2, height/2, color.NRGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestImageVariantDimensionsAndFormats(t *testing.T) {
	for _, tc := range []struct {
		query        string
		sw, sh, w, h int
		format       string
	}{
		{"maxWidth=40", 120, 60, 40, 20, "webp"},
		{"Width=40&Height=40&Format=PNG", 60, 120, 20, 40, "png"},
		{"maxWidth=4096&format=jpg", 60, 120, 60, 120, "jpeg"},
		{"fillWidth=30&fillHeight=40", 120, 60, 30, 40, "webp"},
		{"fillWidth=30&fillHeight=40", 60, 120, 30, 40, "webp"},
		{"maxHeight=20&quality=1", 60, 120, 10, 20, "webp"},
		{"format=png", 5000, 1, 4096, 1, "png"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			o, requested, err := parseImageVariantOptions(httptest.NewRequest("GET", "/?"+tc.query, nil))
			if err != nil || !requested {
				t.Fatalf("parse: %v", err)
			}
			data, err := encodeImageVariant(variantPNG(t, tc.sw, tc.sh), o)
			if err != nil {
				t.Fatal(err)
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || format != tc.format || cfg.Width != tc.w || cfg.Height != tc.h {
				t.Fatalf("got %v %s %v", cfg, format, err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, alpha := decoded.At(0, 0).RGBA()
			if tc.format != "jpeg" && alpha != 0 {
				t.Fatal("transparency lost")
			}
		})
	}
}

func TestImageVariantHTTPAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.png")
	original := variantPNG(t, 120, 60)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	v := newImageVariants(dir)
	serve := func(method, query, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/?"+query, nil)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		if !v.serveFile(w, r, "source", path, imageBrowserCacheControl) {
			t.Fatal("not served")
		}
		return w
	}
	if got := serve("GET", "", ""); !bytes.Equal(got.Body.Bytes(), original) {
		t.Fatal("original changed")
	}
	first := serve("GET", "maxWidth=40", "")
	if first.Code != 200 || first.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("response: %v", first)
	}
	etag := first.Header().Get("ETag")
	if got := serve("GET", "maxWidth=40", etag); got.Code != 304 || got.Body.Len() != 0 {
		t.Fatal("conditional GET")
	}
	if got := serve("HEAD", "maxWidth=40", ""); got.Body.Len() != 0 || got.Header().Get("Content-Length") != first.Header().Get("Content-Length") {
		t.Fatal("HEAD")
	}
	files, _ := filepath.Glob(filepath.Join(v.dir, "*", "*.webp"))
	if len(files) != 1 {
		t.Fatalf("cache files: %v", files)
	}
	before, _ := os.Stat(files[0])
	serve("GET", "maxWidth=40", "")
	after, _ := os.Stat(files[0])
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("cache regenerated")
	}
	updated := time.Now().Add(time.Second)
	if err := os.Chtimes(path, updated, updated); err != nil {
		t.Fatal(err)
	}
	if got := serve("GET", "maxWidth=40", etag); got.Code != 200 || got.Header().Get("ETag") == etag {
		t.Fatal("source update did not invalidate")
	}
	for _, query := range []string{"width=0", "width=-1", "width=4097", "quality=101", "quality=0", "format=avif", "width=2&Width=3", "width=1&width=1", "fillWidth=20", "height=bad"} {
		if got := serve("GET", query, ""); got.Code != 400 {
			t.Fatalf("%s: %d", query, got.Code)
		}
	}
}

func TestImageVariantFallbackAndCancellation(t *testing.T) {
	oversized := variantPNG(t, 1, 1)
	binary.BigEndian.PutUint32(oversized[16:20], 32_000_001)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if _, err := encodeImageVariant(oversized, imageVariantOptions{Quality: 80, Format: "webp"}); err == nil {
		t.Fatal("pixel limit not enforced")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	var gifBytes bytes.Buffer
	if err := gif.Encode(&gifBytes, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{gifBytes.Bytes(), testJPEG} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if !newImageVariants(dir).serveFile(w, httptest.NewRequest("GET", "/?width=1", nil), "source", path, imageBrowserCacheControl) {
			t.Fatal("fallback missing")
		}
		if !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("fallback changed or cached")
		}
	}
	if err := os.WriteFile(path, variantPNG(t, 20, 10), 0600); err != nil {
		t.Fatal(err)
	}
	v := newImageVariants(path) // 普通文件不能创建缓存子目录。
	w := httptest.NewRecorder()
	v.serveFile(w, httptest.NewRequest("GET", "/?width=1", nil), "source", path, imageBrowserCacheControl)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("write failure fallback")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w = httptest.NewRecorder()
	v.serveFile(w, httptest.NewRequest("GET", "/?width=1", nil).WithContext(ctx), "source", path, imageBrowserCacheControl)
	if w.Body.Len() != 0 {
		t.Fatal("canceled request wrote body")
	}
}

func TestImageVariantConcurrentGeneration(t *testing.T) {
	v := newImageVariants(t.TempDir())
	data := variantPNG(t, 120, 60)
	var reads atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			if !v.serve(w, httptest.NewRequest("GET", "/", nil), "same", imageVariantOptions{MaxWidth: 40, Quality: 80, Format: "webp"}, func() ([]byte, error) { reads.Add(1); return data, nil }) {
				t.Error("not served")
			}
			if w.Header().Get("Content-Type") != "image/webp" {
				t.Error("not webp")
			}
		}()
	}
	wg.Wait()
	if reads.Load() != 1 {
		t.Fatalf("generated %d times", reads.Load())
	}
}

func TestImageVariantRemoteColdAndHot(t *testing.T) {
	data := variantPNG(t, 120, 60)
	var requests atomic.Int32
	p := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: t.TempDir()}}, zap.NewNop())
	p.client = &http.Client{Transport: imageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}
	for range 2 {
		w := httptest.NewRecorder()
		if err := p.Serve(t.Context(), w, httptest.NewRequest("GET", "/api/img?maxWidth=40", nil), "https://image.tmdb.org/poster"); err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
		if err != nil || format != "webp" || cfg.Width != 40 {
			t.Fatalf("response: %v %s %v", cfg, format, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("upstream requests: %d", requests.Load())
	}
}

func TestImageVariantOrientationAndAnimation(t *testing.T) {
	// Little-endian TIFF，IFD0 的 Orientation=6（顺时针 90 度）。
	exif := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewNRGBA(image.Rect(0, 0, 120, 60)), nil); err != nil {
		t.Fatal(err)
	}
	app1 := append([]byte("Exif\x00\x00"), exif...)
	withExif := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(app1) + 2)}, app1...)
	withExif = append(withExif, jpegData.Bytes()[2:]...)
	pngChunk := func(kind string, payload []byte) []byte {
		chunk := make([]byte, len(payload)+12)
		binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
		copy(chunk[4:8], kind)
		copy(chunk[8:], payload)
		binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
		return chunk
	}
	pngData := variantPNG(t, 120, 60)
	pngExif := append(append(append([]byte{}, pngData[:33]...), pngChunk("eXIf", exif)...), pngData[33:]...)
	for _, source := range [][]byte{withExif, pngExif} {
		data, err := encodeImageVariant(source, imageVariantOptions{MaxWidth: 20, Quality: 80, Format: "webp"})
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width != 20 || cfg.Height != 40 {
			t.Fatalf("orientation: %v %v", cfg, err)
		}
	}
	apng := append(append(append([]byte{}, pngData[:33]...), pngChunk("acTL", []byte{0, 0, 0, 2, 0, 0, 0, 0})...), pngData[33:]...)
	if _, err := encodeImageVariant(apng, imageVariantOptions{Quality: 80, Format: "webp"}); err == nil {
		t.Fatal("APNG was flattened")
	}
	for _, invalid := range [][]byte{nil, exif[:7], {'I', 'I', 42, 0, 255, 255, 255, 255}} {
		if variantTIFFOrientation(invalid) != 1 {
			t.Fatal("invalid EXIF")
		}
	}
	source := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	source.Set(0, 0, color.White)
	for i, point := range []image.Point{{0, 0}, {2, 0}, {2, 1}, {0, 1}, {0, 0}, {1, 0}, {1, 2}, {0, 2}} {
		got := orientVariantImage(source, i+1)
		if got.At(point.X, point.Y) != (color.NRGBA{255, 255, 255, 255}) {
			t.Fatalf("orientation %d wrong pixel mapping", i+1)
		}
	}
}

func TestImageVariantRemoteCacheWriteFailure(t *testing.T) {
	p := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: t.TempDir()}}, zap.NewNop())
	raw := "https://image.tmdb.org/uncached.png"
	_, path, _ := p.remoteImageCachePathsForValidated(raw)
	p.client = &http.Client{Transport: imageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		// 让原图原子重命名失败，仍应能处理已下载的字节。
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(variantPNG(t, 120, 60))), Request: r}, nil
	})}
	w := httptest.NewRecorder()
	if err := p.Serve(t.Context(), w, httptest.NewRequest("GET", "/?maxWidth=40", nil), raw); err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil || cfg.Width != 40 || format != "webp" {
		t.Fatalf("memory variant: %v %s %v", cfg, format, err)
	}
}

func TestImageVariantRemoteRefreshWriteFailureUsesNewBytes(t *testing.T) {
	p := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: t.TempDir()}}, zap.NewNop())
	raw := "https://image.tmdb.org/refresh.png"
	_, path, _ := p.remoteImageCachePathsForValidated(raw)
	if err := writeStoredImage(path, variantPNG(t, 120, 60), "test-*.tmp"); err != nil {
		t.Fatal(err)
	}
	p.client = &http.Client{Transport: imageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		// 抓取已开始后令 CreateTemp 失败，但旧缓存文件仍在。
		p.cacheDir = path
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(variantPNG(t, 60, 120))), Request: r}, nil
	})}
	w := httptest.NewRecorder()
	if err := p.Serve(t.Context(), w, httptest.NewRequest("GET", "/?maxWidth=40&refresh=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil || cfg.Width != 40 || cfg.Height != 80 || format != "webp" {
		t.Fatalf("served stale image: %v %s %v", cfg, format, err)
	}
}
