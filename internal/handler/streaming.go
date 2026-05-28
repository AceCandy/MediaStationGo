// Package handler — HLS / image-proxy / scrape endpoints.
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func hlsPlaylistHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := svc.Stream.ServeHLSPlaylist(c.Writer, c.Request, c.Param("id"))
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

func hlsSegmentHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := svc.Stream.ServeHLSSegment(c.Writer, c.Request, c.Param("id"), c.Param("seg"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
	}
}

func stopTranscodeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc.Transcoder.StopJob(c.Param("id"))
		c.Status(http.StatusNoContent)
	}
}

func imageProxyHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.Query("url")
		// Serve handles upstream errors internally by returning a 1×1 PNG
		// placeholder, so the only error we can get back here is a malformed
		// URL. In that case we still return 400 to make the misuse visible.
		if err := svc.ImageProxy.Serve(c.Request.Context(), c.Writer, c.Request, raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
}

// scrapeOneHandler enriches a single media via the configured scraper chain.
func scrapeOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if err := svc.Scraper.EnrichOne(c.Request.Context(), m); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		refreshed, _ := svc.Repo.Media.FindByID(c.Request.Context(), m.ID)
		c.JSON(http.StatusOK, refreshed)
	}
}

// scrapeLibraryHandler retries every pending/no_match media in a library.
func scrapeLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Run in the background so HTTP returns instantly; the WS hub
		// pushes per-item progress on the "scrape" topic.
		go func(libID string) {
			_, _ = svc.Scraper.EnrichLibrary(context.Background(), libID, true)
		}(c.Param("id"))
		c.JSON(http.StatusAccepted, gin.H{"status": "scraping"})
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
