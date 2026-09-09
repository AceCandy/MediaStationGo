package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func tmdbRecheckListHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Param("key") != "tmdb_episode_metadata_recheck" {
			c.JSON(http.StatusNotFound, gin.H{"error": "task definition not found"})
			return
		}
		if svc == nil || svc.Repo == nil || svc.Repo.Metadata == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task center unavailable"})
			return
		}
		page, err1 := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, err2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if err1 != nil || err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination"})
			return
		}
		var out any
		var err error
		if id := c.Param("metadataID"); id != "" {
			out, err = svc.Repo.Metadata.ListTMDbRecheckFiles(c.Request.Context(), id, page, size)
		} else {
			out, err = svc.Repo.Metadata.ListTMDbRechecks(c.Request.Context(), c.Query("status"), c.Query("keyword"), page, size)
		}
		if errors.Is(err, repository.ErrTMDbRecheckFilter) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recheck filter"})
			return
		}
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
