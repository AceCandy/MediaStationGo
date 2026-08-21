package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func peopleBackfillHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.Scheduler == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "people backfill unavailable"})
			return
		}
		job, ok := service.TaskDefinitionSchedulerJob(service.TaskDefinitionPeopleBackfill)
		if !ok || !triggerSchedulerJob(c, svc, job) {
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
	}
}

// peopleBackfillLibraryHandler 保留旧入口兼容，回填语义已统一为全局缺失对象。
func peopleBackfillLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return peopleBackfillHandler(svc)
}
