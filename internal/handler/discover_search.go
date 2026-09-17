package handler

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func discoverSearchHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Query string `json:"query"`
			Kind  string `json:"kind"`
			Page  int    `json:"page"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&input) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "搜索参数无效"})
			return
		}
		input.Query = strings.TrimSpace(input.Query)
		if input.Query == "" || utf8.RuneCountInString(input.Query) > 100 || input.Page < 1 || input.Page > 500 || (input.Kind != "multi" && input.Kind != "movie" && input.Kind != "tv") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "搜索参数无效"})
			return
		}
		c.Header("Cache-Control", "private, no-store")
		if svc.Discover == nil || svc.Scraper == nil || !discoverProviderEnabled(c.Request.Context(), svc, "tmdb") {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TMDb 搜索暂不可用"})
			return
		}
		items, hasNext, err := svc.Discover.SearchTMDb(c.Request.Context(), input.Query, input.Kind, input.Page)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "TMDb 搜索失败，请重试"})
			return
		}
		if err := svc.Scraper.QueueMissingSearchMetadata(c.Request.Context(), items); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "搜索资料登记失败，请重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "has_next": hasNext, "page": input.Page})
	}
}
