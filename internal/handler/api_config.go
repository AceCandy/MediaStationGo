// Package handler — third-party API config (TMDb / Bangumi / TheTVDB / …).
//
// All routes live under /api/admin/api-configs/* so only administrators
// can list / update / delete provider keys.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func listAPIConfigsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := svc.APIConfig.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func getAPIConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.APIConfig.Get(c.Request.Context(), c.Param("provider"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if view == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, view)
	}
}

func updateAPIConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var patch service.APIConfigPatch
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		view, err := svc.APIConfig.Update(c.Request.Context(), c.Param("provider"), patch)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, view)
	}
}

func deleteAPIConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.APIConfig.Delete(c.Request.Context(), c.Param("provider")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func listProxyPoolHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := svc.ProxyPool.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func replaceProxyPoolHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request struct {
			Items []service.ProxyPoolInput `json:"items"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		items, err := svc.ProxyPool.Replace(c.Request.Context(), request.Items)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func getProxyPoolConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := svc.ProxyPool.GetConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "proxy pool configuration unavailable"})
			return
		}
		c.JSON(http.StatusOK, config)
	}
}

func updateProxyPoolConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var patch service.ProxyPoolConfigPatch
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		config, err := svc.ProxyPool.UpdateConfig(c.Request.Context(), patch)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, config)
	}
}

func checkProxyPoolHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := svc.ProxyPool.Check(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "proxy pool check failed"})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func cleanupProxyPoolHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, err := svc.ProxyPool.Cleanup(c.Request.Context(), request.Token)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "proxy pool cleanup failed; run the check again"})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
