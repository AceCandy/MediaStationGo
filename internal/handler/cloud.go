// Package handler — cloud-disk (网盘) endpoints: directory browsing, QR-code
// login, media import and 302 playback redirects.
package handler

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

// cloudListHandler browses a configured cloud disk directory.
func cloudListHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		dir := c.Query("dir")
		entries, err := svc.StorageCfg.CloudList(c.Request.Context(), typ, dir)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"error": err.Error(), "items": []any{}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": entries})
	}
}

// cloudImportHandler turns a cloud file into a playable 302-backed media item.
func cloudImportHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		var in struct {
			Ref  string `json:"ref" binding:"required"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		m, err := svc.StorageCfg.CloudImport(c.Request.Context(), typ, in.Ref, in.Name, in.Size)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

// cloudMountHandler creates or reuses a cloud:// media library for a cloud
// directory, then scans it recursively so cloud files become playable STRM/302
// media rows without copying bytes to local disk.
func cloudMountHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		var in struct {
			Dir       string `json:"dir"`
			Name      string `json:"name"`
			MediaType string `json:"media_type"`
		}
		_ = c.ShouldBindJSON(&in)
		if !cloud.IsCloudType(typ) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported cloud provider"})
			return
		}
		if _, err := svc.StorageCfg.CloudProvider(c.Request.Context(), typ); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		path := "cloud://" + typ
		if dir := strings.TrimSpace(in.Dir); dir != "" {
			path += "/" + url.PathEscape(dir)
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = cloudMountLibraryName(typ, strings.TrimSpace(in.Dir))
		}
		mediaType := strings.TrimSpace(in.MediaType)
		if mediaType == "" || strings.EqualFold(mediaType, "auto") {
			mediaType = cloudMountMediaType(strings.TrimSpace(in.Dir), name)
		}
		libs, err := svc.Repo.Library.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var lib *model.Library
		for i := range libs {
			if libs[i].Path == path {
				lib = &libs[i]
				break
			}
		}
		if lib == nil {
			lib = &model.Library{Name: name, Path: path, Type: mediaType, Enabled: true}
			if err := svc.Repo.Library.Create(c.Request.Context(), lib); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		var scan any
		if svc.Scan != nil {
			res, err := svc.Scan.ScanLibrary(c.Request.Context(), lib.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "library": lib})
				return
			}
			scan = res
		}
		c.JSON(http.StatusOK, gin.H{"library": lib, "scan": scan})
	}
}

func cloudMountLibraryName(typ, dir string) string {
	base := typ
	switch typ {
	case cloud.TypeQuark:
		base = "夸克网盘"
	case cloud.Type115:
		base = "115 网盘"
	case cloud.TypeCloudDrive2:
		base = "CloudDrive2"
	case cloud.TypeOpenList:
		base = "OpenList"
	}
	if dir == "" || dir == "0" {
		return base
	}
	return base + " · " + dir
}

func cloudMountMediaType(dir, name string) string {
	text := strings.ToLower(dir + " " + name)
	switch {
	case strings.Contains(text, "成人") || strings.Contains(text, "adult") || strings.Contains(text, "jav") || strings.Contains(text, "9kg"):
		return "adult"
	case strings.Contains(text, "动漫") || strings.Contains(text, "动画") || strings.Contains(text, "国漫") || strings.Contains(text, "日番") || strings.Contains(text, "anime"):
		return "anime"
	case strings.Contains(text, "综艺") || strings.Contains(text, "variety") || strings.Contains(text, "show"):
		return "variety"
	case strings.Contains(text, "剧") || strings.Contains(text, "series") || strings.Contains(text, "tv"):
		return "tv"
	default:
		return "movie"
	}
}

// cloud115QRStartHandler begins a 115 QR-code login and returns the session +
// QR image URL for the frontend to render.
func cloud115QRStartHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Param("type") != cloud.Type115 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "qr login is only supported for 115"})
			return
		}
		sess, err := cloud.QRStart(c.Request.Context(), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, sess)
	}
}

// cloud115QRPollHandler polls a 115 QR session; on confirmation it returns the
// session cookie so the frontend can save it as the storage credential.
func cloud115QRPollHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Param("type") != cloud.Type115 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "qr login is only supported for 115"})
			return
		}
		var sess cloud.QRSession
		if err := c.ShouldBindJSON(&sess); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := cloud.QRPoll(c.Request.Context(), nil, &sess)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, st)
	}
}

// cloudPlayHandler resolves a cloud file to its direct link and either issues a
// 302 redirect (true offload — host does not stream the bytes) or, when the
// provider requires authenticated headers, reverse-proxies the response.
func cloudPlayHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		ref := c.Query("ref")
		if ref == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ref required"})
			return
		}
		link, err := svc.StorageCfg.CloudResolve(c.Request.Context(), typ, ref, c.Request.UserAgent())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if !link.Proxy {
			// Pure offload: send the client straight to the cloud CDN.
			c.Redirect(http.StatusFound, link.URL)
			return
		}
		// Proxy mode: the direct link needs auth headers the browser cannot
		// carry. Stream through with Range forwarding.
		method := c.Request.Method
		if method == "" {
			method = http.MethodGet
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), method, link.URL, nil)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		for k, v := range link.Headers {
			req.Header.Set(k, v)
		}
		if rng := c.GetHeader("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
			if v := resp.Header.Get(h); v != "" {
				c.Header(h, v)
			}
		}
		c.Status(resp.StatusCode)
		if c.Request.Method != http.MethodHead {
			_, _ = io.Copy(c.Writer, resp.Body)
		}
	}
}
