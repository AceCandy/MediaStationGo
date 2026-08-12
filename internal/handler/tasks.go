// Package handler — live tasks board.
//
// /api/tasks aggregates recent background work.
// into a single snapshot suitable for the
// React Tasks panel. The panel can layer this REST snapshot on top of
// live WS events for instant updates.
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func tasksHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		background := service.TaskSnapshot{}
		page := service.TaskPage{Items: []service.BackgroundTask{}, Page: 1, PageSize: 30}
		if svc.Tasks != nil {
			background = svc.Tasks.Snapshot()
			pageNum, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
			var err error
			page, err = svc.Tasks.List(pageNum, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tasks"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"background_tasks": background,
			"items":            page.Items,
			"page":             page.Page,
			"page_size":        page.PageSize,
			"total":            page.Total,
		})
	}
}

func taskLogHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Tasks == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		tailBytes, _ := strconv.ParseInt(c.DefaultQuery("tail_bytes", "0"), 10, 64)
		log, found, err := svc.Tasks.ReadLog(c.Param("id"), tailBytes)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read task log"})
			return
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, log)
	}
}
