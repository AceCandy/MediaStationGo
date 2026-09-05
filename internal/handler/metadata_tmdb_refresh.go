package handler

import (
	"errors"
	"net/http"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func refreshMetadataTMDbHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := svc.Scraper.RefreshMetadataTMDb(c.Request.Context(), c.Param("id"))
		switch {
		case errors.Is(err, service.ErrMediaNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "元数据不存在"})
		case errors.Is(err, service.ErrTMDbRefreshIdentity):
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前元数据缺少唯一有效的 TMDB 标识，或 TMDB 返回的身份不一致"})
		case err != nil:
			c.JSON(http.StatusBadGateway, gin.H{"error": "TMDB 信息刷新未完成，请检查 TMDB 配置或稍后重试"})
		default:
			c.JSON(http.StatusOK, gin.H{"status": "complete"})
		}
	}
}
