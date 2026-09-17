package handler

import (
	"net/http"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func registerHongGuoDownloadRoutes(authed *gin.RouterGroup, svc *service.Container) {
	group := authed.Group("/catalogs/hongguo/downloads", middleware.AdminRequired())
	group.Use(func(c *gin.Context) {
		if svc.HongGuoDownloads == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "下载服务不可用"})
			return
		}
		c.Next()
	})
	group.GET("/config", func(c *gin.Context) {
		cfg, err := svc.HongGuoDownloads.Config(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": "读取下载配置失败"})
			return
		}
		c.JSON(200, cfg)
	})
	group.PUT("/config", func(c *gin.Context) {
		var body struct {
			Root string `json:"root"`
			service.HongGuoDownloadConfigPatch
		}
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, gin.H{"error": "下载配置无效"})
			return
		}
		cfg, err := svc.HongGuoDownloads.SaveConfig(c.Request.Context(), body.Root, body.HongGuoDownloadConfigPatch)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cfg)
	})
	group.GET("", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			c.JSON(400, gin.H{"error": "分页参数无效"})
			return
		}
		rows, total, err := svc.HongGuoDownloads.List(c.Request.Context(), page)
		if err != nil {
			c.JSON(500, gin.H{"error": "读取下载任务失败"})
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total, "page": page})
	})
	group.POST("", func(c *gin.Context) {
		var body struct {
			SourceID string `json:"source_id"`
		}
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, gin.H{"error": "下载请求无效"})
			return
		}
		count, err := svc.HongGuoDownloads.Enqueue(c.Request.Context(), body.SourceID)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(202, gin.H{"added": count})
	})
	group.POST("/supplement", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var body struct {
			Count int `json:"count"`
		}
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, gin.H{"error": "补充下载数量无效"})
			return
		}
		result, err := svc.HongGuoDownloads.Supplement(c.Request.Context(), body.Count)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(202, result)
	})
	group.POST("/:id/:action", func(c *gin.Context) {
		if _, err := uuid.Parse(c.Param("id")); err != nil {
			c.JSON(400, gin.H{"error": "任务 ID 无效"})
			return
		}
		if err := svc.HongGuoDownloads.Action(c.Request.Context(), c.Param("id"), c.Param("action")); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.Status(204)
	})
	group.GET("/works", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			c.JSON(400, gin.H{"error": "分页参数无效"})
			return
		}
		failedOnly, err := strconv.ParseBool(c.DefaultQuery("failed_only", "false"))
		if err != nil {
			c.JSON(400, gin.H{"error": "失败筛选参数无效"})
			return
		}
		rows, total, err := svc.HongGuoDownloads.ListWorks(c.Request.Context(), page, failedOnly)
		if err != nil {
			c.JSON(500, gin.H{"error": "读取作品下载任务失败"})
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total})
	})
	group.GET("/works/:source/episodes", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 || !hongguo.ValidID(c.Param("source")) {
			c.JSON(400, gin.H{"error": "作品或分页参数无效"})
			return
		}
		rows, total, err := svc.HongGuoDownloads.ListEpisodes(c.Request.Context(), c.Param("source"), page)
		if err != nil {
			c.JSON(500, gin.H{"error": "读取分集下载任务失败"})
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total})
	})
	group.POST("/works/:source/retry", func(c *gin.Context) {
		added, skipped, err := svc.HongGuoDownloads.RetryFailedWork(c.Request.Context(), c.Param("source"))
		if err != nil {
			c.JSON(400, gin.H{"error": "重试作品失败，请检查作品 ID 或稍后重试"})
			return
		}
		c.JSON(200, gin.H{"added": added, "skipped": skipped})
	})
}
