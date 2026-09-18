package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gen2brain/webp"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
)

// imageVariants 保存可重新生成的图片，不覆盖原图，也不自动清理。
type imageVariants struct {
	dir    string
	slots  chan struct{}
	mu     sync.Mutex
	active map[string]chan struct{}
}

func newImageVariants(cacheDir string) *imageVariants {
	return &imageVariants{dir: filepath.Join(cacheDir, "image-variants"), slots: make(chan struct{}, 2), active: make(map[string]chan struct{})}
}

type imageVariantOptions struct {
	Width, Height, MaxWidth, MaxHeight, FillWidth, FillHeight int
	Quality                                                   int
	Format                                                    string
}

func parseImageVariantOptions(r *http.Request) (imageVariantOptions, bool, error) {
	o := imageVariantOptions{Quality: 80, Format: "webp"}
	seen := make(map[string]bool)
	for name, values := range r.URL.Query() {
		name = strings.ToLower(name)
		var dst *int
		switch name {
		case "width":
			dst = &o.Width
		case "height":
			dst = &o.Height
		case "maxwidth":
			dst = &o.MaxWidth
		case "maxheight":
			dst = &o.MaxHeight
		case "fillwidth":
			dst = &o.FillWidth
		case "fillheight":
			dst = &o.FillHeight
		case "quality":
			dst = &o.Quality
		case "format":
		default:
			continue
		}
		if seen[name] || len(values) != 1 {
			return o, true, fmt.Errorf("duplicate image parameter: %s", name)
		}
		seen[name] = true
		if name == "format" {
			o.Format = strings.ToLower(values[0])
			if o.Format == "jpg" {
				o.Format = "jpeg"
			}
			if o.Format != "webp" && o.Format != "jpeg" && o.Format != "png" {
				return o, true, fmt.Errorf("unsupported image format")
			}
			continue
		}
		n, err := strconv.Atoi(values[0])
		limit := 4096
		if name == "quality" {
			limit = 100
		}
		if err != nil || n < 1 || n > limit {
			return o, true, fmt.Errorf("invalid image parameter: %s", name)
		}
		*dst = n
	}
	if (o.FillWidth == 0) != (o.FillHeight == 0) {
		return o, true, fmt.Errorf("fillWidth and fillHeight are required together")
	}
	return o, len(seen) != 0, nil
}

func (p *ImageProxy) serveImageFile(w http.ResponseWriter, r *http.Request, key, path, cacheControl string) bool {
	if p == nil {
		return serveImageFile(w, r, key, path, cacheControl)
	}
	return p.variants.serveFile(w, r, key, path, cacheControl)
}

func (v *imageVariants) serveFile(w http.ResponseWriter, r *http.Request, key, path, cacheControl string) bool {
	o, requested, err := parseImageVariantOptions(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	if !requested || v == nil {
		return serveImageFile(w, r, key, path, cacheControl)
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() == 0 {
		return false
	}
	source := fmt.Sprintf("%s:%d:%d", path, stat.Size(), stat.ModTime().UnixNano())
	if v.serve(w, r, source, o, func() ([]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, maxArtworkBytes+1))
	}) {
		return true
	}
	return serveImageFile(w, r, key, path, "no-store")
}

// serve 合并同一变体的生成；等待并发名额和其它请求时响应取消。
func (v *imageVariants) serve(w http.ResponseWriter, r *http.Request, source string, o imageVariantOptions, read func() ([]byte, error)) bool {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("v1:%s:%+v", source, o))))
	path := filepath.Join(v.dir, key[:2], key+"."+o.Format)
	for {
		if r.Context().Err() != nil {
			return true
		}
		if serveImageFile(w, r, key, path, imageBrowserCacheControl) {
			return true
		}
		v.mu.Lock()
		if done := v.active[key]; done != nil {
			v.mu.Unlock()
			select {
			case <-r.Context().Done():
				return true
			case <-done:
				continue
			}
		}
		done := make(chan struct{})
		v.active[key] = done
		v.mu.Unlock()
		defer func() { v.mu.Lock(); delete(v.active, key); close(done); v.mu.Unlock() }()
		break
	}
	select {
	case v.slots <- struct{}{}:
	case <-r.Context().Done():
		return true
	}
	defer func() { <-v.slots }()
	// 另一个请求可能在取得生成所有权前已经写入缓存。
	if serveImageFile(w, r, key, path, imageBrowserCacheControl) {
		return true
	}
	data, err := read()
	if err != nil {
		return false
	}
	data, err = encodeImageVariant(data, o)
	if err != nil {
		return false
	}
	if r.Context().Err() != nil {
		return true
	}
	if err := writeStoredImage(path, data, ".variant-*.tmp"); err != nil {
		return false
	}
	return serveImageFile(w, r, key, path, imageBrowserCacheControl)
}

func encodeImageVariant(data []byte, o imageVariantOptions) ([]byte, error) {
	if len(data) > maxArtworkBytes {
		return nil, fmt.Errorf("image too large")
	}
	if isTransparentPlaceholderData(data) {
		return nil, fmt.Errorf("placeholder passthrough")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32_000_000 {
		return nil, fmt.Errorf("image pixel limit exceeded")
	}
	orientation, animated := variantMetadata(data, format)
	// 动图保留原始字节，避免缩略处理静默丢帧。
	if animated || format == "gif" || (format == "webp" && len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&2 != 0) {
		return nil, fmt.Errorf("animated image passthrough")
	}
	var src image.Image
	if format == "webp" {
		src, err = webp.Decode(bytes.NewReader(data), webp.Options{AutoRotate: true})
	} else {
		src, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, err
	}
	src = orientVariantImage(src, orientation)
	bounds := src.Bounds()
	if o.FillWidth > 0 {
		if int64(bounds.Dx())*int64(o.FillHeight) > int64(bounds.Dy())*int64(o.FillWidth) {
			width := max(1, bounds.Dy()*o.FillWidth/o.FillHeight)
			bounds.Min.X += (bounds.Dx() - width) / 2
			bounds.Max.X = bounds.Min.X + width
		} else {
			height := max(1, bounds.Dx()*o.FillHeight/o.FillWidth)
			bounds.Min.Y += (bounds.Dy() - height) / 2
			bounds.Max.Y = bounds.Min.Y + height
		}
	}
	scale := math.Min(1, math.Min(4096/float64(bounds.Dx()), 4096/float64(bounds.Dy())))
	for _, limit := range []struct{ value, source int }{{o.Width, bounds.Dx()}, {o.Height, bounds.Dy()}, {o.MaxWidth, bounds.Dx()}, {o.MaxHeight, bounds.Dy()}, {o.FillWidth, bounds.Dx()}, {o.FillHeight, bounds.Dy()}} {
		if limit.value > 0 {
			scale = math.Min(scale, float64(limit.value)/float64(limit.source))
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, max(1, int(math.Round(float64(bounds.Dx())*scale))), max(1, int(math.Round(float64(bounds.Dy())*scale)))))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)
	var out bytes.Buffer
	switch o.Format {
	case "jpeg":
		// JPEG 不支持透明，使用白底而不是隐式黑底。
		background := image.NewRGBA(dst.Bounds())
		draw.Draw(background, background.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(background, background.Bounds(), dst, image.Point{}, draw.Over)
		err = jpeg.Encode(&out, background, &jpeg.Options{Quality: o.Quality})
	case "png":
		err = png.Encode(&out, dst)
	default:
		err = webp.Encode(&out, dst, webp.Options{Quality: o.Quality, Method: 4})
	}
	return out.Bytes(), err
}
