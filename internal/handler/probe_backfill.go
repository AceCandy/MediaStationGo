package handler

import (
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

		task := svc.Tasks.StartTriggered(service.TaskKindProbe, service.TaskTriggerManual, "媒体轨道回填："+library.Name, service.TaskUpdate{
			Stage: "probe", SourcePath: library.Path,
			Message: "媒体轨道回填已启动",
			Metrics: service.ProbeBackfillResult{}.Metrics(),
		})
		if task == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create task execution failed"})
			return
		}
		go func() {
			result, err := svc.MediaProbe.BackfillLibrary(svc.Context(), libraryID, 0, func(current service.ProbeBackfillResult) {
				task.Update(service.TaskUpdate{Stage: "probe", Metrics: current.Metrics(), Details: current.Details})
			})
			stage, message := "completed", "媒体轨道回填完成"
			if err != nil {
				stage, message = "probe", "媒体轨道回填失败"
			}
			finishHTTPTask(task, err, stage, message, result.Metrics(), result.Details, false)
		}()
		c.JSON(http.StatusAccepted, gin.H{"status": "started"})
	}
}
