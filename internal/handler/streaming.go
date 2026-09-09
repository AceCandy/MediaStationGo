// Package handler — image-proxy / scrape endpoints.
package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func scrapeIssuesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Media == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "media scrape issues unavailable"})
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
		statuses := strings.Split(c.DefaultQuery("status", "error,no_match"), ",")
		result, err := svc.Media.ListScrapeIssues(c.Request.Context(), c.Query("library_id"), c.Query("keyword"), statuses, page, pageSize)
		if errors.Is(err, service.ErrInvalidScrapeIssueStatus) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "status must be error or no_match"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list media scrape issues"})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func imageProxyHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.Query("url")
		if c.Query("refresh") != "" {
			_ = svc.ImageProxy.RemoveFailed(raw)
		} else if c.Query("retry") != "" {
			_ = svc.ImageProxy.RemoveFailed(raw)
		}
		// Serve handles upstream errors internally by returning a 1×1 PNG
		// placeholder, so the only error we can get back here is a malformed
		// URL. In that case we still return 400 to make the misuse visible.
		if err := svc.ImageProxy.Serve(c.Request.Context(), c.Writer, c.Request, raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
}

type scrapeRequest struct {
	EpisodeArtwork *bool `json:"episode_artwork"`
	EpisodeImages  *bool `json:"episode_images"`
	RefreshMatched *bool `json:"refresh_matched"`
	IncludeMatched *bool `json:"include_matched"`
}

func (r scrapeRequest) episodeArtworkOption() *bool {
	if r.EpisodeImages != nil {
		return r.EpisodeImages
	}
	return r.EpisodeArtwork
}

func (r scrapeRequest) includeMatchedOption() bool {
	if r.IncludeMatched != nil {
		return *r.IncludeMatched
	}
	if r.RefreshMatched != nil {
		return *r.RefreshMatched
	}
	return false
}

func scrapeOptionsFromRequest(c *gin.Context, retryNoMatch bool) (service.ScrapeOptions, error) {
	options := service.ScrapeOptions{RetryNoMatch: retryNoMatch}
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return options, nil
	}
	var req scrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return options, nil
		}
		return options, err
	}
	options.EpisodeArtwork = req.episodeArtworkOption()
	options.IncludeMatched = req.includeMatchedOption()
	return options, nil
}

// scrapeOneHandler enriches a single media via the configured scraper chain.
func scrapeOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		options, err := scrapeOptionsFromRequest(c, true)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scrape options"})
			return
		}
		options.IncludeMatched = true
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if _, err := svc.Scraper.ResetMediaScrape(c.Request.Context(), m.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
	}
}

// reprobeHandler re-runs ffprobe against a single media. Admin-only.
func reprobeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Stream.Probe(c.Request.Context(), c.Param("id"), svc.FFprobe); err != nil {
			if errors.Is(err, service.ErrMediaNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			// ffprobe unavailable or file inaccessible — still 200 with error info
			c.JSON(http.StatusOK, gin.H{"code": 1, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
	}
}
