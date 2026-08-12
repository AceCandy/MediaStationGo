package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func peopleBackfillLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Repo == nil || svc.Repo.Library == nil || svc.Scraper == nil || svc.Tasks == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "people backfill unavailable"})
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
		task := svc.Tasks.StartTriggered(service.TaskKindPeople, service.TaskTriggerManual, "人物信息回填："+library.Name, service.TaskUpdate{Stage: "people", SourcePath: library.Path, Message: "人物信息回填已启动", Metrics: service.PeopleBackfillResult{}.Metrics()})
		if task == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create task execution failed"})
			return
		}
		go func() {
			result, runErr := svc.Scraper.BackfillLibraryPeople(svc.Context(), libraryID, func(current service.PeopleBackfillResult) {
				task.Update(service.TaskUpdate{Stage: "people", Metrics: current.Metrics(), Details: current.Details})
			})
			stage, message := "completed", "人物信息回填完成"
			if runErr != nil {
				stage, message = "people", "人物信息回填失败"
			}
			finishHTTPTask(task, runErr, stage, message, result.Metrics(), result.Details)
		}()
		c.JSON(http.StatusAccepted, gin.H{"status": "started"})
	}
}
