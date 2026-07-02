// Package handler — STRM (URL-as-file) admin endpoints.
//
// Setting a media row's strm_url makes the stream handler issue a 302
// redirect to that URL instead of opening a local file. This lets the
// operator expose WebDAV / Alist / S3 / HTTP direct links as ordinary
// MediaStationGo entries.
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type strmReq struct {
	URL string `json:"url" binding:"required"`
}

func setSTRMHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req strmReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		url := strings.TrimSpace(req.URL)
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "url must start with http:// or https://"})
			return
		}
		mediaID := c.Param("id")
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), mediaID)
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		if err := svc.Repo.DB.WithContext(c.Request.Context()).
			Model(&model.Media{}).
			Where("id = ?", mediaID).
			Update("strm_url", url).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"strm_url": url})
	}
}

func clearSTRMHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Repo.DB.WithContext(c.Request.Context()).
			Model(&model.Media{}).
			Where("id = ?", c.Param("id")).
			Update("strm_url", "").Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// importSTRMHandler creates a media row directly from a (library_id, title, url)
// tuple — useful for adding a streaming-only entry without an on-disk file.
type importSTRMReq struct {
	LibraryID string `json:"library_id" binding:"required"`
	Title     string `json:"title" binding:"required"`
	URL       string `json:"url" binding:"required"`
}

func importSTRMHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req importSTRMReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		url := strings.TrimSpace(req.URL)
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "url must start with http:// or https://"})
			return
		}
		m := &model.Media{
			LibraryID: req.LibraryID,
			Title:     req.Title,
			Path:      url,
			STRMURL:   url,
			Container: "strm",
		}
		if err := svc.Repo.Media.Upsert(c.Request.Context(), m); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, m)
	}
}

type generateSTRMReq struct {
	LibraryID    string `json:"library_id"`
	OutputDir    string `json:"output_dir"`
	BaseURL      string `json:"base_url"`
	Enabled      bool   `json:"enabled"`
	Overwrite    bool   `json:"overwrite"`
	IncludeLocal *bool  `json:"include_local"`
	PreserveTree bool   `json:"preserve_tree"`
}

type generateSTRMTreeReq struct {
	Provider     string   `json:"provider"`
	TreeText     string   `json:"tree_text"`
	Paths        []string `json:"paths"`
	SourceRoot   string   `json:"source_root"`
	OutputPrefix string   `json:"output_prefix"`
	OutputDir    string   `json:"output_dir"`
	BaseURL      string   `json:"base_url"`
	Overwrite    bool     `json:"overwrite"`
	Cleanup      bool     `json:"cleanup"`
	DryRun       bool     `json:"dry_run"`
	BatchLimit   int      `json:"batch_limit"`
}

func generateSTRMHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req generateSTRMReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		strmSvc := svc.STRM
		if strmSvc == nil {
			strmSvc = service.NewSTRMService(svc.Log, svc.Repo, svc.Cfg)
		}
		baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		if baseURL == "" {
			baseURL = strings.TrimRight(absoluteRequestURL(c, "/"), "/")
		}
		includeLocal := true
		if req.IncludeLocal != nil {
			includeLocal = *req.IncludeLocal
		}
		options := service.GenerateSTRMOptions{
			LibraryID:     req.LibraryID,
			OutputDir:     req.OutputDir,
			BaseURL:       baseURL,
			Enabled:       req.Enabled,
			Overwrite:     req.Overwrite,
			IncludeLocal:  includeLocal,
			PreserveTree:  req.PreserveTree,
			PlaybackToken: strmPlaybackTokenForRequest(c, svc),
		}
		var res *service.GenerateSTRMResult
		var err error
		if strings.TrimSpace(req.LibraryID) == "*" {
			res, err = strmSvc.GenerateForAllLibraries(c.Request.Context(), options)
		} else {
			res, err = strmSvc.GenerateForLibrary(c.Request.Context(), options)
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, res)
	}
}

func generateSTRMFromTreeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req generateSTRMTreeReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		strmSvc := svc.STRM
		if strmSvc == nil {
			strmSvc = service.NewSTRMService(svc.Log, svc.Repo, svc.Cfg)
		}
		baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		if baseURL == "" {
			baseURL = strings.TrimRight(absoluteRequestURL(c, "/"), "/")
		}
		res, err := strmSvc.GenerateFromTree(c.Request.Context(), service.GenerateSTRMTreeOptions{
			Provider:     req.Provider,
			TreeText:     req.TreeText,
			Paths:        req.Paths,
			SourceRoot:   req.SourceRoot,
			OutputPrefix: req.OutputPrefix,
			OutputDir:    req.OutputDir,
			BaseURL:      baseURL,
			Overwrite:    req.Overwrite,
			Cleanup:      req.Cleanup,
			DryRun:       req.DryRun,
			BatchLimit:   req.BatchLimit,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, res)
	}
}

func strmPlaybackTokenForRequest(c *gin.Context, svc *service.Container) string {
	if svc == nil || svc.Auth == nil || svc.Repo == nil || svc.Repo.User == nil {
		return ""
	}
	uid := middleware.GetUserID(c)
	if uid == "" {
		return ""
	}
	u, err := svc.Repo.User.FindByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		return ""
	}
	token, err := svc.Auth.IssueEmbyToken(u)
	if err != nil {
		return ""
	}
	return token
}
