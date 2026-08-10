// Package handler — live tasks board.
//
// /api/tasks aggregates recent background work.
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
		background := service.TaskSnapshot{}
		if svc.Tasks != nil {
			background = svc.Tasks.Snapshot()
		}
		c.JSON(http.StatusOK, gin.H{
			"background_tasks": background,
		})
	}
}
