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

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func tasksHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		system := c.Query("system")
		if system != "" && system != model.TaskSystemCommon && system != model.TaskSystemCatalog && system != model.TaskSystemHongGuo {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task system"})
			return
		}
		background := service.TaskSnapshot{}
		page := service.TaskPage{Items: []service.BackgroundTask{}, Page: 1, PageSize: 30}
		definitions := []service.TaskDefinition{}
		if svc != nil && svc.Tasks != nil {
			if system == "" {
				background = svc.Tasks.Snapshot()
			}
			pageNum, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
			var err error
			page, err = svc.Tasks.ListSystem(system, pageNum, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tasks"})
				return
			}
			var scheduler []service.JobStatus
			if svc.Scheduler != nil {
				scheduler = svc.Scheduler.Status()
			}
			definitions, err = svc.Tasks.DefinitionsForSystem(scheduler, system)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list task definitions"})
				return
			}
			if system != "" {
				background.Active, background.Recent = []service.BackgroundTask{}, []service.BackgroundTask{}
				for _, definition := range definitions {
					if definition.Current != nil {
						background.Active = append(background.Active, *definition.Current)
					}
					if definition.Latest != nil {
						background.Recent = append(background.Recent, *definition.Latest)
					}
				}
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
		if key == service.TaskKindHongGuoSupplement {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
			var request struct {
				Count int `json:"count"`
			}
			if c.ShouldBindJSON(&request) != nil || request.Count < 1 || request.Count > 100 {
				c.JSON(400, gin.H{"error": "本次补充数量须为 1–100 部"})
				return
			}
			if svc == nil || svc.Scheduler == nil {
				c.JSON(503, gin.H{"error": "调度服务不可用"})
				return
			}
			if !handleSchedulerRunResult(c, svc.Scheduler.RunHongGuoSupplementNowAsync(c.Request.Context(), request.Count)) {
				return
			}
			c.JSON(202, gin.H{"status": "queued"})
			return
		}
		if key == service.TaskDefinitionMediaScrape {
			if svc == nil || svc.Scraper == nil || svc.Repo == nil || svc.Repo.Library == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "media scrape unavailable"})
				return
			}
			var request struct {
				LibraryID    string `json:"library_id"`
				AllLibraries bool   `json:"all_libraries"`
			}
			if err := c.ShouldBindJSON(&request); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid media scrape request"})
				return
			}
			request.LibraryID = strings.TrimSpace(request.LibraryID)
			if (request.LibraryID == "") == !request.AllLibraries {
				c.JSON(http.StatusBadRequest, gin.H{"error": "choose exactly one of library_id or all_libraries"})
				return
			}
			libraries, err := svc.Repo.Library.List(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load libraries"})
				return
			}
			if request.LibraryID != "" {
				found := false
				for _, library := range libraries {
					if library.ID != request.LibraryID {
						continue
					}
					found = true
					if !service.LibraryTypeSupportsMetadataScrape(library.Type) {
						c.JSON(http.StatusBadRequest, gin.H{"error": "library type does not support media scrape"})
						return
					}
					libraries = []model.Library{library}
					break
				}
				if !found {
					c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
					return
				}
			}
			var queued int64
			processedLibraries := 0
			for _, library := range libraries {
				if !service.LibraryTypeSupportsMetadataScrape(library.Type) {
					continue
				}
				count, err := svc.Scraper.ResetLibraryScrape(c.Request.Context(), library.ID, false)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to queue media scrape"})
					return
				}
				queued += count
				processedLibraries++
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "queued", "count": queued, "libraries": processedLibraries})
			return
		}
		if key == service.TaskDefinitionLibraryScan {
			if svc == nil || svc.Scheduler == nil || svc.Repo == nil || svc.Repo.Library == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "library scan unavailable"})
				return
			}
			var request struct {
				LibraryID string `json:"library_id"`
			}
			if err := c.ShouldBindJSON(&request); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid library scan request"})
				return
			}
			request.LibraryID = strings.TrimSpace(request.LibraryID)
			if request.LibraryID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "library_id required"})
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
			if !triggerLibraryScanJob(c, svc, request.LibraryID) {
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
			return
		}
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
			if err := startProbeBackfill(svc, service.TaskTriggerManual, taskName, sourcePath, request.LibraryID, request.Limit); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, service.ErrMediaProbeBackfillRunning) {
					status = http.StatusConflict
				}
				c.JSON(status, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "started"})
			return
		}
		if key == service.TaskDefinitionTMDbSnapshotBackfill {
			if svc == nil || svc.Scraper == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TMDB snapshot backfill unavailable"})
				return
			}
			if err := svc.Scraper.StartTMDbSnapshotBackfill(svc.Context(), false); err != nil {
				switch {
				case errors.Is(err, service.ErrTMDbSnapshotBackfillRunning):
					c.JSON(http.StatusConflict, gin.H{"error": "TMDB snapshot backfill already running"})
				case errors.Is(err, service.ErrTMDbSnapshotBackfillUnavailable):
					c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TMDB snapshot backfill unavailable"})
				default:
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start TMDB snapshot backfill"})
				}
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "started"})
			return
		}
		if key == service.TaskDefinitionSeriesLocalCorrection {
			if svc == nil || svc.Scraper == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "series local correction unavailable"})
				return
			}
			if err := svc.Scraper.StartSeriesLocalCorrection(svc.Context(), false); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, service.ErrSeriesLocalCorrectionRunning) {
					status = http.StatusConflict
				} else if errors.Is(err, service.ErrSeriesLocalCorrectionUnavailable) {
					status = http.StatusServiceUnavailable
				}
				c.JSON(status, gin.H{"error": "无法启动剧集本地资料纠正，请检查是否已有任务运行或服务不可用"})
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "started"})
			return
		}
		if !service.TaskDefinitionExists(key) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task definition not found"})
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
			Count           *int  `json:"count"`
		}
		if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule request"})
			return
		}
		var counts []int
		if request.Count != nil {
			counts = append(counts, *request.Count)
		}
		if err := svc.Scheduler.UpdateSchedule(c.Request.Context(), job, *request.Enabled, request.IntervalSeconds, counts...); err != nil {
			switch {
			case errors.Is(err, service.ErrSchedulerCountInvalid):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
