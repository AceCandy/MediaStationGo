package handler

import (
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerHuangGuoAIDownloadRoutes(authed *gin.RouterGroup, svc *service.Container) {
	group := authed.Group("/catalogs/huangguoai/downloads", middleware.AdminRequired())
	group.Use(func(c *gin.Context) {
		if svc.HuangGuoAIDownloads == nil {
			c.AbortWithStatus(503)
			return
		}
		if !mediaVisibilityForRequest(c, svc).IncludeNSFW {
			c.AbortWithStatus(404)
			return
		}
		c.Next()
	})
	group.GET("/works", func(c *gin.Context) {
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil {
			c.Status(400)
			return
		}
		rows, total, e := svc.HuangGuoAIDownloads.Works(c.Request.Context(), page, c.Query("status"), c.Query("keyword"))
		if e != nil {
			c.Status(400)
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total})
	})
	group.GET("/works/:sourceID/episodes", func(c *gin.Context) {
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil {
			c.Status(400)
			return
		}
		rows, total, e := svc.HuangGuoAIDownloads.Episodes(c.Request.Context(), c.Param("sourceID"), page)
		if e != nil {
			c.Status(400)
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total})
	})
	group.POST("/works/:sourceID/:action", func(c *gin.Context) {
		count, e := svc.HuangGuoAIDownloads.WorkAction(c.Request.Context(), c.Param("sourceID"), c.Param("action"))
		if e != nil {
			c.Status(400)
			return
		}
		c.JSON(200, gin.H{"updated": count})
	})
	group.GET("/config", func(c *gin.Context) {
		cfg, err := svc.HuangGuoAIDownloads.Config(c.Request.Context())
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, cfg)
	})
	group.PUT("/config", func(c *gin.Context) {
		var body service.HuangGuoAIDownloadConfig
		if c.ShouldBindJSON(&body) != nil {
			c.Status(400)
			return
		}
		cfg, err := svc.HuangGuoAIDownloads.SaveConfig(c.Request.Context(), body)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cfg)
	})
	group.GET("", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			c.Status(400)
			return
		}
		rows, total, err := svc.HuangGuoAIDownloads.List(c.Request.Context(), page)
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total, "page": page})
	})
	group.POST("", func(c *gin.Context) {
		if !requireTasksReady(c, svc) {
			return
		}
		var body struct {
			SourceID string `json:"source_id"`
		}
		if c.ShouldBindJSON(&body) != nil {
			c.Status(400)
			return
		}
		count, err := svc.HuangGuoAIDownloads.Enqueue(c.Request.Context(), body.SourceID)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(202, gin.H{"added": count})
	})
	group.POST("/:id/:action", func(c *gin.Context) {
		if _, err := uuid.Parse(c.Param("id")); err != nil {
			c.Status(400)
			return
		}
		if err := svc.HuangGuoAIDownloads.Action(c.Request.Context(), c.Param("id"), c.Param("action")); err != nil {
			c.JSON(400, gin.H{"error": "操作失败，请检查任务状态或稍后重试"})
			return
		}
		c.Status(204)
	})
}
