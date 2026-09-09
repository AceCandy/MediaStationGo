package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func searchDoubanBindingHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
		defer cancel()
		items, err := svc.Scraper.SearchDoubanBinding(ctx, c.Param("id"), c.Query("query"))
		if err != nil {
			writeDoubanBindingError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func bindDoubanHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req service.DoubanBindingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "豆瓣绑定参数无效"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
		defer cancel()
		degraded, err := svc.Scraper.BindDouban(ctx, c.Param("id"), req)
		if err != nil {
			writeDoubanBindingError(c, err)
			return
		}
		status := "complete"
		if degraded {
			status = "degraded"
		}
		c.JSON(http.StatusOK, gin.H{"status": status})
	}
}

func writeDoubanBindingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDoubanSearchUnavailable):
		c.JSON(http.StatusBadGateway, gin.H{"error": service.ErrDoubanSearchUnavailable.Error()})
	case errors.Is(err, service.ErrMediaNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "元数据不存在"})
	case errors.Is(err, service.ErrDoubanBindingInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": service.ErrDoubanBindingInvalid.Error()})
	case errors.Is(err, service.ErrDoubanBindingConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrDoubanSubjectNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "豆瓣条目或对应类型详情不存在，未更改绑定"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "豆瓣信息获取或保存失败，未更改绑定，请稍后重试"})
	}
}
