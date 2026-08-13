// Package handler — live tasks board.
//
// /api/tasks aggregates recent background work.
// into a single snapshot suitable for the
// React Tasks panel. The panel can layer this REST snapshot on top of
// live WS events for instant updates.
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func tasksHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		background := service.TaskSnapshot{}
		page := service.TaskPage{Items: []service.BackgroundTask{}, Page: 1, PageSize: 30}
		definitions := []service.TaskDefinition{}
		if svc != nil && svc.Tasks != nil {
			background = svc.Tasks.Snapshot()
			pageNum, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
			var err error
			page, err = svc.Tasks.List(pageNum, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tasks"})
				return
			}
			var scheduler []service.JobStatus
			if svc.Scheduler != nil {
				scheduler = svc.Scheduler.Status()
			}
			definitions, err = svc.Tasks.Definitions(scheduler)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list task definitions"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"background_tasks": background,
			"items":            page.Items,
			"page":             page.Page,
			"page_size":        page.PageSize,
			"total":            page.Total,
			"definitions":      definitions,
		})
	}
}

func taskDefinitionHistoryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Tasks == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task center unavailable"})
			return
		}
		pageNum, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
		page, err := svc.Tasks.DefinitionHistory(c.Param("key"), pageNum, pageSize)
		if errors.Is(err, service.ErrTaskDefinitionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task definition not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list task history"})
			return
		}
		c.JSON(http.StatusOK, page)
	}
}

func taskDefinitionRunHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("key")
		if key == service.TaskDefinitionPeopleBackfill {
			if svc == nil || svc.Scraper == nil || svc.Tasks == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "people backfill unavailable"})
				return
			}
			svc.Scraper.TriggerPeopleBackfill()
			c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
			return
		}
		job, ok := service.TaskDefinitionSchedulerJob(key)
		if !ok {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "task does not support manual execution"})
			return
		}
		if svc == nil || svc.Scheduler == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler unavailable"})
			return
		}
		if !triggerSchedulerJob(c, svc, job) {
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
	}
}

func taskDefinitionLogHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Tasks == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task center unavailable"})
			return
		}
		tailBytes, _ := strconv.ParseInt(c.DefaultQuery("tail_bytes", "0"), 10, 64)
		log, err := svc.Tasks.ReadDefinitionLog(c.Param("key"), c.Query("date"), tailBytes)
		if errors.Is(err, service.ErrTaskDefinitionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task definition not found"})
			return
		}
		if errors.Is(err, service.ErrTaskLogDateNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "task log date not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read task log"})
			return
		}
		c.JSON(http.StatusOK, log)
	}
}
