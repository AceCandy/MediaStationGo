package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func probeLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Repo == nil || svc.Repo.Library == nil || svc.MediaProbe == nil || svc.Tasks == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "media probe unavailable"})
			return
		}
		libraryID := strings.TrimSpace(c.Param("id"))
		library, err := svc.Repo.Library.FindByID(c.Request.Context(), libraryID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if library == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
			return
		}

		if err := startProbeBackfill(svc, service.TaskTriggerManual, "媒体轨道回填："+library.Name, library.Path, libraryID, 0); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrMediaProbeBackfillRunning) {
				status = http.StatusConflict
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "started"})
	}
}

func startProbeBackfill(svc *service.Container, trigger, name, sourcePath, libraryID string, limit int) error {
	if svc == nil || svc.MediaProbe == nil || svc.Tasks == nil {
		return service.ErrMediaProbeBackfillUnavailable
	}
	svc.MediaProbe.SetTaskTracker(svc.Log, svc.Tasks, svc.Context())
	return svc.MediaProbe.StartBackfill(trigger, name, sourcePath, libraryID, limit)
}
