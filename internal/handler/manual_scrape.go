package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type manualScrapeApplyReq struct {
	MediaIDs []string                    `json:"media_ids"`
	Match    service.ManualScrapeRequest `json:"match"`
}

const manualScrapeApplyTimeout = 5 * time.Minute

func manualScrapeSearchHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		results, err := svc.Scraper.ManualSearch(
			c.Request.Context(),
			m,
			c.Query("query"),
			c.DefaultQuery("provider", "all"),
			c.Query("media_type"),
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": results})
	}
}

func manualScrapeApplyOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req service.ManualScrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		applyCtx, cancel := manualScrapeApplyContext(c)
		defer cancel()
		media, err := svc.Scraper.ApplyManualMatch(applyCtx, c.Param("id"), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, media)
	}
}

func manualScrapeApplyBatchHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req manualScrapeApplyReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ids := compactManualScrapeIDs(req.MediaIDs)
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "media_ids required"})
			return
		}
		applyCtx, cancel := manualScrapeApplyContext(c)
		defer cancel()
		applied := 0
		errorsOut := make([]string, 0)
		for _, id := range ids {
			if _, err := svc.Scraper.ApplyManualMatch(applyCtx, id, req.Match); err != nil {
				errorsOut = append(errorsOut, id+": "+err.Error())
				continue
			}
			applied++
		}
		if applied == 0 && len(errorsOut) > 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": strings.Join(errorsOut, "\n")})
			return
		}
		c.JSON(http.StatusOK, gin.H{"applied": applied, "errors": errorsOut})
	}
}

func manualScrapeApplyContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(c.Request.Context()), manualScrapeApplyTimeout)
}

func compactManualScrapeIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
