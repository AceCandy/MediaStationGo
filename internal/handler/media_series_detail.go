package handler

import (
	"net/http"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

// getMediaSeriesHandler 只通过当前可见文件读取其整剧元数据和用户收藏。
func getMediaSeriesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		series, err := svc.Media.GetMediaSeriesVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if series == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		identity := series.MetadataID
		if series.CatalogSource == "nfo" {
			identity = series.CatalogItemID
		}
		favorite, err := svc.Repo.Favorite.IsFavoriteByIdentity(c.Request.Context(), toString(uid), identity, "")
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"series": series, "favourite": favorite})
	}
}

// getMediaSeasonHandler 返回关联季的只读展示资料，不提供文件播放身份。
func getMediaSeasonHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		season, err := svc.Media.GetMediaSeasonVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"season": season})
	}
}

// setMediaSeriesFavoriteHandler 收藏整剧身份，不把代表文件收藏成单集。
func setMediaSeriesFavoriteHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Favourite *bool `json:"favourite" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "favourite is required"})
			return
		}
		series, err := svc.Media.GetMediaSeriesVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if series == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		identity := series.MetadataID
		if series.CatalogSource == "nfo" {
			identity = series.CatalogItemID
		}
		if _, err := svc.Repo.Favorite.SetByIdentity(c.Request.Context(), toString(uid), identity, c.Param("id"), *req.Favourite); err != nil {
			writeFavoriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"favourite": *req.Favourite})
	}
}
