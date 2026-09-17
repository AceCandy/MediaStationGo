package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func discoverDetailHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		identity := repository.DiscoverIdentity{TMDbID: id, MediaType: c.Param("kind")}
		if err != nil || !identity.Valid() {
			c.JSON(400, gin.H{"error": "作品身份无效"})
			return
		}
		detail, err := svc.Media.DiscoverTMDbDetail(c.Request.Context(), identity)
		if err != nil {
			c.JSON(502, gin.H{"error": "作品详情暂时不可用，请稍后重试"})
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, detail)
	}
}

func discoverTMDbRefreshHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		identity := repository.DiscoverIdentity{TMDbID: id, MediaType: c.Param("kind")}
		if err != nil || !identity.Valid() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "作品身份无效"})
			return
		}
		if err := svc.Scraper.RefreshMetadataTMDbByIdentity(c.Request.Context(), identity); err != nil {
			switch {
			case errors.Is(err, service.ErrMediaNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "本地元数据不存在"})
			case errors.Is(err, service.ErrTMDbRefreshIdentity):
				c.JSON(http.StatusBadRequest, gin.H{"error": "当前作品缺少唯一有效的 TMDB 标识，或 TMDB 返回的身份不一致"})
			default:
				c.JSON(http.StatusBadGateway, gin.H{"error": "TMDB 信息刷新未完成，请稍后重试"})
			}
			return
		}
		if svc.Media == nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "作品详情暂时不可用，请稍后重试"})
			return
		}
		detail, err := svc.Media.DiscoverTMDbDetail(c.Request.Context(), identity)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "作品详情暂时不可用，请稍后重试"})
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(http.StatusOK, detail)
	}
}

func discoverLibraryStatusHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
		var body struct {
			Items []repository.DiscoverIdentity `json:"items"`
		}
		if c.ShouldBindJSON(&body) != nil || len(body.Items) == 0 || len(body.Items) > 100 {
			c.JSON(400, gin.H{"error": "请提供 1 至 100 个作品"})
			return
		}
		for _, item := range body.Items {
			if !item.Valid() {
				c.JSON(400, gin.H{"error": "作品身份无效"})
				return
			}
		}
		visibility := mediaVisibilityForRequest(c, svc)
		items := []repository.DiscoverIdentity{}
		var err error
		if !visibility.LibraryRestricted || len(visibility.AllowedLibraryIDs) > 0 {
			items, err = svc.Repo.MediaView.FindDiscoverLibraryItems(c.Request.Context(), body.Items, repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "读取入库状态失败"})
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, gin.H{"items": items})
	}
}
