package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// mediaCredit 是详情页演职员横滚的轻量投影，头像走 /api/img 代理。
type mediaCredit = service.MediaCredit

// GET /media/:id/credits → 该媒体关联元数据的演职员（按展示顺序）。
func listMediaCreditsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMediaVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if c.Query("scope") == "series" {
			series, err := svc.Media.GetMediaSeriesVisible(c.Request.Context(), m.ID, mediaVisibilityForRequest(c, svc))
			if err != nil {
				writeInternalOrCanceled(c, err)
				return
			}
			if series == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			m = series
		}
		identity := m.MetadataID
		if m.CatalogSource == "nfo" {
			identity = m.CatalogItemID
		} else if m.CatalogSource == "hongguo" {
			identity = "hongguo:" + m.LookupCatalogID
		}
		credits, err := svc.Media.ListMetadataCredits(c.Request.Context(), identity)
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": credits})
	}
}
