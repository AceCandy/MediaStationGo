// Package handler — live tasks board.
//
// /api/tasks aggregates running ffmpeg transcodes and recent scrape progress
// into a single snapshot suitable for the
// React Tasks panel. The panel can layer this REST snapshot on top of
// live WS events for instant updates.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func tasksHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var transcodes []service.ActiveJob
		if svc.Transcoder != nil {
			transcodes = svc.Transcoder.Active()
		}
		background := service.TaskSnapshot{}
		if svc.Tasks != nil {
			background = svc.Tasks.Snapshot()
		}
		c.JSON(http.StatusOK, gin.H{
			"transcodes":       transcodes,
			"background_tasks": background,
		})
	}
}
