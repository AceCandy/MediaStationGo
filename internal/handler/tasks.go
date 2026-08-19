// Package handler — live tasks board.
//
// /api/tasks aggregates recent background work.
// into a single snapshot suitable for the
// React Tasks panel. The panel can layer this REST snapshot on top of
// live WS events for instant updates.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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
		if key == service.TaskDefinitionProbeBackfill {
			if svc == nil || svc.MediaProbe == nil || svc.Tasks == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "media probe backfill unavailable"})
				return
			}
			var request struct {
				Limit     int    `json:"limit"`
				LibraryID string `json:"library_id"`
			}
			if c.Request != nil && c.Request.Body != nil {
				if err := json.NewDecoder(c.Request.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
					c.JSON(http.StatusBadRequest, gin.H{"error": "invalid probe backfill request"})
					return
				}
			}
			if request.Limit < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be positive"})
				return
			}
			request.LibraryID = strings.TrimSpace(request.LibraryID)
			taskName := "媒体轨道回填"
			sourcePath := ""
			if request.LibraryID != "" {
				if svc.Repo == nil || svc.Repo.Library == nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"error": "library unavailable"})
					return
				}
				library, err := svc.Repo.Library.FindByID(c.Request.Context(), request.LibraryID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load library"})
					return
				}
				if library == nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
					return
				}
				taskName += "：" + library.Name
				sourcePath = library.Path
			}
			task := svc.Tasks.StartTriggeredIfKindIdle(service.TaskKindProbe, service.TaskTriggerManual, taskName, service.TaskUpdate{
				Stage: "probe", SourcePath: sourcePath, Message: "媒体轨道回填已启动", Metrics: service.ProbeBackfillResult{}.Metrics(),
			})
			if task == nil {
				if svc.Tasks.IsKindRunning(service.TaskKindProbe) {
					c.JSON(http.StatusConflict, gin.H{"error": "media probe backfill already running"})
				} else {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "create task execution failed"})
				}
				return
			}
			go func() {
				progress := func(current service.ProbeBackfillResult) {
					task.Update(service.TaskUpdate{Stage: "probe", Metrics: current.Metrics(), Details: current.Details, DetailsWithoutLevel: true})
				}
				var result service.ProbeBackfillResult
				var err error
				if request.LibraryID == "" {
					result, err = svc.MediaProbe.BackfillAll(svc.Context(), request.Limit, progress)
				} else {
					result, err = svc.MediaProbe.BackfillLibrary(svc.Context(), request.LibraryID, request.Limit, progress)
				}
				stage, message := "completed", "媒体轨道回填完成"
				if err != nil {
					stage, message = "probe", "媒体轨道回填失败"
				}
				finishHTTPTask(task, err, stage, message, result.Metrics(), nil, false)
			}()
			c.JSON(http.StatusAccepted, gin.H{"status": "started"})
			return
		}
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

func taskDefinitionScheduleHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("key")
		if !service.TaskDefinitionExists(key) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task definition not found"})
			return
		}
		job, ok := service.TaskDefinitionScheduleJob(key)
		if !ok {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "task schedule is not configurable"})
			return
		}
		if svc == nil || svc.Scheduler == nil || svc.Tasks == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler unavailable"})
			return
		}
		var request struct {
			Enabled         *bool `json:"enabled"`
			IntervalSeconds int64 `json:"interval_seconds"`
		}
		if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule request"})
			return
		}
		if err := svc.Scheduler.UpdateSchedule(c.Request.Context(), job, *request.Enabled, request.IntervalSeconds); err != nil {
			switch {
			case errors.Is(err, service.ErrSchedulerIntervalInvalid):
				c.JSON(http.StatusBadRequest, gin.H{"error": "interval is outside the supported range"})
			case errors.Is(err, service.ErrSchedulerJobNotFound), errors.Is(err, service.ErrSchedulerConfigUnsupported):
				c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "task schedule is not configurable"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update task schedule"})
			}
			return
		}
		definitions, err := svc.Tasks.Definitions(svc.Scheduler.Status())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to refresh task definition"})
			return
		}
		for _, definition := range definitions {
			if definition.Key == key {
				c.JSON(http.StatusOK, definition)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "task definition not found"})
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
