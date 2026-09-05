// Package handler — library / media HTTP endpoints.
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type createLibraryReq struct {
	Name     string                     `json:"name" binding:"required"`
	Path     string                     `json:"path"`
	Paths    []string                   `json:"paths"`
	Roots    []service.LibraryRootInput `json:"roots"`
	Type     string                     `json:"type"`
	CoverURL string                     `json:"cover_url"`
}

func listLibrariesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libs, err := svc.Media.ListLibraries(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		role, _ := c.Get(middleware.CtxUserRole)
		includeHidden := role == "admin" && (c.Query("include_hidden") == "1" || c.Query("all") == "1")
		if !includeHidden {
			visibility := mediaVisibilityForRequest(c, svc)
			filtered := libs[:0]
			for _, lib := range libs {
				if service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, lib, visibility) {
					filtered = append(filtered, lib)
				}
			}
			libs = filtered
		}
		c.JSON(http.StatusOK, libs)
	}
}

func getLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		lib, err := svc.Repo.Library.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if lib == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		role, _ := c.Get(middleware.CtxUserRole)
		includeHidden := role == "admin" && (c.Query("include_hidden") == "1" || c.Query("all") == "1")
		if includeHidden {
			c.JSON(http.StatusOK, lib)
			return
		}
		if !service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, *lib, mediaVisibilityForRequest(c, svc)) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, lib)
	}
}

func createLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createLibraryReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		roots := req.Roots
		if len(roots) == 0 {
			for _, path := range req.Paths {
				roots = append(roots, service.LibraryRootInput{Path: path})
			}
		}
		if len(roots) == 0 && strings.TrimSpace(req.Path) != "" {
			roots = append(roots, service.LibraryRootInput{Path: req.Path})
		}
		result, err := svc.Media.CreateLibraryWithRootsAndCover(c.Request.Context(), req.Name, req.Type, req.CoverURL, roots)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrCloudLibraryRootUnsupported) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		l := result.Library
		uid, _ := c.Get("ctx_user_id")
		svc.Audit.Record(c.Request.Context(), toString(uid), "library.create", l.ID, c.ClientIP(), l.Path)
		// Refresh fsnotify watcher to pick up the new library root.
		go func() { _ = svc.Watcher.Refresh(context.Background()) }()
		if result.Created {
			_, scanErr := startLibraryScanTask(svc, l, service.TaskTriggerEvent, "新增媒体库自动扫描")
			logAutomaticScanStartError(svc, l.ID, scanErr)
		} else {
			for i := range result.AddedRoots {
				root := result.AddedRoots[i]
				if !root.Enabled {
					continue
				}
				_, scanErr := startLibraryRootScanTask(svc, l.ID, root.ID, l.Name, root.Path, service.TaskTriggerEvent, "新增路径自动扫描")
				logAutomaticScanStartError(svc, root.ID, scanErr)
			}
		}
		c.JSON(http.StatusCreated, l)
	}
}

type updateLibraryReq struct {
	CoverURL string `json:"cover_url"`
}

func updateLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req updateLibraryReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := svc.Media.UpdateLibraryCover(c.Request.Context(), c.Param("id"), req.CoverURL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		lib, err := svc.Repo.Library.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || lib == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
			return
		}
		c.JSON(http.StatusOK, lib)
	}
}

func uploadLibraryCoverHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxLibraryCoverRequestBytes)
		header, err := c.FormFile("cover")
		if err != nil {
			status := http.StatusBadRequest
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				status = http.StatusRequestEntityTooLarge
			}
			c.JSON(status, gin.H{"error": "valid cover image is required"})
			return
		}
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		file, err := header.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer file.Close()
		lib, err := svc.Media.SaveLibraryCover(c.Request.Context(), c.Param("id"), file)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrInvalidLibraryCover) {
				status = http.StatusBadRequest
			} else if errors.Is(err, service.ErrLibraryNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, lib)
	}
}

func serveLibraryCoverHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Media.ServeLibraryCover(c.Request.Context(), c.Writer, c.Request, c.Param("id")); err != nil {
			if errors.Is(err, service.ErrLibraryCoverNotFound) {
				c.Status(http.StatusNotFound)
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
	}
}

func clearLibraryCoverHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		lib, err := svc.Media.ClearLibraryCover(c.Request.Context(), c.Param("id"))
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrLibraryNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, lib)
	}
}

func deleteLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := svc.Media.DeleteLibrary(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get("ctx_user_id")
		svc.Audit.Record(c.Request.Context(), toString(uid), "library.delete", id, c.ClientIP(), "")
		go func() { _ = svc.Watcher.Refresh(context.Background()) }()
		c.Status(http.StatusNoContent)
	}
}

func listMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
		groupVersions := c.DefaultQuery("group_versions", "1") != "0"
		visibility := mediaVisibilityForRequest(c, svc)
		visibility.MissingPoster = c.Query("missing_poster") == "1"
		visibility.MissingChineseTitle = c.Query("missing_chinese_title") == "1"
		if !groupVersions {
			items, total, err := svc.Media.ListMediaVisible(c.Request.Context(), id, page, size, visibility)
			if err != nil {
				writeInternalOrCanceled(c, err)
				return
			}
			if items == nil {
				items = []model.MediaView{}
			}
			c.JSON(http.StatusOK, gin.H{
				"items":     items,
				"total":     total,
				"page":      page,
				"page_size": size,
			})
			return
		}
		items, total, err := svc.Media.ListMediaVisibleGrouped(c.Request.Context(), id, page, size, visibility)
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if items == nil {
			items = []service.MediaItem{}
		}
		c.JSON(http.StatusOK, gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": size,
		})
	}
}

func getMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMediaVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

func getMediaSTRMTargetHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, err := svc.Media.GetSTRMTargetVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if target == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"target": target})
	}
}

func enrichMediaFromDoubanHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMediaVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "豆瓣信息补齐失败"})
			return
		}
		if m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if (m.MetadataKind != model.MetadataKindMovie && m.MetadataKind != model.MetadataKindSeries) || strings.TrimSpace(m.DoubanID) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持已绑定唯一豆瓣 ID 的电影或电视剧"})
			return
		}
		if svc.Scraper == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "豆瓣信息补齐失败"})
			return
		}
		degraded, err := svc.Scraper.EnrichFromDouban(c.Request.Context(), m.MetadataID)
		switch {
		case errors.Is(err, service.ErrDoubanEnrichmentIneligible):
			c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持已绑定唯一豆瓣 ID 的电影或电视剧"})
		case errors.Is(err, service.ErrDoubanTemporarilyUnavailable):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "豆瓣请求受限或暂时不可用，请稍后重试"})
		case errors.Is(err, service.ErrDoubanSubjectNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "豆瓣条目不存在"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "豆瓣信息补齐失败"})
		default:
			status := "complete"
			if degraded {
				status = "degraded"
			}
			c.JSON(http.StatusOK, gin.H{"status": status})
		}
	}
}

func listMediaVersionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		items, err := svc.Media.ListMediaVersions(c.Request.Context(), c.Param("id"), toString(uid), mediaVisibilityForRequest(c, svc))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(items) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, items)
	}
}

func ensureMediaProbeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMedia(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if m == nil || !mediaViewVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if svc.MediaProbe == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "media probe unavailable"})
			return
		}
		if svc.MediaProbe.NeedsProbe(c.Request.Context(), m.ID) {
			if _, err := svc.MediaProbe.ProbeMedia(c.Request.Context(), m.ID); err != nil {
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "media probe failed"})
				return
			}
			m, err = svc.Media.GetMedia(c.Request.Context(), m.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, m)
	}
}

func updateMediaMetadataHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req service.MediaMetadataUpdate
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		m, err := svc.Media.UpdateMetadata(c.Request.Context(), c.Param("id"), req)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				status = http.StatusNotFound
			} else if strings.Contains(strings.ToLower(err.Error()), "required") {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

func searchMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		q := c.Query("q")
		groupVersions := c.DefaultQuery("group_versions", "1") != "0"
		if c.Query("page") != "" || c.Query("page_size") != "" {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
			if !groupVersions {
				items, total, err := svc.Media.SearchMediaVisiblePage(c.Request.Context(), q, page, size, mediaVisibilityForRequest(c, svc))
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{
					"items":     items,
					"total":     total,
					"page":      page,
					"page_size": size,
				})
				return
			}
			items, total, err := svc.Media.SearchMediaVisiblePageGrouped(c.Request.Context(), q, page, size, mediaVisibilityForRequest(c, svc))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"items":     items,
				"total":     total,
				"page":      page,
				"page_size": size,
			})
			return
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		if !groupVersions {
			items, err := svc.Media.SearchMediaVisible(c.Request.Context(), q, limit, mediaVisibilityForRequest(c, svc))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"items": items})
			return
		}
		items, err := svc.Media.SearchMediaVisibleGrouped(c.Request.Context(), q, limit, mediaVisibilityForRequest(c, svc))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func streamHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetRawMedia(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if !enforceScopedPlaybackToken(c, m.ID) {
			return
		}
		err = svc.Stream.ServeMedia(c.Writer, c.Request, m)
		if errors.Is(err, service.ErrMediaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
}
